// Package fanout dispatches the SAME prompt to every selected reviewer CLI in
// parallel (claude, codex, agy by default; agent opt-in), parses each
// reviewer's JSON verdict, and emits a merged canonical response. This is the
// "adversarial review" audit capability — fresh-eyes, single-shot, FAIL-OR —
// folded into converge from the former standalone adversarial-review skill.
//
// Driven by the `converge audit` subcommand:
//
//	cat prompt.txt | converge audit
//	converge audit --prompt-file prompt.txt --reviewers claude,codex,agy
//
// The merged response shape (see SKILL.md `audit` mode for the contract):
//
//	{
//	  "summary": "all_pass" | "some_fail" | "parse_error",
//	  "verdicts": [
//	    {"draft_id": "<id>", "verdict": "PASS"|"FAIL",
//	     "issues": ["[claude] ...", "[claude+codex] ...", "[claude+codex+agy] ..."]}
//	  ],
//	  "reviewers": ["claude", "codex", "agy"],
//	  "skipped": {"<reviewer>": "<reason>"},
//	  "parse_error": ["<reviewer>", ...],
//	  "error": string, "raw_response": string
//	}
//
// Merge rule: a draft is FAIL if ANY reviewer flagged it FAIL. Issues are
// clustered by overlap and prefixed with the reviewers that raised them.
package fanout

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/michaellady/mike-skills/converge/internal/ledger"
	"github.com/michaellady/mike-skills/llm-provider/agent"
	"github.com/michaellady/mike-skills/llm-provider/agy"
	"github.com/michaellady/mike-skills/llm-provider/claude"
	"github.com/michaellady/mike-skills/llm-provider/codex"
	"github.com/michaellady/mike-skills/llm-provider/provider"
)

type verdict struct {
	DraftID string   `json:"draft_id"`
	Verdict string   `json:"verdict"`
	Issues  []string `json:"issues"`
}

// UnmarshalJSON makes a verdict tolerant of the shape differences between reviewer CLIs.
// The id field may arrive as "draft_id" or "id", and each issue may be a plain string OR
// a structured object ({severity,file,line,issue,...}) — codex emits objects, agy emits
// strings. Objects are flattened to a readable one-line string so the merge (which
// clusters on string issues) works uniformly across reviewers. (Without this, codex's
// object-shaped issues fail to unmarshal into []string and the whole reviewer is dropped
// as a parse_error.)
func (v *verdict) UnmarshalJSON(b []byte) error {
	var aux struct {
		DraftID string            `json:"draft_id"`
		ID      string            `json:"id"`
		Verdict string            `json:"verdict"`
		Issues  []json.RawMessage `json:"issues"`
	}
	if err := json.Unmarshal(b, &aux); err != nil {
		return err
	}
	v.DraftID = aux.DraftID
	if v.DraftID == "" {
		v.DraftID = aux.ID
	}
	v.Verdict = aux.Verdict
	v.Issues = make([]string, 0, len(aux.Issues))
	for _, raw := range aux.Issues {
		if s := issueToString(raw); s != "" {
			v.Issues = append(v.Issues, s)
		}
	}
	return nil
}

// issueToString renders one issue element: a JSON string as-is, a structured issue object
// as "[severity] file:line — message", else the raw JSON as a fallback (never dropped).
func issueToString(raw json.RawMessage) string {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return ""
	}
	if s[0] == '"' {
		var str string
		if err := json.Unmarshal([]byte(s), &str); err == nil {
			return str
		}
		return s
	}
	var o struct {
		Severity string `json:"severity"`
		File     string `json:"file"`
		Line     any    `json:"line"`
		LineNum  any    `json:"line_start"`
		Issue    string `json:"issue"`
		Message  string `json:"message"`
		Detail   string `json:"detail"`
		Title    string `json:"title"`
	}
	if err := json.Unmarshal([]byte(s), &o); err != nil {
		return s // unknown shape — keep the raw JSON rather than drop the finding
	}
	text := firstNonEmpty(o.Issue, o.Message, o.Detail, o.Title)
	if text == "" {
		return s
	}
	var sb strings.Builder
	if o.Severity != "" {
		fmt.Fprintf(&sb, "[%s] ", o.Severity)
	}
	if o.File != "" {
		sb.WriteString(o.File)
		if ln := lineString(o.Line, o.LineNum); ln != "" {
			sb.WriteString(":" + ln)
		}
		sb.WriteString(" — ")
	}
	sb.WriteString(text)
	return sb.String()
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// lineString renders the first present line number from candidate fields (JSON numbers
// decode as float64; strings are passed through).
func lineString(vals ...any) string {
	for _, v := range vals {
		switch n := v.(type) {
		case float64:
			if n != 0 {
				return fmt.Sprintf("%d", int(n))
			}
		case string:
			if n != "" {
				return n
			}
		}
	}
	return ""
}

type reviewerResp struct {
	Summary  string    `json:"summary"`
	Verdicts []verdict `json:"verdicts"`
}

type mergedResp struct {
	Summary     string            `json:"summary"`
	Verdicts    []verdict         `json:"verdicts"`
	Reviewers   []string          `json:"reviewers"`
	Skipped     map[string]string `json:"skipped,omitempty"`
	ParseError  []string          `json:"parse_error,omitempty"`
	Error       string            `json:"error,omitempty"`
	RawResponse string            `json:"raw_response,omitempty"`
}

// reviewerSpec describes one reviewer the audit knows how to dispatch.
// Order in `registeredReviewers` is the canonical reviewer order — also the
// order issue attribution is rendered in (e.g. "[claude+codex+agy]").
type reviewerSpec struct {
	name  string
	cli   string // CLI name on PATH
	model string // optional --model override passed to the provider ("" = provider default)
	make  func() provider.Provider
}

// registeredReviewers is every provider the audit knows how to dispatch.
// Same provider constructors converge's internal/dispatch wires; kept here as
// a list so attribution order and PATH (cli) names stay explicit.
var registeredReviewers = []reviewerSpec{
	{name: "claude", cli: "claude", make: func() provider.Provider { return claude.New() }},
	{name: "codex", cli: "codex", make: func() provider.Provider { return codex.New() }},
	{name: "agent", cli: "agent", make: func() provider.Provider { return agent.New() }},
	// composer-2.5 and grok-build are Cursor `agent` CLI *models*, not separate
	// binaries — same `agent` provider, pinned via --model. cli stays "agent"
	// (PATH check), and runProvider keys its thread temp-file on `name` (not
	// p.Name(), which is "agent" for all three) so concurrent runs don't collide.
	{name: "composer-2.5", cli: "agent", model: "composer-2.5", make: func() provider.Provider { return agent.New() }},
	{name: "grok-build", cli: "agent", model: "grok-build-0.1", make: func() provider.Provider { return agent.New() }},
	{name: "agy", cli: "agy", make: func() provider.Provider { return agy.New() }},
}

// defaultReviewers is the comma-separated default for --reviewers.
//
// Default = claude + codex + agy + composer-2.5 + grok-build: independent agent
// families catch different failure modes. composer-2.5 and grok-build run via
// the Cursor `agent` CLI and need a paid Cursor plan — on a free/low-tier plan
// they quota-fail and land under `skipped`, so including them by default is
// safe (they simply don't contribute when unavailable). The bare `agent`
// provider stays OPT-IN to avoid a redundant `auto`-model run alongside these.
//
// Per-reviewer failures degrade gracefully: a reviewer that quota-fails,
// auth-fails, or times out is reported under `skipped` (see unavailableReason),
// NOT `parse_error` — so the remaining reviewers still produce a merged verdict.
const defaultReviewers = "claude,codex,agy,composer-2.5,grok-build"

// Run executes one audit fan-out. args are the `converge audit` subcommand
// args (flags only). Returns the desired process exit code.
func Run(args []string) int {
	start := time.Now()
	fs := flag.NewFlagSet("audit", flag.ContinueOnError)
	var promptFile string
	var timeoutSec int
	var quiet bool
	var reviewersCSV string
	var noLedger bool
	var label string
	var ledgerPath string
	fs.StringVar(&promptFile, "prompt-file", "", "path to prompt file; if empty, read from stdin")
	fs.IntVar(&timeoutSec, "timeout", 300, "per-reviewer timeout (seconds)")
	fs.BoolVar(&quiet, "quiet", false, "suppress provider heartbeat lines on stderr")
	fs.StringVar(&reviewersCSV, "reviewers", defaultReviewers,
		"comma-separated reviewers to dispatch (registered: claude,codex,agent,composer-2.5,grok-build,agy)")
	fs.BoolVar(&noLedger, "no-ledger", false, "do not record this audit to the SQLite ledger")
	fs.StringVar(&label, "label", "", "label for the ledger audit row (defaults to the first draft id)")
	fs.StringVar(&ledgerPath, "ledger", "", "ledger DB path (overrides CONVERGE_LEDGER for this run)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	// A --ledger flag points the ledger package at a specific DB by exporting
	// CONVERGE_LEDGER, which is where ledger.open() looks first.
	if ledgerPath != "" {
		_ = os.Setenv("CONVERGE_LEDGER", ledgerPath)
	}

	selected, err := selectReviewers(reviewersCSV)
	if err != nil {
		fmt.Fprintf(os.Stderr, "audit: %v\n", err)
		return 2
	}

	promptPath, cleanup, err := resolvePromptPath(promptFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "audit: prompt input: %v\n", err)
		return 2
	}
	defer cleanup()

	out := mergedResp{
		Verdicts:  []verdict{},
		Reviewers: []string{},
		Skipped:   map[string]string{},
	}

	type result struct {
		name      string
		out       string
		err       error
		latencyMs int64
	}
	resultsCh := make(chan result, len(selected))
	var wg sync.WaitGroup

	for _, r := range selected {
		if _, ok := lookCLI(r.cli); !ok {
			out.Skipped[r.name] = fmt.Sprintf("%s CLI not on PATH", r.cli)
			continue
		}
		wg.Add(1)
		r := r
		go func() {
			defer wg.Done()
			provStart := time.Now()
			s, e := runProvider(r.name, r.make(), r.model, promptPath, timeoutSec, quiet)
			resultsCh <- result{name: r.name, out: s, err: e, latencyMs: time.Since(provStart).Milliseconds()}
		}()
	}
	wg.Wait()
	close(resultsCh)

	latencyByName := map[string]int64{}
	parsed := map[string]*reviewerResp{}
	for res := range resultsCh {
		latencyByName[res.name] = res.latencyMs
		if res.err != nil {
			// A reviewer dispatched but unable to produce a verdict for reasons
			// outside the audit (quota, auth, timeout) is "skipped", not a
			// malformed-output parse_error. Keeps the merged result honest.
			if reason, ok := unavailableReason(res.err); ok {
				out.Skipped[res.name] = reason
			} else {
				out.ParseError = append(out.ParseError, res.name)
			}
			out.RawResponse += fmt.Sprintf("[%s error] %v\n", res.name, res.err)
			continue
		}
		r, perr := parseResponse(res.out)
		if perr != nil {
			out.ParseError = append(out.ParseError, res.name)
			out.RawResponse += fmt.Sprintf("[%s raw]\n%s\n", res.name, res.out)
			continue
		}
		parsed[res.name] = r
	}

	if len(parsed) == 0 {
		out.Summary = "parse_error"
		out.Error = "no reviewers returned a usable verdict (all skipped, errored, or malformed JSON)"
		recordLedger(out, parsed, selected, promptPath, label, noLedger, latencyByName, time.Since(start).Milliseconds())
		printReviewerLine(out, parsed, selected, quiet)
		emit(out)
		return 2
	}

	merged := merge(parsed, selected)
	out.Summary = merged.Summary
	out.Verdicts = merged.Verdicts
	for _, r := range selected {
		if _, ok := parsed[r.name]; ok {
			out.Reviewers = append(out.Reviewers, r.name)
		}
	}

	if len(out.Skipped) == 0 {
		out.Skipped = nil
	}
	recordLedger(out, parsed, selected, promptPath, label, noLedger, latencyByName, time.Since(start).Milliseconds())
	printReviewerLine(out, parsed, selected, quiet)
	emit(out)
	return 0
}

// issueRe parses a merged issue string of the form
// "[claude+codex] HIGH: title — detail (file:line)" into its attribution,
// severity, and the remaining text. Issues that don't match are still recorded
// (severity / raised_by empty).
var issueRe = regexp.MustCompile(`^\[(?P<raised_by>[^\]]*)\]\s*(?P<sev>CRITICAL|HIGH|MEDIUM|LOW):\s*(?P<rest>.*)$`)

// locRe captures a trailing parenthesized group that looks like "file:line".
var locRe = regexp.MustCompile(`\(([^()]*:\d+)\)\s*$`)

// recordLedger builds an AuditRecord from the merged result and persists it,
// best-effort: any error is logged to stderr and otherwise ignored so the
// audit's own exit code and stdout JSON are never affected. Skipped entirely
// when noLedger is set.
func recordLedger(out mergedResp, parsed map[string]*reviewerResp, selected []reviewerSpec, promptPath, label string, noLedger bool, latency map[string]int64, durationMs int64) {
	if noLedger {
		return
	}

	rec := ledger.AuditRecord{
		AuditID:            ledger.NewAuditID(),
		TS:                 time.Now().UTC().Format(time.RFC3339),
		Label:              label,
		Summary:            out.Summary,
		PromptSHA256:       promptSHA256(promptPath),
		ReviewersRequested: selectedNames(selected),
		DurationMs:         durationMs,
	}
	if rec.Label == "" {
		rec.Label = firstDraftID(out.Verdicts)
	}

	// One ReviewRow per SELECTED reviewer.
	for _, r := range selected {
		row := ledger.ReviewRow{Model: r.name, LatencyMs: latency[r.name]}
		switch {
		case parsed[r.name] != nil:
			row.Status = "responded"
			row.Verdict = parsed[r.name].Summary
		case contains(out.ParseError, r.name):
			row.Status = "parse_error"
		case skippedHas(out.Skipped, r.name):
			row.Status = "skipped"
		default:
			// Defensive: a selected reviewer that produced no signal at all
			// is recorded as skipped rather than silently dropped.
			row.Status = "skipped"
		}
		rec.Reviews = append(rec.Reviews, row)
	}

	// One FindingRow per merged issue across all verdicts.
	for _, v := range out.Verdicts {
		for _, issue := range v.Issues {
			rec.Findings = append(rec.Findings, parseIssue(issue))
		}
	}

	if err := ledger.Record(rec); err != nil {
		fmt.Fprintf(os.Stderr, "audit: ledger: %v\n", err)
	}
}

// parseIssue turns one merged issue string into a FindingRow. Title = text up to
// the first " — " (em dash) or "(", whichever comes first; Loc = the last
// parenthesized "file:line" group if present.
func parseIssue(issue string) ledger.FindingRow {
	raisedBy, severity, rest := "", "", issue
	if m := issueRe.FindStringSubmatch(issue); m != nil {
		raisedBy = m[issueRe.SubexpIndex("raised_by")]
		severity = m[issueRe.SubexpIndex("sev")]
		rest = m[issueRe.SubexpIndex("rest")]
	}

	loc := ""
	if lm := locRe.FindStringSubmatch(rest); lm != nil {
		loc = strings.TrimSpace(lm[1])
	}

	title := rest
	if i := strings.Index(title, " — "); i >= 0 {
		title = title[:i]
	} else if i := strings.Index(title, "("); i >= 0 {
		title = title[:i]
	}
	title = strings.TrimSpace(title)

	return ledger.FindingRow{
		FindingID: ledger.FindingID(loc, title),
		Severity:  severity,
		Title:     title,
		Loc:       loc,
		RaisedBy:  raisedBy,
	}
}

// printReviewerLine prints ONE per-reviewer ground-truth line to STDERR
// summarizing every SELECTED reviewer, e.g.:
//
//	reviewers: codex=responded(some_fail) claude=parse_error composer-2.5=skipped
//
// This makes a false all_pass impossible to miss. Suppressed under --quiet.
func printReviewerLine(out mergedResp, parsed map[string]*reviewerResp, selected []reviewerSpec, quiet bool) {
	if quiet {
		return
	}
	parts := make([]string, 0, len(selected))
	for _, r := range selected {
		switch {
		case parsed[r.name] != nil:
			parts = append(parts, fmt.Sprintf("%s=responded(%s)", r.name, parsed[r.name].Summary))
		case contains(out.ParseError, r.name):
			parts = append(parts, fmt.Sprintf("%s=parse_error", r.name))
		case skippedHas(out.Skipped, r.name):
			parts = append(parts, fmt.Sprintf("%s=skipped", r.name))
		default:
			parts = append(parts, fmt.Sprintf("%s=skipped", r.name))
		}
	}
	fmt.Fprintf(os.Stderr, "reviewers: %s\n", strings.Join(parts, " "))
}

func promptSHA256(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func selectedNames(selected []reviewerSpec) string {
	names := make([]string, 0, len(selected))
	for _, r := range selected {
		names = append(names, r.name)
	}
	return strings.Join(names, ",")
}

func firstDraftID(verdicts []verdict) string {
	for _, v := range verdicts {
		if v.DraftID != "" {
			return v.DraftID
		}
	}
	return ""
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func skippedHas(m map[string]string, k string) bool {
	if m == nil {
		return false
	}
	_, ok := m[k]
	return ok
}

// selectReviewers parses the comma-separated --reviewers flag against the
// registered set and returns specs in the user's requested order.
func selectReviewers(csv string) ([]reviewerSpec, error) {
	csv = strings.TrimSpace(csv)
	if csv == "" {
		return nil, fmt.Errorf("--reviewers cannot be empty")
	}
	byName := map[string]reviewerSpec{}
	knownNames := []string{}
	for _, r := range registeredReviewers {
		byName[r.name] = r
		knownNames = append(knownNames, r.name)
	}
	out := []reviewerSpec{}
	seen := map[string]bool{}
	for _, raw := range strings.Split(csv, ",") {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		spec, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("unknown reviewer %q (registered: %s)", name, strings.Join(knownNames, ","))
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, spec)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("--reviewers selected no reviewers")
	}
	return out, nil
}

func resolvePromptPath(p string) (string, func(), error) {
	if p != "" {
		if _, err := os.Stat(p); err != nil {
			return "", nil, err
		}
		return p, func() {}, nil
	}
	tmp, err := os.CreateTemp("", "converge-audit-prompt-*.txt")
	if err != nil {
		return "", nil, err
	}
	if _, err := io.Copy(tmp, os.Stdin); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return "", nil, err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return "", nil, err
	}
	return tmp.Name(), func() { _ = os.Remove(tmp.Name()) }, nil
}

func lookCLI(name string) (string, bool) {
	p, err := exec.LookPath(name)
	if err != nil {
		return "", false
	}
	return p, true
}

func runProvider(name string, p provider.Provider, model string, promptPath string, timeoutSec int, quiet bool) (string, error) {
	var buf strings.Builder
	opts := provider.Options{
		PromptFile: promptPath,
		Model:      model,
		Timeout:    time.Duration(timeoutSec) * time.Second,
		Quiet:      quiet,
		Stdout:     &buf,
		Stderr:     os.Stderr,
		// Key the thread temp-file on the reviewer's registry name, NOT
		// p.Name(): agent / composer-2.5 / grok-build all return "agent", so
		// using p.Name() would collide when several run concurrently.
		ThreadOut: filepath.Join(os.TempDir(), fmt.Sprintf("converge-audit-%s-%d.thread", name, os.Getpid())),
	}
	err := p.Run(context.Background(), opts)
	return buf.String(), err
}

// unavailableReason classifies a provider error as a graceful "skipped" — the
// reviewer was dispatched but couldn't produce a verdict for reasons outside
// the audit itself (quota/rate limit, auth, or timeout) — versus a genuine
// failure that belongs in parse_error.
func unavailableReason(err error) (string, bool) {
	if err == nil {
		return "", false
	}
	msg := strings.ToLower(err.Error())
	containsAny := func(subs ...string) bool {
		for _, s := range subs {
			if strings.Contains(msg, s) {
				return true
			}
		}
		return false
	}
	switch {
	case containsAny("usage limit", "quota", "rate limit", "too many requests", "429"):
		return "usage/quota limit", true
	case containsAny("not logged in", "unauthorized", "authentication", "auth error", "401", "403"):
		return "auth/login required", true
	case containsAny("timed out", "timeout", "deadline exceeded"):
		return "timed out", true
	}
	return "", false
}

// parseResponse extracts the JSON verdict object from a reviewer's reply,
// tolerating markdown fences and surrounding prose.
func parseResponse(s string) (*reviewerResp, error) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if i := strings.Index(s, "\n"); i >= 0 {
			s = s[i+1:]
		}
		if j := strings.LastIndex(s, "```"); j >= 0 {
			s = s[:j]
		}
		s = strings.TrimSpace(s)
	}
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end < 0 || end < start {
		return nil, fmt.Errorf("no JSON object found")
	}
	s = s[start : end+1]
	var r reviewerResp
	if err := json.Unmarshal([]byte(s), &r); err != nil {
		return nil, err
	}
	// If the reviewer didn't emit the wrapped contract ({"summary":...,"verdicts":[{"draft_id",
	// "verdict":"PASS"|"FAIL","issues":[...]}]}), it most often returned the FLAT single-verdict
	// critique shape instead — {"verdict":"pass"|"fail"|"needs_revision", and one of
	// "issues"/"findings"/"blocking":[...]}. Recover THAT as a single unnamed-draft verdict rather
	// than discarding the whole reviewer as a parse_error (the old behavior, which threw away a real
	// PASS/FAIL — agy/composer/grok routinely emit the flat shape). The false-green guard still holds:
	// we extract the actual verdict, so a flat needs_revision/fail becomes a FAIL in the merge — it
	// does NOT silently pass. Only a response with no verdicts AND no normalizable flat verdict is a
	// genuine parse failure (kept loud as parse_error).
	if len(r.Verdicts) == 0 {
		fv, ok := parseFlatVerdict(s)
		if !ok {
			return nil, fmt.Errorf("response has no verdicts and no recognizable flat verdict (off-schema reviewer output?)")
		}
		r.Verdicts = []verdict{*fv}
	}
	return &r, nil
}

// parseFlatVerdict adapts the flat single-verdict critique shape a reviewer
// sometimes emits — {"verdict":"pass"|"fail"|"needs_revision", and one of
// "issues"/"findings"/"blocking"/"blocking_findings":[...]} — into a single
// unnamed-draft (DraftID="") verdict. It returns false when the verdict word can't
// be normalized to PASS/FAIL, so the caller keeps the loud parse_error rather than
// guessing a pass — preserving the merge-gating false-green guard.
func parseFlatVerdict(s string) (*verdict, bool) {
	var aux struct {
		Verdict          string            `json:"verdict"`
		Issues           []json.RawMessage `json:"issues"`
		Findings         []json.RawMessage `json:"findings"`
		Blocking         []json.RawMessage `json:"blocking"`
		BlockingFindings []json.RawMessage `json:"blocking_findings"`
	}
	if err := json.Unmarshal([]byte(s), &aux); err != nil {
		return nil, false
	}
	nv := normalizeVerdict(aux.Verdict)
	if nv == "" {
		return nil, false
	}
	raws := firstNonEmptyRaw(aux.Issues, aux.Findings, aux.Blocking, aux.BlockingFindings)
	issues := make([]string, 0, len(raws))
	for _, raw := range raws {
		if str := issueToString(raw); str != "" {
			issues = append(issues, str)
		}
	}
	return &verdict{DraftID: "", Verdict: nv, Issues: issues}, true
}

// firstNonEmptyRaw returns the first non-empty []json.RawMessage from the candidates.
func firstNonEmptyRaw(lists ...[]json.RawMessage) []json.RawMessage {
	for _, l := range lists {
		if len(l) > 0 {
			return l
		}
	}
	return nil
}

// normalizeVerdict maps a reviewer's free-form verdict word to the merge's
// canonical "PASS"/"FAIL", or "" if unrecognized. Fail-ish words are checked FIRST
// so "needs_revision" resolves to FAIL before any pass match. Case-insensitive and
// substring-based, so it also accepts the already-canonical "PASS"/"FAIL".
func normalizeVerdict(s string) string {
	l := strings.ToLower(strings.TrimSpace(s))
	switch {
	case l == "":
		return ""
	case strings.Contains(l, "fail"), strings.Contains(l, "revis"),
		strings.Contains(l, "reject"), strings.Contains(l, "block"):
		return "FAIL"
	case strings.Contains(l, "pass"), strings.Contains(l, "approve"),
		strings.Contains(l, "lgtm"):
		return "PASS"
	}
	return ""
}

// merge consolidates verdicts across N reviewers using the FAIL-OR rule:
// a draft is FAIL if ANY reviewer flagged it FAIL. Issues are clustered by
// overlap and attributed to every reviewer that raised them. `selected` is the
// canonical reviewer order — defines the prefix order on rendered issues.
func merge(parsed map[string]*reviewerResp, selected []reviewerSpec) mergedResp {
	reviewerOrder := []string{}
	for _, r := range selected {
		if _, ok := parsed[r.name]; ok {
			reviewerOrder = append(reviewerOrder, r.name)
		}
	}

	// Index verdicts: draft_id → reviewer → *verdict.
	type slot struct {
		perReviewer map[string]*verdict
	}
	draftOrder := []string{}
	slots := map[string]*slot{}
	for _, name := range reviewerOrder {
		r := parsed[name]
		for i := range r.Verdicts {
			v := &r.Verdicts[i]
			s, ok := slots[v.DraftID]
			if !ok {
				draftOrder = append(draftOrder, v.DraftID)
				s = &slot{perReviewer: map[string]*verdict{}}
				slots[v.DraftID] = s
			}
			s.perReviewer[name] = v
		}
	}

	out := mergedResp{Verdicts: []verdict{}}
	anyFail := false
	for _, id := range draftOrder {
		s := slots[id]
		v := verdict{DraftID: id, Verdict: "PASS", Issues: []string{}}
		for _, rn := range reviewerOrder {
			if rv, ok := s.perReviewer[rn]; ok && rv.Verdict == "FAIL" {
				v.Verdict = "FAIL"
				anyFail = true
				break
			}
		}
		v.Issues = clusterIssues(s.perReviewer, reviewerOrder)
		out.Verdicts = append(out.Verdicts, v)
	}
	if anyFail {
		out.Summary = "some_fail"
	} else {
		out.Summary = "all_pass"
	}
	return out
}

// clusterIssues collects every (reviewer, issue) pair, clusters overlapping
// issues, and renders each cluster as "[r1+r2+...] <issue text>" in canonical
// reviewer order.
func clusterIssues(perReviewer map[string]*verdict, reviewerOrder []string) []string {
	type cluster struct {
		reviewers map[string]bool
		text      string
	}
	clusters := []*cluster{}

	add := func(reviewer, issue string) {
		for _, c := range clusters {
			if issueOverlaps(c.text, issue) {
				c.reviewers[reviewer] = true
				return
			}
		}
		clusters = append(clusters, &cluster{
			reviewers: map[string]bool{reviewer: true},
			text:      issue,
		})
	}

	for _, rn := range reviewerOrder {
		v, ok := perReviewer[rn]
		if !ok || v == nil {
			continue
		}
		for _, issue := range v.Issues {
			add(rn, issue)
		}
	}

	out := []string{}
	for _, c := range clusters {
		ordered := []string{}
		for _, rn := range reviewerOrder {
			if c.reviewers[rn] {
				ordered = append(ordered, rn)
			}
		}
		out = append(out, "["+strings.Join(ordered, "+")+"] "+c.text)
	}
	return out
}

// issueOverlaps returns true when two issue strings appear to flag the same
// underlying problem: identical (case-insensitive) OR a 12-char substring of
// one appears verbatim in the other.
func issueOverlaps(a, b string) bool {
	la := strings.ToLower(a)
	lb := strings.ToLower(b)
	if la == lb {
		return true
	}
	const window = 12
	if len(la) < window || len(lb) < window {
		return false
	}
	for i := 0; i+window <= len(la); i++ {
		if strings.Contains(lb, la[i:i+window]) {
			return true
		}
	}
	return false
}

func emit(out mergedResp) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintf(os.Stderr, "audit: emit json: %v\n", err)
	}
}

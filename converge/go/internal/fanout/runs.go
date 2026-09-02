// Resumable / recoverable audit runs.
//
// Every fresh `converge audit` writes a durable per-run directory under
// $CONVERGE_RUNS_DIR (default $HOME/.converge/runs/<run-id>/), where run-id ==
// the ledger audit_id. The directory accumulates, the instant each reviewer
// returns:
//
//	meta.json        {run_id, ts, cwd, reviewers_requested, timeout, deadline,
//	                  prompt_sha256, prompt}
//	prompt.txt       the composed prompt (durable copy for re-dispatch)
//	<name>.json      the reviewer's raw stdout (the verdict, pre-parse)
//	<name>.err       the reviewer's error text, when it failed
//	<name>.session   the reviewer's session/thread id (claude/codex/muse;
//	                  muse records the id but has no headless transcript
//	                  reader, so it recovers like agy)
//
// This makes a wall-clock kill non-destructive:
//
//   - `audit --resume <run-id>`  re-dispatches ONLY the reviewers whose
//     persisted output is missing or unparseable, then merges the union.
//   - `audit --recover <run-id>` salvages WITHOUT re-running: it reads each
//     <name>.json, and for a reviewer with no usable file it locates that
//     reviewer's OWN CLI transcript (claude/codex/cursor) and extracts the final
//     assistant message. agy writes no transcript, and muse has no headless
//     transcript reader, so both are non-recoverable (resumable via --resume).
//   - `audit --list-runs`        enumerates the run dirs newest-first with
//     per-run completeness.
package fanout

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// auditMode is the routing decision made by preparseMode.
type auditMode int

const (
	modeFresh auditMode = iota
	modeResume
	modeRecover
	modeListRuns
)

// preparseMode peels the resumable/recoverable MODE flags off the raw args
// BEFORE the normal flag block, returning the remaining args for flag.Parse.
// Only one mode may be requested. `--resume`/`--recover` consume the following
// token as the run-id.
func preparseMode(args []string) (auditMode, string, []string, error) {
	mode := modeFresh
	id := ""
	rest := make([]string, 0, len(args))
	set := func(m auditMode) error {
		if mode != modeFresh {
			return fmt.Errorf("choose only one of --resume / --recover / --list-runs")
		}
		mode = m
		return nil
	}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--list-runs":
			if err := set(modeListRuns); err != nil {
				return mode, id, nil, err
			}
		case "--resume", "--recover":
			m := modeResume
			if args[i] == "--recover" {
				m = modeRecover
			}
			if err := set(m); err != nil {
				return mode, id, nil, err
			}
			if i+1 >= len(args) {
				return mode, id, nil, fmt.Errorf("%s requires a <run-id>", args[i])
			}
			id = args[i+1]
			i++
		default:
			rest = append(rest, args[i])
		}
	}
	return mode, id, rest, nil
}

// runMeta is the durable meta.json record for a run.
type runMeta struct {
	RunID              string `json:"run_id"`
	TS                 string `json:"ts"`
	Cwd                string `json:"cwd,omitempty"`
	ReviewersRequested string `json:"reviewers_requested"`
	TimeoutSec         int    `json:"timeout"`
	DeadlineSec        int    `json:"deadline,omitempty"`
	PromptSHA256       string `json:"prompt_sha256"`
	Prompt             string `json:"prompt"`
}

// runsBaseDir resolves the runs root: $CONVERGE_RUNS_DIR, else
// $HOME/.converge/runs (mirrors the ledger's $HOME/.converge convention).
func runsBaseDir() string {
	if p := os.Getenv("CONVERGE_RUNS_DIR"); p != "" {
		return p
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".converge", "runs")
	}
	return filepath.Join(os.TempDir(), ".converge", "runs")
}

func runDirFor(runID string) string { return filepath.Join(runsBaseDir(), runID) }

// prepareRunDir creates the run dir (0700) and writes meta.json + prompt.txt
// before any reviewer is dispatched.
func prepareRunDir(runID string, selected []reviewerSpec, promptPath string, timeoutSec, deadlineSec int) (string, error) {
	dir := runDirFor(runID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	promptBytes, err := os.ReadFile(promptPath)
	if err != nil {
		return dir, fmt.Errorf("read prompt: %w", err)
	}
	cwd, _ := os.Getwd()
	meta := runMeta{
		RunID:              runID,
		TS:                 time.Now().UTC().Format(time.RFC3339),
		Cwd:                cwd,
		ReviewersRequested: selectedNames(selected),
		TimeoutSec:         timeoutSec,
		DeadlineSec:        deadlineSec,
		PromptSHA256:       promptSHA256(promptPath),
		Prompt:             string(promptBytes),
	}
	b, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return dir, err
	}
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), b, 0o644); err != nil {
		return dir, err
	}
	// Durable prompt copy: --resume re-dispatches from this file so it never
	// depends on the original stdin/-prompt-file still existing.
	if err := os.WriteFile(filepath.Join(dir, "prompt.txt"), promptBytes, 0o644); err != nil {
		return dir, err
	}
	return dir, nil
}

// loadMeta reads a run's meta.json.
func loadMeta(dir string) (runMeta, error) {
	var m runMeta
	b, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return m, fmt.Errorf("parse meta.json: %w", err)
	}
	return m, nil
}

// loadPersistedVerdict reads <dir>/<name>.json and parses it as a reviewer
// verdict. Returns (resp, true) only when a non-empty file parses — the same
// bar the fresh path uses to call a reviewer "responded".
func loadPersistedVerdict(dir, name string) (*reviewerResp, bool) {
	b, err := os.ReadFile(filepath.Join(dir, name+".json"))
	if err != nil {
		return nil, false
	}
	if strings.TrimSpace(string(b)) == "" {
		return nil, false
	}
	resp, perr := parseResponse(string(b))
	if perr != nil {
		return nil, false
	}
	return resp, true
}

// resumePromptPath returns a prompt file path usable for re-dispatch: the
// durable prompt.txt when present, else a temp file materialized from
// meta.Prompt. The returned cleanup removes the temp file (no-op for prompt.txt).
func resumePromptPath(dir string, meta runMeta) (string, func()) {
	p := filepath.Join(dir, "prompt.txt")
	if _, err := os.Stat(p); err == nil {
		return p, func() {}
	}
	tmp, err := os.CreateTemp("", "converge-resume-prompt-*.txt")
	if err != nil {
		return "", func() {}
	}
	_, _ = tmp.WriteString(meta.Prompt)
	_ = tmp.Close()
	return tmp.Name(), func() { _ = os.Remove(tmp.Name()) }
}

// resumeOpts carries the flags a --resume re-dispatch needs.
type resumeOpts struct {
	timeoutSec  int
	deadlineSec int
	quiet       bool
	noLedger    bool
	label       string
	start       time.Time
}

// resumeRun reconstructs the merge for <run-id>, re-dispatching only the
// reviewers whose persisted output is missing or unparseable. Idempotent: run it
// again once all reviewers are complete and it re-dispatches nothing.
func resumeRun(runID string, o resumeOpts) int {
	dir := runDirFor(runID)
	meta, err := loadMeta(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "audit: --resume %s: %v\n", runID, err)
		return 2
	}
	selected, err := selectReviewers(meta.ReviewersRequested)
	if err != nil {
		fmt.Fprintf(os.Stderr, "audit: --resume %s: %v\n", runID, err)
		return 2
	}
	promptPath, cleanup := resumePromptPath(dir, meta)
	defer cleanup()

	out := freshOut()
	parsed := map[string]*reviewerResp{}
	latency := map[string]int64{}
	toRun := make([]reviewerSpec, 0, len(selected))
	for _, r := range selected {
		if resp, ok := loadPersistedVerdict(dir, r.name); ok {
			parsed[r.name] = resp
			continue
		}
		toRun = append(toRun, r)
	}

	fmt.Fprintf(os.Stderr, "run-id: %s\n", runID)
	fmt.Fprintf(os.Stderr, "resume: %d/%d complete on disk, re-dispatching %d\n", len(parsed), len(selected), len(toRun))

	doneCh := dispatchAndCollect(toRun, dir, promptPath, o.timeoutSec, o.deadlineSec, o.quiet, out, parsed, latency)
	code := finalize(runID, out, parsed, selected, promptPath, o.label, o.noLedger, latency, o.start, o.quiet)
	waitGrace(doneCh)
	return code
}

// recoverOpts carries the flags a --recover salvage needs.
type recoverOpts struct {
	quiet    bool
	noLedger bool
	label    string
	start    time.Time
}

// recoverRun salvages <run-id> WITHOUT re-running any reviewer. Per reviewer:
// use the persisted <name>.json if it parses; else (for claude/codex/cursor)
// locate the reviewer's own CLI transcript and extract its final assistant
// message; agy writes no transcript and muse has no headless transcript
// reader, so both degrade to skipped; anything else
// unreconstructable is skipped(unrecoverable).
func recoverRun(runID string, o recoverOpts) int {
	dir := runDirFor(runID)
	meta, err := loadMeta(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "audit: --recover %s: %v\n", runID, err)
		return 2
	}
	selected, err := selectReviewers(meta.ReviewersRequested)
	if err != nil {
		fmt.Fprintf(os.Stderr, "audit: --recover %s: %v\n", runID, err)
		return 2
	}

	// prompt.txt (when present) gives the ledger an honest prompt_sha256; recover
	// never re-dispatches so a missing prompt is non-fatal.
	promptPath := filepath.Join(dir, "prompt.txt")
	if _, statErr := os.Stat(promptPath); statErr != nil {
		promptPath = ""
	}

	out := freshOut()
	parsed := map[string]*reviewerResp{}
	latency := map[string]int64{} // recover has no live latency to report
	usedTranscripts := map[string]bool{}

	fmt.Fprintf(os.Stderr, "run-id: %s\n", runID)
	for _, r := range selected {
		// 1. Persisted stdout — the fast path, works for every reviewer.
		if resp, ok := loadPersistedVerdict(dir, r.name); ok {
			parsed[r.name] = resp
			continue
		}
		// 2. agy writes no transcript of its own, and muse has no headless
		// transcript reader (`muse resume` is interactive) — both are
		// intrinsically non-recoverable; use --resume to re-run them.
		if r.cli == "agy" || r.cli == "muse" {
			out.Skipped[r.name] = fmt.Sprintf("no transcript — %s non-recoverable", r.cli)
			continue
		}
		// 3. The reviewer's own CLI transcript.
		if text, ok := recoverFromTranscript(r, dir, meta, usedTranscripts); ok {
			if resp, perr := parseResponse(text); perr == nil {
				parsed[r.name] = resp
				// Persist the salvaged verdict so the run dir becomes complete and
				// a subsequent --resume/--recover is idempotent.
				_ = os.WriteFile(filepath.Join(dir, r.name+".json"), []byte(text), 0o644)
				fmt.Fprintf(os.Stderr, "recover: %s reconstructed from transcript\n", r.name)
				continue
			}
		}
		// 4. Nothing usable on disk or in a transcript.
		out.Skipped[r.name] = "unrecoverable (no persisted output or transcript)"
	}

	return finalize(runID, out, parsed, selected, promptPath, o.label, o.noLedger, latency, o.start, o.quiet)
}

// recoverFromTranscript locates the reviewer's own CLI transcript and extracts
// its final assistant message. Locators:
//
//	claude  ~/.claude/projects/<cwd-slug>/<session-id>.jsonl      (by session id)
//	codex   ~/.codex/sessions/YYYY/MM/DD/rollout-*-<thread-id>.jsonl (by thread id)
//	cursor  ~/.cursor/projects/<slug>/agent-transcripts/<chat-id>/<chat-id>.jsonl
//	        (the `agent` text-mode provider captures no chat-id, so cursor is
//	         located by time window — best-effort, see recoverFromTimeWindow)
//
// Session/thread ids are read from <dir>/<name>.session (repointed ThreadOut).
func recoverFromTranscript(r reviewerSpec, dir string, meta runMeta, used map[string]bool) (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}
	switch r.cli {
	case "claude":
		sid := readSession(dir, r.name)
		if sid == "" {
			return "", false
		}
		return extractFromGlob(filepath.Join(home, ".claude", "projects", "*", sid+".jsonl"))
	case "codex":
		tid := readSession(dir, r.name)
		if tid == "" {
			return "", false
		}
		return extractFromGlob(filepath.Join(home, ".codex", "sessions", "*", "*", "*", "rollout-*-"+tid+".jsonl"))
	case "agent":
		return recoverFromTimeWindow(home, meta, used)
	}
	return "", false
}

// readSession returns the trimmed <dir>/<name>.session contents, or "".
func readSession(dir, name string) string {
	b, err := os.ReadFile(filepath.Join(dir, name+".session"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// extractFromGlob tries each glob match (newest first) and returns the first
// transcript that yields a final assistant message.
func extractFromGlob(pattern string) (string, bool) {
	matches, _ := filepath.Glob(pattern)
	sortByMTimeDesc(matches)
	for _, m := range matches {
		if text, ok := extractFinalAssistant(m); ok {
			return text, true
		}
	}
	return "", false
}

// recoverFromTimeWindow finds a cursor `agent` transcript whose mtime falls in
// the run's execution window and extracts its final assistant message. Because
// the text-mode `agent` provider records no chat-id, this cannot bind a specific
// transcript to a specific cursor reviewer (composer-2.5 vs grok-build vs kimi
// vs glm) — it consumes the newest UNUSED in-window transcript, tracking `used`
// so two cursor reviewers never claim the same file. Best-effort by design; the
// persisted <name>.json is the reliable cursor recovery path.
func recoverFromTimeWindow(home string, meta runMeta, used map[string]bool) (string, bool) {
	start, err := time.Parse(time.RFC3339, meta.TS)
	if err != nil {
		return "", false
	}
	slack := time.Duration(meta.TimeoutSec)*time.Second + 5*time.Minute
	lo, hi := start.Add(-2*time.Minute), start.Add(slack)

	pattern := filepath.Join(home, ".cursor", "projects", "*", "agent-transcripts", "*", "*.jsonl")
	matches, _ := filepath.Glob(pattern)

	type cand struct {
		path string
		mt   time.Time
	}
	cands := make([]cand, 0, len(matches))
	for _, m := range matches {
		if used[m] {
			continue
		}
		mt := fileMTime(m)
		if mt.Before(lo) || mt.After(hi) {
			continue
		}
		cands = append(cands, cand{m, mt})
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].mt.After(cands[j].mt) })
	for _, c := range cands {
		if text, ok := extractFinalAssistant(c.path); ok {
			used[c.path] = true
			return text, true
		}
	}
	return "", false
}

// extractFinalAssistant scans a reviewer CLI transcript (JSONL) and returns the
// LAST assistant message's text. It tolerates all three transcript shapes:
//
//	claude  {"type":"assistant","message":{"content":[{"type":"text","text":…}]}}
//	codex   {"type":"response_item","payload":{"role":"assistant",
//	          "content":[{"type":"output_text","text":…}]}}
//	cursor  {"role":"assistant","message":{"content":[{"type":"text","text":…}]}}
func extractFinalAssistant(path string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 32*1024*1024) // transcripts can carry large tool blocks
	last := ""
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] != '{' {
			continue
		}
		if text, ok := assistantTextFromLine([]byte(line)); ok && text != "" {
			last = text
		}
	}
	if last == "" {
		return "", false
	}
	return last, true
}

// assistantTextFromLine returns an assistant message's text from one transcript
// line, across the claude / codex / cursor envelope shapes.
func assistantTextFromLine(b []byte) (string, bool) {
	var env struct {
		Type    string          `json:"type"`
		Role    string          `json:"role"`
		Message json.RawMessage `json:"message"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(b, &env); err != nil {
		return "", false
	}
	// codex: response_item -> payload{role:assistant, content:[...]}
	if env.Type == "response_item" && len(env.Payload) > 0 {
		if t, ok := textFromMessageObj(env.Payload); ok {
			return t, true
		}
	}
	// claude: type==assistant -> message{content:[...]}
	if env.Type == "assistant" && len(env.Message) > 0 {
		if t, ok := textFromMessageObj(env.Message); ok {
			return t, true
		}
	}
	// cursor: role==assistant -> message{content:[...]}
	if env.Role == "assistant" && len(env.Message) > 0 {
		if t, ok := textFromMessageObj(env.Message); ok {
			return t, true
		}
	}
	return "", false
}

// textFromMessageObj pulls readable text out of a message object whose content
// is a string OR an array of {type,text} blocks (text | output_text). A role
// field, when present, must be "assistant".
func textFromMessageObj(raw json.RawMessage) (string, bool) {
	var m struct {
		Role    string          `json:"role"`
		Text    string          `json:"text"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return "", false
	}
	if m.Role != "" && m.Role != "assistant" {
		return "", false
	}
	if len(m.Content) == 0 {
		if m.Text != "" {
			return m.Text, true
		}
		return "", false
	}
	// content as a bare string
	var s string
	if json.Unmarshal(m.Content, &s) == nil && strings.TrimSpace(s) != "" {
		return s, true
	}
	// content as an array of typed blocks
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(m.Content, &blocks) == nil {
		var sb strings.Builder
		for _, bl := range blocks {
			if (bl.Type == "text" || bl.Type == "output_text") && bl.Text != "" {
				if sb.Len() > 0 {
					sb.WriteString("\n")
				}
				sb.WriteString(bl.Text)
			}
		}
		if sb.Len() > 0 {
			return sb.String(), true
		}
	}
	return "", false
}

// listRuns enumerates the run dirs newest-first with per-run completeness.
func listRuns(w io.Writer) int {
	base := runsBaseDir()
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintln(w, "(no runs recorded yet)")
			return 0
		}
		fmt.Fprintf(os.Stderr, "audit: --list-runs: %v\n", err)
		return 1
	}

	type row struct {
		id, ts, reviewers string
		complete, total   int
	}
	rows := make([]row, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(base, e.Name())
		meta, err := loadMeta(dir)
		if err != nil {
			continue
		}
		selected, _ := selectReviewers(meta.ReviewersRequested)
		complete := 0
		for _, r := range selected {
			if _, ok := loadPersistedVerdict(dir, r.name); ok {
				complete++
			}
		}
		rows = append(rows, row{id: meta.RunID, ts: meta.TS, reviewers: meta.ReviewersRequested, complete: complete, total: len(selected)})
	}
	// RFC3339 timestamps sort lexically; newest first.
	sort.Slice(rows, func(i, j int) bool { return rows[i].ts > rows[j].ts })

	if len(rows) == 0 {
		fmt.Fprintln(w, "(no runs recorded yet)")
		return 0
	}
	fmt.Fprintf(w, "%-34s %-20s %-9s %s\n", "RUN_ID", "TS", "COMPLETE", "REVIEWERS")
	for _, r := range rows {
		fmt.Fprintf(w, "%-34s %-20s %-9s %s\n", r.id, r.ts, fmt.Sprintf("%d/%d", r.complete, r.total), r.reviewers)
	}
	return 0
}

// fileMTime returns a path's modification time, or the zero time on error.
func fileMTime(path string) time.Time {
	fi, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return fi.ModTime()
}

// sortByMTimeDesc orders paths newest-modified first, in place.
func sortByMTimeDesc(paths []string) {
	sort.Slice(paths, func(i, j int) bool { return fileMTime(paths[i]).After(fileMTime(paths[j])) })
}

// Command adversarial-review dispatches the SAME prompt to every selected
// reviewer CLI in parallel (claude, codex, agy by default; agent opt-in),
// parses each reviewer's JSON verdict, and emits a merged canonical response.
//
// Usage:
//
//	cat prompt.txt | adversarial-review
//	adversarial-review --prompt-file prompt.txt
//
// The merged response shape (see SKILL.md for the contract):
//
//	{
//	  "summary": "all_pass" | "some_fail" | "parse_error",
//	  "verdicts": [
//	    {"draft_id": "<id>", "verdict": "PASS"|"FAIL",
//	     "issues": ["[claude] ...", "[codex] ...", "[claude+agent] ...", "[claude+codex+agent] ..."]}
//	  ],
//	  "reviewers": ["claude", "codex", "agent"],
//	  "skipped": {"<reviewer>": "<reason>"},
//	  "parse_error": ["<reviewer>", ...],
//	  "error": string, "raw_response": string
//	}
//
// Merge rule: a draft is FAIL if ANY reviewer flagged it FAIL.
// Issues are clustered by overlap and prefixed with the reviewers that raised
// them, e.g. "[claude+codex] ..." when both flag the same problem.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

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

// reviewerSpec describes one reviewer the binary knows how to dispatch.
// Order in `registeredReviewers` is the canonical reviewer order — also the
// order that issue attribution is rendered in (e.g. "[claude+codex+agent]").
type reviewerSpec struct {
	name string
	cli  string // CLI name on PATH
	make func() provider.Provider
}

// registeredReviewers is every provider the binary knows how to dispatch.
// Order here is the canonical reviewer order used for issue attribution.
var registeredReviewers = []reviewerSpec{
	{name: "claude", cli: "claude", make: func() provider.Provider { return claude.New() }},
	{name: "codex", cli: "codex", make: func() provider.Provider { return codex.New() }},
	{name: "agent", cli: "agent", make: func() provider.Provider { return agent.New() }},
	{name: "agy", cli: "agy", make: func() provider.Provider { return agy.New() }},
}

// defaultReviewers is the comma-separated default for the --reviewers flag.
//
// Default = claude + codex + agy: three independent agent families catch
// different failure modes (Claude: tone/voice/CTA; Codex: logical & structural
// inconsistency; agy: a third perspective that has, in practice, caught
// stragglers the other two missed), and all three are reliable enough to run
// every time. (`agy` replaced the deprecated `gemini` provider.)
//
// `agent` (Cursor) is registered but OPT-IN: free/low-tier Cursor plans
// quota-fail on nearly every run, so including it by default just adds noise.
// Pass --reviewers claude,codex,agent,agy to add it for high-stakes drafts.
//
// Per-reviewer failures degrade gracefully: a reviewer that quota-fails,
// auth-fails, or times out is reported under `skipped` (see unavailableReason),
// NOT `parse_error` — so the remaining reviewers still produce a merged verdict.
const defaultReviewers = "claude,codex,agy"

func main() {
	var promptFile string
	var timeoutSec int
	var quiet bool
	var reviewersCSV string
	flag.StringVar(&promptFile, "prompt-file", "", "path to prompt file; if empty, read from stdin")
	flag.IntVar(&timeoutSec, "timeout", 300, "per-reviewer timeout (seconds)")
	flag.BoolVar(&quiet, "quiet", false, "suppress provider heartbeat lines on stderr")
	flag.StringVar(&reviewersCSV, "reviewers", defaultReviewers,
		"comma-separated reviewers to dispatch (registered: claude,codex,agent,agy)")
	flag.Parse()

	selected, err := selectReviewers(reviewersCSV)
	if err != nil {
		die("%v", err)
	}

	promptPath, cleanup, err := resolvePromptPath(promptFile)
	if err != nil {
		die("prompt input: %v", err)
	}
	defer cleanup()

	out := mergedResp{
		Verdicts:  []verdict{},
		Reviewers: []string{},
		Skipped:   map[string]string{},
	}

	type result struct {
		name string
		out  string
		err  error
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
			s, e := runProvider(r.make(), promptPath, timeoutSec, quiet)
			resultsCh <- result{name: r.name, out: s, err: e}
		}()
	}
	wg.Wait()
	close(resultsCh)

	parsed := map[string]*reviewerResp{}
	for res := range resultsCh {
		if res.err != nil {
			// A reviewer that was dispatched but couldn't produce a verdict for
			// reasons outside the audit (quota, auth, timeout) is "skipped", not a
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
		emit(out)
		os.Exit(2)
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
	emit(out)
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
	tmp, err := os.CreateTemp("", "adversarial-review-prompt-*.txt")
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

func runProvider(p provider.Provider, promptPath string, timeoutSec int, quiet bool) (string, error) {
	var buf strings.Builder
	opts := provider.Options{
		PromptFile: promptPath,
		Timeout:    time.Duration(timeoutSec) * time.Second,
		Quiet:      quiet,
		Stdout:     &buf,
		Stderr:     os.Stderr,
		ThreadOut:  filepath.Join(os.TempDir(), fmt.Sprintf("ar-%s-%d.thread", p.Name(), os.Getpid())),
	}
	err := p.Run(context.Background(), opts)
	return buf.String(), err
}

// unavailableReason classifies a provider error as a graceful "skipped" — the
// reviewer was dispatched but couldn't produce a verdict for reasons outside
// the audit itself (quota/rate limit, auth, or timeout) — versus a genuine
// failure that belongs in parse_error. Returns (reason, true) when the error
// indicates unavailability. This keeps the merged output honest: a Cursor agent
// that hit its usage cap is reported as skipped, not as a malformed-JSON
// parse_error, and a reviewer that ran out of time on a huge prompt is "timed
// out" rather than looking like it returned garbage.
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

// parseResponse extracts the JSON verdict object from a reviewer's reply.
// Reviewers sometimes wrap JSON in markdown fences or surrounding prose;
// tolerate that by trimming fences and slicing between the first '{' and
// last '}'.
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
	return &r, nil
}

// merge consolidates verdicts across N reviewers using the FAIL-OR rule:
// a draft is FAIL if ANY reviewer flagged it FAIL. Issues are clustered by
// overlap and attributed to every reviewer that raised them.
//
// `selected` is the canonical reviewer order for this invocation — defines
// the prefix order on rendered issues (e.g. "[claude+codex]" vs "[codex+claude]").
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

// clusterIssues collects every (reviewer, issue) pair, clusters issues that
// overlap (same underlying problem flagged by multiple reviewers), and
// renders each cluster as "[r1+r2+...] <issue text>" using the canonical
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
// underlying problem. Heuristic: identical (case-insensitive) OR a 12-char
// substring of one appears verbatim in the other. Tuned to favor PASS-only
// dedup over false collapses (when in doubt, keep them separate).
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
		die("emit json: %v", err)
	}
}

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "adversarial-review: "+format+"\n", args...)
	os.Exit(2)
}

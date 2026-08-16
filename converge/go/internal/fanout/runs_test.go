package fanout

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// captureStdout runs fn with os.Stdout/os.Stderr redirected, returning what was
// written to stdout (stderr is discarded to keep test output clean).
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout, os.Stderr = wOut, wErr
	outCh := make(chan string)
	go func() { b, _ := io.ReadAll(rOut); outCh <- string(b) }()
	go func() { _, _ = io.Copy(io.Discard, rErr) }()

	fn()

	_ = wOut.Close()
	_ = wErr.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	return <-outCh
}

// seedRun writes a run dir with meta.json and the given <name>.json files.
func seedRun(t *testing.T, runID, reviewers string, files map[string]string) string {
	t.Helper()
	base := t.TempDir()
	t.Setenv("CONVERGE_RUNS_DIR", base)
	dir := filepath.Join(base, runID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	meta := runMeta{
		RunID:              runID,
		TS:                 time.Now().UTC().Format(time.RFC3339),
		ReviewersRequested: reviewers,
		TimeoutSec:         300,
		Prompt:             "review these drafts",
		PromptSHA256:       "deadbeef",
	}
	b, _ := json.MarshalIndent(meta, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "prompt.txt"), []byte(meta.Prompt), 0o644)
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name+".json"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestPreparseMode(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		wantMode auditMode
		wantID   string
		wantRest []string
		wantErr  bool
	}{
		{"fresh", []string{"--reviewers", "claude", "--quiet"}, modeFresh, "", []string{"--reviewers", "claude", "--quiet"}, false},
		{"list", []string{"--list-runs"}, modeListRuns, "", []string{}, false},
		{"resume", []string{"--resume", "abc123", "--quiet"}, modeResume, "abc123", []string{"--quiet"}, false},
		{"recover", []string{"--recover", "def456"}, modeRecover, "def456", []string{}, false},
		{"resume-missing-id", []string{"--resume"}, modeResume, "", nil, true},
		{"two-modes", []string{"--resume", "x", "--recover", "y"}, modeResume, "", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mode, id, rest, err := preparseMode(tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got mode=%v id=%q rest=%v", mode, id, rest)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if mode != tc.wantMode || id != tc.wantID {
				t.Fatalf("mode=%v id=%q, want mode=%v id=%q", mode, id, tc.wantMode, tc.wantID)
			}
			if strings.Join(rest, ",") != strings.Join(tc.wantRest, ",") {
				t.Fatalf("rest=%v, want %v", rest, tc.wantRest)
			}
		})
	}
}

func TestLoadPersistedVerdict(t *testing.T) {
	dir := seedRun(t, "r1", "claude,codex", map[string]string{
		"claude": `{"summary":"all_pass","verdicts":[{"draft_id":"a","verdict":"PASS","issues":[]}]}`,
		"codex":  `not json at all`, // failed/unparseable → treated as missing
	})
	if _, ok := loadPersistedVerdict(dir, "claude"); !ok {
		t.Errorf("claude verdict should load as complete")
	}
	if _, ok := loadPersistedVerdict(dir, "codex"); ok {
		t.Errorf("unparseable codex file must NOT count as complete (drives re-dispatch)")
	}
	if _, ok := loadPersistedVerdict(dir, "agy"); ok {
		t.Errorf("absent agy file must NOT count as complete")
	}
}

// TestResumeReconstructsMergeFromDisk is the core-bug regression: reviewers
// finished and persisted their verdicts, then a wall-clock kill discarded the
// unemitted merge. --resume rebuilds the identical FAIL-OR merge (with [r]
// attribution) purely from the persisted <name>.json files — nothing re-run.
func TestResumeReconstructsMergeFromDisk(t *testing.T) {
	seedRun(t, "run-abc", "claude,codex", map[string]string{
		"claude": `{"summary":"all_pass","verdicts":[{"draft_id":"a","verdict":"PASS","issues":[]}]}`,
		// codex flags the same draft FAIL with an object-shaped issue.
		"codex": `{"summary":"some_fail","verdicts":[{"id":"a","verdict":"FAIL","issues":[{"severity":"high","file":"x.go","line":1,"issue":"boom"}]}]}`,
	})

	out := captureStdout(t, func() {
		if code := resumeRun("run-abc", resumeOpts{timeoutSec: 300, noLedger: true, start: time.Now()}); code != 0 {
			t.Fatalf("resume exit code = %d, want 0", code)
		}
	})

	var got mergedResp
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("resume emit not valid JSON: %v\n%s", err, out)
	}
	if got.Summary != "some_fail" {
		t.Errorf("summary = %q, want some_fail (FAIL-OR)", got.Summary)
	}
	if len(got.Verdicts) != 1 || got.Verdicts[0].Verdict != "FAIL" {
		t.Fatalf("want one FAIL verdict, got %+v", got.Verdicts)
	}
	if len(got.Verdicts[0].Issues) == 0 || !strings.HasPrefix(got.Verdicts[0].Issues[0], "[codex]") {
		t.Errorf("issue attribution lost: %v", got.Verdicts[0].Issues)
	}
	if !strings.Contains(got.Verdicts[0].Issues[0], "boom") {
		t.Errorf("issue text lost: %v", got.Verdicts[0].Issues)
	}
	if strings.Join(got.Reviewers, ",") != "claude,codex" {
		t.Errorf("reviewers = %v, want [claude codex]", got.Reviewers)
	}
}

// TestResumeIdempotent: a second --resume over a fully-complete run re-dispatches
// nothing and emits the same merge.
func TestResumeIdempotent(t *testing.T) {
	seedRun(t, "run-idem", "claude", map[string]string{
		"claude": `{"summary":"all_pass","verdicts":[{"draft_id":"a","verdict":"PASS","issues":[]}]}`,
	})
	run := func() string {
		return captureStdout(t, func() {
			_ = resumeRun("run-idem", resumeOpts{timeoutSec: 300, noLedger: true, start: time.Now()})
		})
	}
	if a, b := run(), run(); a != b {
		t.Fatalf("resume not idempotent:\nfirst:  %s\nsecond: %s", a, b)
	}
}

// TestRecoverAgyDegradesToSkipped: --recover salvages persisted verdicts and
// degrades agy (which writes no transcript) to skipped, without re-running.
func TestRecoverAgyDegradesToSkipped(t *testing.T) {
	seedRun(t, "run-rec", "claude,agy", map[string]string{
		"claude": `{"summary":"all_pass","verdicts":[{"draft_id":"a","verdict":"PASS","issues":[]}]}`,
		// no agy.json, no agy transcript → must degrade to skipped
	})

	out := captureStdout(t, func() {
		if code := recoverRun("run-rec", recoverOpts{noLedger: true, start: time.Now()}); code != 0 {
			t.Fatalf("recover exit code = %d, want 0 (claude gave a verdict)", code)
		}
	})

	var got mergedResp
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("recover emit not valid JSON: %v\n%s", err, out)
	}
	reason, ok := got.Skipped["agy"]
	if !ok {
		t.Fatalf("agy must be skipped on recover, skipped=%v", got.Skipped)
	}
	if !strings.Contains(reason, "non-recoverable") {
		t.Errorf("agy skip reason = %q, want a non-recoverable note", reason)
	}
	if strings.Join(got.Reviewers, ",") != "claude" {
		t.Errorf("reviewers = %v, want [claude]", got.Reviewers)
	}
	if got.Summary != "all_pass" {
		t.Errorf("summary = %q, want all_pass", got.Summary)
	}
}

// TestExtractFinalAssistant covers the transcript extractor across all three CLI
// shapes and confirms it returns the LAST assistant message.
func TestExtractFinalAssistant(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	claudeFile := write("claude.jsonl",
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"draft one"}]}}`+"\n"+
			`{"type":"user","message":{"role":"user","content":"tool result"}}`+"\n"+
			`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"CLAUDE FINAL"}]}}`+"\n")
	if got, ok := extractFinalAssistant(claudeFile); !ok || got != "CLAUDE FINAL" {
		t.Errorf("claude: got (%q,%v), want (\"CLAUDE FINAL\", true)", got, ok)
	}

	codexFile := write("codex.jsonl",
		`{"type":"session_meta","payload":{"session_id":"x"}}`+"\n"+
			`{"type":"response_item","payload":{"type":"reasoning","summary":"thinking"}}`+"\n"+
			`{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"CODEX FINAL"}]}}`+"\n")
	if got, ok := extractFinalAssistant(codexFile); !ok || got != "CODEX FINAL" {
		t.Errorf("codex: got (%q,%v), want (\"CODEX FINAL\", true)", got, ok)
	}

	cursorFile := write("cursor.jsonl",
		`{"role":"user","message":{"content":[{"type":"text","text":"prompt"}]}}`+"\n"+
			`{"role":"assistant","message":{"content":[{"type":"text","text":"CURSOR FINAL"}]}}`+"\n")
	if got, ok := extractFinalAssistant(cursorFile); !ok || got != "CURSOR FINAL" {
		t.Errorf("cursor: got (%q,%v), want (\"CURSOR FINAL\", true)", got, ok)
	}

	// A transcript with no assistant message yields (,false).
	noneFile := write("none.jsonl", `{"type":"user","message":{"content":"hi"}}`+"\n")
	if got, ok := extractFinalAssistant(noneFile); ok {
		t.Errorf("no-assistant transcript: got (%q,%v), want ok=false", got, ok)
	}
}

// TestRecoverFromClaudeTranscript: no persisted claude.json, but a claude
// transcript exists at the session id captured in claude.session — recover
// reconstructs the verdict from it and persists it back (idempotency).
func TestRecoverFromClaudeTranscript(t *testing.T) {
	sid := "11111111-2222-3333-4444-555555555555"
	dir := seedRun(t, "run-tx", "claude", map[string]string{}) // no claude.json
	if err := os.WriteFile(filepath.Join(dir, "claude.session"), []byte(sid), 0o644); err != nil {
		t.Fatal(err)
	}
	// Fake the claude project transcript under a temp HOME.
	home := t.TempDir()
	t.Setenv("HOME", home)
	projDir := filepath.Join(home, ".claude", "projects", "-Users-someone-proj")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	verdict := `{"summary":"some_fail","verdicts":[{"draft_id":"a","verdict":"FAIL","issues":["missing citation"]}]}`
	transcript := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":` + strconv.Quote(verdict) + `}]}}` + "\n"
	if err := os.WriteFile(filepath.Join(projDir, sid+".jsonl"), []byte(transcript), 0o644); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		if code := recoverRun("run-tx", recoverOpts{noLedger: true, start: time.Now()}); code != 0 {
			t.Fatalf("recover exit code = %d, want 0", code)
		}
	})
	var got mergedResp
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("recover emit not valid JSON: %v\n%s", err, out)
	}
	if got.Summary != "some_fail" || len(got.Verdicts) != 1 || got.Verdicts[0].Verdict != "FAIL" {
		t.Fatalf("transcript-recovered verdict wrong: %+v", got)
	}
	// Salvaged verdict persisted back for idempotency.
	if _, ok := loadPersistedVerdict(dir, "claude"); !ok {
		t.Errorf("recover should persist the salvaged verdict to claude.json")
	}
}

func TestListRunsCompleteness(t *testing.T) {
	base := t.TempDir()
	t.Setenv("CONVERGE_RUNS_DIR", base)
	// Two runs sharing the base dir. seedRun sets CONVERGE_RUNS_DIR to its own
	// TempDir, so build these by hand under one base.
	mk := func(id, reviewers string, files map[string]string, ts string) {
		dir := filepath.Join(base, id)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		meta := runMeta{RunID: id, TS: ts, ReviewersRequested: reviewers, TimeoutSec: 300, Prompt: "p"}
		b, _ := json.MarshalIndent(meta, "", "  ")
		_ = os.WriteFile(filepath.Join(dir, "meta.json"), b, 0o644)
		for n, c := range files {
			_ = os.WriteFile(filepath.Join(dir, n+".json"), []byte(c), 0o644)
		}
	}
	pass := `{"summary":"all_pass","verdicts":[{"draft_id":"a","verdict":"PASS","issues":[]}]}`
	mk("run-older", "claude,codex", map[string]string{"claude": pass}, "2026-08-14T00:00:00Z")            // 1/2
	mk("run-newer", "claude,codex", map[string]string{"claude": pass, "codex": pass}, "2026-08-15T00:00:00Z") // 2/2

	out := captureStdout(t, func() {
		if code := listRuns(os.Stdout); code != 0 {
			t.Fatalf("list-runs exit = %d", code)
		}
	})
	if !strings.Contains(out, "run-newer") || !strings.Contains(out, "2/2") {
		t.Errorf("list-runs missing complete run:\n%s", out)
	}
	if !strings.Contains(out, "run-older") || !strings.Contains(out, "1/2") {
		t.Errorf("list-runs missing partial run:\n%s", out)
	}
	// Newest-first ordering.
	if strings.Index(out, "run-newer") > strings.Index(out, "run-older") {
		t.Errorf("list-runs not newest-first:\n%s", out)
	}
}

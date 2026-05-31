package fanout

import (
	"strings"
	"testing"
)

func TestParseResponse_Plain(t *testing.T) {
	r, err := parseResponse(`{"summary":"all_pass","verdicts":[{"draft_id":"x","verdict":"PASS","issues":[]}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if r.Summary != "all_pass" || len(r.Verdicts) != 1 || r.Verdicts[0].DraftID != "x" {
		t.Fatalf("bad parse: %#v", r)
	}
}

func TestParseResponse_FencedWithProse(t *testing.T) {
	in := "Here is my review:\n\n```json\n{\"summary\":\"some_fail\",\"verdicts\":[{\"draft_id\":\"a\",\"verdict\":\"FAIL\",\"issues\":[\"bad quote\"]}]}\n```\n\nLet me know.\n"
	r, err := parseResponse(in)
	if err != nil {
		t.Fatal(err)
	}
	if r.Summary != "some_fail" || r.Verdicts[0].Issues[0] != "bad quote" {
		t.Fatalf("bad parse: %#v", r)
	}
}

func TestParseResponse_NoJSON(t *testing.T) {
	if _, err := parseResponse("I'm sorry, I cannot help with that."); err == nil {
		t.Fatal("expected error")
	}
}

// TestParseResponse_OffSchemaNoVerdicts is the regression for the false-green bug: a reviewer
// that returns valid JSON in the flat critique shape ({"verdict":...,"issues":[...]}) — instead of
// the audit schema with a verdicts[] array — must be rejected as a parse failure. Otherwise it
// unmarshals to zero verdicts and merge() silently reports all_pass, dropping the reviewer's
// findings. (Observed live: codex returned needs_revision with 3 critical issues, yet the run
// reported all_pass.)
func TestParseResponse_OffSchemaNoVerdicts(t *testing.T) {
	cases := []string{
		`{"verdict":"needs_revision","summary":"bad","issues":[{"id":"R1","severity":"critical","title":"x"}]}`,
		`{"summary":"all_pass"}`,
		`{"summary":"all_pass","verdicts":[]}`,
	}
	for _, in := range cases {
		if _, err := parseResponse(in); err == nil {
			t.Fatalf("expected a parse error for off-schema/zero-verdict response: %s", in)
		}
	}
}

// TestParseResponse_CodexObjectIssues covers the codex shape: the id field arrives as
// "id" (not "draft_id") and each issue is a structured OBJECT, not a string. Before the
// tolerant verdict.UnmarshalJSON, this failed to parse and the whole reviewer was dropped.
func TestParseResponse_CodexObjectIssues(t *testing.T) {
	in := `{
	  "summary": "some_fail",
	  "verdicts": [
	    {"id": "1", "verdict": "PASS", "issues": []},
	    {"id": "2", "verdict": "FAIL", "issues": [
	      {"severity": "high", "file": "internal/x.go", "line": 114, "issue": "drops needs-review items"},
	      {"severity": "medium", "file": "internal/y.go", "line_start": 7, "message": "lossy mapping"}
	    ]}
	  ]
	}`
	r, err := parseResponse(in)
	if err != nil {
		t.Fatalf("codex object-issues must parse, got error: %v", err)
	}
	if len(r.Verdicts) != 2 {
		t.Fatalf("want 2 verdicts, got %d", len(r.Verdicts))
	}
	if r.Verdicts[0].DraftID != "1" || r.Verdicts[1].DraftID != "2" {
		t.Fatalf("the \"id\" field should populate DraftID: %#v", r.Verdicts)
	}
	got := r.Verdicts[1].Issues
	if len(got) != 2 {
		t.Fatalf("want 2 flattened issues, got %d: %#v", len(got), got)
	}
	if !strings.Contains(got[0], "[high]") || !strings.Contains(got[0], "internal/x.go:114") || !strings.Contains(got[0], "drops needs-review items") {
		t.Errorf("issue not flattened readably: %q", got[0])
	}
	if !strings.Contains(got[1], "internal/y.go:7") || !strings.Contains(got[1], "lossy mapping") {
		t.Errorf("line_start/message not handled: %q", got[1])
	}
}

// TestIssueToString covers the element renderer's branches directly.
func TestIssueToString(t *testing.T) {
	cases := map[string]string{
		`"plain string"`:                    "plain string",
		`null`:                              "",
		`{"issue":"bare issue"}`:            "bare issue",
		`{"severity":"low","title":"t"}`:    "[low] t",
		`{"file":"a.go","detail":"d"}`:      "a.go — d",
		`{"unknown":"shape"}`:               `{"unknown":"shape"}`, // no known text field → raw fallback
		`{"severity":"high","message":"m"}`: "[high] m",
	}
	for in, want := range cases {
		if got := issueToString([]byte(in)); got != want {
			t.Errorf("issueToString(%s) = %q, want %q", in, got, want)
		}
	}
}

// helper: build a selected []reviewerSpec from canonical names without
// invoking actual provider constructors.
func selectedFor(names ...string) []reviewerSpec {
	byName := map[string]reviewerSpec{}
	for _, r := range registeredReviewers {
		byName[r.name] = r
	}
	out := []reviewerSpec{}
	for _, n := range names {
		if r, ok := byName[n]; ok {
			out = append(out, r)
		}
	}
	return out
}

func TestMerge_AllPass_Default(t *testing.T) {
	parsed := map[string]*reviewerResp{
		"claude": {Verdicts: []verdict{
			{DraftID: "a", Verdict: "PASS", Issues: []string{}},
			{DraftID: "b", Verdict: "PASS", Issues: []string{}},
		}},
		"codex": {Verdicts: []verdict{
			{DraftID: "a", Verdict: "PASS", Issues: []string{}},
			{DraftID: "b", Verdict: "PASS", Issues: []string{}},
		}},
	}
	got := merge(parsed, selectedFor("claude", "codex"))
	if got.Summary != "all_pass" {
		t.Fatalf("want all_pass, got %s", got.Summary)
	}
	if len(got.Verdicts) != 2 {
		t.Fatalf("want 2 verdicts, got %d", len(got.Verdicts))
	}
}

func TestMerge_FailOR(t *testing.T) {
	parsed := map[string]*reviewerResp{
		"claude": {Verdicts: []verdict{
			{DraftID: "a", Verdict: "PASS", Issues: []string{}},
			{DraftID: "b", Verdict: "FAIL", Issues: []string{"claude-only flag"}},
		}},
		"codex": {Verdicts: []verdict{
			{DraftID: "a", Verdict: "FAIL", Issues: []string{"codex-only flag"}},
			{DraftID: "b", Verdict: "PASS", Issues: []string{}},
		}},
	}
	got := merge(parsed, selectedFor("claude", "codex"))
	if got.Summary != "some_fail" {
		t.Fatalf("want some_fail, got %s", got.Summary)
	}
	if got.Verdicts[0].Verdict != "FAIL" || got.Verdicts[1].Verdict != "FAIL" {
		t.Fatalf("FAIL-OR not enforced: %#v", got.Verdicts)
	}
	if !strings.HasPrefix(got.Verdicts[0].Issues[0], "[codex]") {
		t.Fatalf("want [codex] prefix, got %q", got.Verdicts[0].Issues[0])
	}
	if !strings.HasPrefix(got.Verdicts[1].Issues[0], "[claude]") {
		t.Fatalf("want [claude] prefix, got %q", got.Verdicts[1].Issues[0])
	}
}

func TestMerge_DedupOverlap(t *testing.T) {
	parsed := map[string]*reviewerResp{
		"claude": {Verdicts: []verdict{
			{DraftID: "a", Verdict: "FAIL", Issues: []string{"contains 'unverifiable claim about every leader'"}},
		}},
		"codex": {Verdicts: []verdict{
			{DraftID: "a", Verdict: "FAIL", Issues: []string{"unverifiable claim about every leader: not in source"}},
		}},
	}
	got := merge(parsed, selectedFor("claude", "codex"))
	if len(got.Verdicts[0].Issues) != 1 {
		t.Fatalf("want 1 deduped issue, got %d: %v", len(got.Verdicts[0].Issues), got.Verdicts[0].Issues)
	}
	if !strings.HasPrefix(got.Verdicts[0].Issues[0], "[claude+codex]") {
		t.Fatalf("want [claude+codex] prefix, got %q", got.Verdicts[0].Issues[0])
	}
}

func TestMerge_OnlyClaude(t *testing.T) {
	parsed := map[string]*reviewerResp{
		"claude": {Verdicts: []verdict{
			{DraftID: "a", Verdict: "PASS", Issues: []string{}},
		}},
	}
	got := merge(parsed, selectedFor("claude", "codex"))
	if got.Summary != "all_pass" {
		t.Fatalf("want all_pass, got %s", got.Summary)
	}
	if len(got.Verdicts) != 1 {
		t.Fatalf("want 1 verdict, got %d", len(got.Verdicts))
	}
}

func TestMerge_ThreeWayCluster(t *testing.T) {
	// All three flag the same problem (overlapping wording) → single
	// "[claude+codex+agy]" attribution.
	parsed := map[string]*reviewerResp{
		"claude": {Verdicts: []verdict{
			{DraftID: "a", Verdict: "FAIL", Issues: []string{"unverifiable claim about every leader"}},
		}},
		"codex": {Verdicts: []verdict{
			{DraftID: "a", Verdict: "FAIL", Issues: []string{"the unverifiable claim about every leader is not in source"}},
		}},
		"agy": {Verdicts: []verdict{
			{DraftID: "a", Verdict: "FAIL", Issues: []string{"contains an unverifiable claim about every leader"}},
		}},
	}
	got := merge(parsed, selectedFor("claude", "codex", "agy"))
	if len(got.Verdicts[0].Issues) != 1 {
		t.Fatalf("want 1 clustered issue, got %d: %v", len(got.Verdicts[0].Issues), got.Verdicts[0].Issues)
	}
	if !strings.HasPrefix(got.Verdicts[0].Issues[0], "[claude+codex+agy]") {
		t.Fatalf("want [claude+codex+agy] prefix, got %q", got.Verdicts[0].Issues[0])
	}
}

func TestSelectReviewers_Default(t *testing.T) {
	got, err := selectReviewers(defaultReviewers)
	if err != nil {
		t.Fatal(err)
	}
	// Default = claude + codex + agy + composer-2.5 + grok-build; the bare
	// `agent` provider stays opt-in.
	want := []string{"claude", "codex", "agy", "composer-2.5", "grok-build"}
	if len(got) != len(want) {
		t.Fatalf("default should be %v, got %d items", want, len(got))
	}
	for i, w := range want {
		if got[i].name != w {
			t.Fatalf("default at %d: want %s, got %s", i, w, got[i].name)
		}
	}
	for _, r := range got {
		if r.name == "agent" {
			t.Fatalf("agent must NOT be in the default selection (opt-in only)")
		}
	}
}

func TestSelectReviewers_CursorModels(t *testing.T) {
	got, err := selectReviewers("composer-2.5,grok-build")
	if err != nil {
		t.Fatal(err)
	}
	// Both route through the Cursor `agent` CLI, pinned to distinct models.
	want := map[string]string{"composer-2.5": "composer-2.5", "grok-build": "grok-build-0.1"}
	if len(got) != len(want) {
		t.Fatalf("want %d reviewers, got %d", len(want), len(got))
	}
	for _, r := range got {
		if r.cli != "agent" {
			t.Fatalf("%s should dispatch via the agent CLI, got cli=%q", r.name, r.cli)
		}
		if r.model != want[r.name] {
			t.Fatalf("%s: want model %q, got %q", r.name, want[r.name], r.model)
		}
	}
}

func TestSelectReviewers_OptInAgent(t *testing.T) {
	got, err := selectReviewers("claude,codex,agent")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[2].name != "agent" {
		t.Fatalf("want agent included, got %#v", got)
	}
}

func TestSelectReviewers_Agy(t *testing.T) {
	got, err := selectReviewers("claude,codex,agy")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[2].name != "agy" {
		t.Fatalf("want agy included, got %#v", got)
	}
}

func TestSelectReviewers_AllFour(t *testing.T) {
	got, err := selectReviewers("claude,codex,agent,agy")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"claude", "codex", "agent", "agy"}
	if len(got) != 4 {
		t.Fatalf("want 4 reviewers, got %d", len(got))
	}
	for i, r := range got {
		if r.name != want[i] {
			t.Fatalf("at %d: want %s, got %s", i, want[i], r.name)
		}
	}
}

func TestSelectReviewers_Unknown(t *testing.T) {
	if _, err := selectReviewers("claude,bogus"); err == nil {
		t.Fatal("expected error for unknown reviewer")
	}
}

func TestSelectReviewers_CustomOrder(t *testing.T) {
	got, err := selectReviewers("codex,claude")
	if err != nil {
		t.Fatal(err)
	}
	if got[0].name != "codex" || got[1].name != "claude" {
		t.Fatalf("want order [codex, claude], got %#v", got)
	}
}

func TestUnavailableReason(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantReason string
		wantOK     bool
	}{
		{"nil", nil, "", false},
		{"cursor usage limit", errString("no output from agent (stderr: S: You've hit your usage limit Get Cursor Pro)"), "usage/quota limit", true},
		{"quota", errString("error: quota exceeded for this project"), "usage/quota limit", true},
		{"429", errString("request failed: 429 Too Many Requests"), "usage/quota limit", true},
		{"auth", errString("not logged in; run `claude login`"), "auth/login required", true},
		{"401", errString("HTTP 401 unauthorized"), "auth/login required", true},
		{"timeout", errString("claude timed out after 9m0s"), "timed out", true},
		{"genuine bad output", errString("unexpected end of JSON input"), "", false},
	}
	for _, tc := range cases {
		gotReason, gotOK := unavailableReason(tc.err)
		if gotOK != tc.wantOK || gotReason != tc.wantReason {
			t.Errorf("%s: unavailableReason = (%q, %v), want (%q, %v)", tc.name, gotReason, gotOK, tc.wantReason, tc.wantOK)
		}
	}
}

// errString is a tiny error helper for table tests.
type errString string

func (e errString) Error() string { return string(e) }

func TestParseIssue(t *testing.T) {
	cases := []struct {
		name       string
		issue      string
		wantSev    string
		wantTitle  string
		wantLoc    string
		wantRaised string
	}{
		{
			name:       "full shape em-dash and loc",
			issue:      "[claude+codex+grok-build] HIGH: drops needs-review items — silently maps to PASS (internal/x.go:114)",
			wantSev:    "HIGH",
			wantTitle:  "drops needs-review items",
			wantLoc:    "internal/x.go:114",
			wantRaised: "claude+codex+grok-build",
		},
		{
			name:       "title ends at first paren when no em-dash",
			issue:      "[codex] MEDIUM: lossy mapping (internal/y.go:7)",
			wantSev:    "MEDIUM",
			wantTitle:  "lossy mapping",
			wantLoc:    "internal/y.go:7",
			wantRaised: "codex",
		},
		{
			name:       "no loc",
			issue:      "[claude] LOW: minor nit — cosmetic",
			wantSev:    "LOW",
			wantTitle:  "minor nit",
			wantLoc:    "",
			wantRaised: "claude",
		},
		{
			name:       "off-shape issue still recorded with empty sev/raised_by",
			issue:      "freeform note without the bracket/severity shape",
			wantSev:    "",
			wantTitle:  "freeform note without the bracket/severity shape",
			wantLoc:    "",
			wantRaised: "",
		},
		{
			name:       "critical with loc but no em-dash or paren in title",
			issue:      "[grok-build] CRITICAL: nil deref (pkg/a.go:42)",
			wantSev:    "CRITICAL",
			wantTitle:  "nil deref",
			wantLoc:    "pkg/a.go:42",
			wantRaised: "grok-build",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseIssue(tc.issue)
			if got.Severity != tc.wantSev {
				t.Errorf("severity = %q, want %q", got.Severity, tc.wantSev)
			}
			if got.Title != tc.wantTitle {
				t.Errorf("title = %q, want %q", got.Title, tc.wantTitle)
			}
			if got.Loc != tc.wantLoc {
				t.Errorf("loc = %q, want %q", got.Loc, tc.wantLoc)
			}
			if got.RaisedBy != tc.wantRaised {
				t.Errorf("raised_by = %q, want %q", got.RaisedBy, tc.wantRaised)
			}
			if got.FindingID == "" || len(got.FindingID) != 16 {
				t.Errorf("finding_id should be 16 hex chars, got %q", got.FindingID)
			}
		})
	}
}

func TestIssueOverlaps(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"hello world is great", "hello world is great", true},
		{"unverifiable claim about leaders", "the unverifiable claim about leaders flagged", true},
		{"short", "differ", false},
		{"completely different topic A", "totally unrelated topic Z", false},
	}
	for _, tc := range cases {
		got := issueOverlaps(tc.a, tc.b)
		if got != tc.want {
			t.Errorf("issueOverlaps(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

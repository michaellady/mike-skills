package main

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
	// "[claude+codex+agent]" attribution.
	parsed := map[string]*reviewerResp{
		"claude": {Verdicts: []verdict{
			{DraftID: "a", Verdict: "FAIL", Issues: []string{"unverifiable claim about every leader"}},
		}},
		"codex": {Verdicts: []verdict{
			{DraftID: "a", Verdict: "FAIL", Issues: []string{"the unverifiable claim about every leader is not in source"}},
		}},
		"agent": {Verdicts: []verdict{
			{DraftID: "a", Verdict: "FAIL", Issues: []string{"contains an unverifiable claim about every leader"}},
		}},
	}
	got := merge(parsed, selectedFor("claude", "codex", "agent"))
	if len(got.Verdicts[0].Issues) != 1 {
		t.Fatalf("want 1 clustered issue, got %d: %v", len(got.Verdicts[0].Issues), got.Verdicts[0].Issues)
	}
	if !strings.HasPrefix(got.Verdicts[0].Issues[0], "[claude+codex+agent]") {
		t.Fatalf("want [claude+codex+agent] prefix, got %q", got.Verdicts[0].Issues[0])
	}
}

func TestSelectReviewers_Default(t *testing.T) {
	got, err := selectReviewers(defaultReviewers)
	if err != nil {
		t.Fatal(err)
	}
	// Default = claude + codex + agy; agent is opt-in.
	want := []string{"claude", "codex", "agy"}
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

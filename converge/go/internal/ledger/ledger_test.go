package ledger

import (
	"path/filepath"
	"strings"
	"testing"
)

// withTempLedger points CONVERGE_LEDGER at a fresh temp DB for the duration of t.
func withTempLedger(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ledger.db")
	t.Setenv("CONVERGE_LEDGER", path)
	return path
}

func sampleRecord() AuditRecord {
	return AuditRecord{
		AuditID:            NewAuditID(),
		TS:                 "2026-05-31T12:00:00Z",
		Label:              "PR#999",
		Summary:            "some_fail",
		PromptSHA256:       "deadbeef",
		ReviewersRequested: "claude,codex",
		DurationMs:         1234,
		Reviews: []ReviewRow{
			{Model: "claude", Status: "responded", Verdict: "some_fail", LatencyMs: 800},
			{Model: "codex", Status: "skipped", Verdict: "", LatencyMs: 50},
		},
		Findings: []FindingRow{
			{
				FindingID: FindingID("internal/x.go:114", "drops needs-review items"),
				Severity:  "HIGH",
				Title:     "drops needs-review items",
				Loc:       "internal/x.go:114",
				RaisedBy:  "claude",
			},
			{
				FindingID: FindingID("internal/y.go:7", "lossy mapping"),
				Severity:  "MEDIUM",
				Title:     "lossy mapping",
				Loc:       "internal/y.go:7",
				RaisedBy:  "claude+codex",
			},
		},
	}
}

func TestRecordAndStats(t *testing.T) {
	withTempLedger(t)
	if err := Record(sampleRecord()); err != nil {
		t.Fatalf("Record: %v", err)
	}

	var sb strings.Builder
	if err := Stats(&sb); err != nil {
		t.Fatalf("Stats: %v", err)
	}
	out := sb.String()

	for _, want := range []string{"claude", "codex", "MODEL", "TOTAL", "PRECISION"} {
		if !strings.Contains(out, want) {
			t.Errorf("Stats output missing %q:\n%s", want, out)
		}
	}

	// claude participated in 1 audit and responded; codex participated and skipped.
	claudeLine := lineFor(out, "claude")
	if claudeLine == "" {
		t.Fatalf("no claude row in:\n%s", out)
	}
	// claude responded to 1 audit → response rate 100%.
	if !strings.Contains(claudeLine, "100%") {
		t.Errorf("claude row should show 100%% response rate: %q", claudeLine)
	}
	// claude raised one HIGH + one MEDIUM (the MEDIUM is claude+codex).
	codexLine := lineFor(out, "codex")
	if codexLine == "" {
		t.Fatalf("no codex row in:\n%s", out)
	}
	// codex skipped its only audit → 0% response rate.
	if !strings.Contains(codexLine, "0%") {
		t.Errorf("codex row should show 0%% response rate: %q", codexLine)
	}
}

func TestDispositionAffectsPrecision(t *testing.T) {
	withTempLedger(t)
	rec := sampleRecord()
	if err := Record(rec); err != nil {
		t.Fatalf("Record: %v", err)
	}

	// Before any disposition, precision is "-" (no scored findings).
	var before strings.Builder
	if err := Stats(&before); err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if !strings.Contains(lineFor(before.String(), "claude"), "-") {
		t.Errorf("claude precision should be '-' before disposition:\n%s", before.String())
	}

	// Mark claude's HIGH finding as fixed → precision becomes 100%.
	fixedID := FindingID("internal/x.go:114", "drops needs-review items")
	if err := Disposition(fixedID, "fixed", "patched in follow-up", "abc123"); err != nil {
		t.Fatalf("Disposition: %v", err)
	}

	var after strings.Builder
	if err := Stats(&after); err != nil {
		t.Fatalf("Stats: %v", err)
	}
	claudeLine := lineFor(after.String(), "claude")
	if !strings.Contains(claudeLine, "100%") {
		t.Errorf("claude precision should reflect the fix (100%%): %q", claudeLine)
	}

	// Now mark the MEDIUM (claude+codex) as a false positive. claude precision
	// over its two scored findings = 1 fixed / (1 fixed + 1 false_positive) = 50%.
	fpID := FindingID("internal/y.go:7", "lossy mapping")
	if err := Disposition(fpID, "false_positive", "", ""); err != nil {
		t.Fatalf("Disposition false_positive: %v", err)
	}
	var after2 strings.Builder
	if err := Stats(&after2); err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if !strings.Contains(lineFor(after2.String(), "claude"), "50%") {
		t.Errorf("claude precision should be 50%% after one fixed + one false_positive:\n%s", after2.String())
	}
}

func TestDispositionValidatesKind(t *testing.T) {
	withTempLedger(t)
	if err := Disposition("abcd", "bogus", "", ""); err == nil {
		t.Fatal("expected an error for an invalid disposition kind")
	}
	if err := Disposition("", "fixed", "", ""); err == nil {
		t.Fatal("expected an error for an empty finding_id")
	}
}

func TestFindingsListing(t *testing.T) {
	withTempLedger(t)
	if err := Record(sampleRecord()); err != nil {
		t.Fatalf("Record: %v", err)
	}
	// Disposition one finding so the listing shows it.
	fixedID := FindingID("internal/x.go:114", "drops needs-review items")
	if err := Disposition(fixedID, "fixed", "", ""); err != nil {
		t.Fatalf("Disposition: %v", err)
	}

	var sb strings.Builder
	if err := Findings(&sb, 10); err != nil {
		t.Fatalf("Findings: %v", err)
	}
	out := sb.String()
	for _, want := range []string{
		"internal/x.go:114",
		"drops needs-review items",
		"lossy mapping",
		"fixed", // the disposition column for the fixed finding
		fixedID,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Findings output missing %q:\n%s", want, out)
		}
	}
}

func TestFindingsEmpty(t *testing.T) {
	withTempLedger(t)
	var sb strings.Builder
	if err := Findings(&sb, 10); err != nil {
		t.Fatalf("Findings: %v", err)
	}
	if !strings.Contains(sb.String(), "no findings recorded") {
		t.Errorf("expected empty notice, got:\n%s", sb.String())
	}
}

func TestFindingIDStable(t *testing.T) {
	a := FindingID("file.go:1", "title")
	b := FindingID("file.go:1", "title")
	c := FindingID("file.go:2", "title")
	if a != b {
		t.Errorf("FindingID not stable: %q != %q", a, b)
	}
	if a == c {
		t.Errorf("FindingID should differ on loc: %q == %q", a, c)
	}
	if len(a) != 16 {
		t.Errorf("FindingID should be 16 hex chars, got %d: %q", len(a), a)
	}
}

// lineFor returns the first line in s whose first whitespace-delimited field
// equals model, or "" if none.
func lineFor(s, model string) string {
	for _, line := range strings.Split(s, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == model {
			return line
		}
	}
	return ""
}

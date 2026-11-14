package provider

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestQuotaSemantics(t *testing.T) {
	for _, tc := range []struct {
		raw     string
		want    int
		percent float64
	}{
		{`{"rateLimitsByLimitId":{"x":{"primary":{"usedPercent":80,"windowDurationMins":10080,"resetsAt":123}}}}`, 1, 80},
		{`{"rateLimits":{"primary":{"usedPercent":0,"windowDurationMins":10080}}}`, 1, 0},
		{`{"rateLimits":{"primary":{"usedPercent":99,"windowDurationMins":300}}}`, 0, 0},
		{`{"rateLimits":{"primary":{"windowDurationMins":10080}}}`, 0, 0},
		{`{"rateLimits":{"primary":{"usedPercent":101,"windowDurationMins":10080}}}`, 0, 0},
	} {
		q, _ := parseCodexWeekly([]byte(tc.raw))
		if len(q) != tc.want {
			t.Fatalf("%s: %+v", tc.raw, q)
		}
		if len(q) > 0 && q[0].UsedPercent != tc.percent {
			t.Fatal(q)
		}
	}
}

func TestMuseWeeklyNullZeroAndMilliseconds(t *testing.T) {
	q, status := parseMuseWeekly([]byte(`{"schemaVersion":1,"allowance":{"weekly":{"usedPercent":0,"resetsAtMs":1794787200000},"asOfMs":1794697897417}}`))
	if status != "reported" || len(q) != 1 || q[0].UsedPercent != 0 || *q[0].ResetsAt != 1794787200 || q[0].ObservedAt != 1794697897 {
		t.Fatal(q, status)
	}
	q, status = parseMuseWeekly([]byte(`{"schemaVersion":1,"allowance":null,"reason":"server_sent_no_usage"}`))
	if len(q) != 0 || status != "server_sent_no_usage" {
		t.Fatal(q, status)
	}
	q, status = parseMuseWeekly([]byte(`{"schemaVersion":1,"allowance":{"weekly":{"usedPercent":14}}}`))
	if len(q) != 0 || status == "reported" {
		t.Fatal("missing freshness was accepted")
	}
}
func TestNativeUsageIgnoresAssistantText(t *testing.T) {
	m := &Meter{Record: UsageRecord{Provider: "claude"}}
	m.Observe([]byte(`{"type":"assistant","usage":{"input_tokens":999,"output_tokens":999},"rate_limit_info":{"unifiedWindows":{"seven_day":{"utilization":1}}}}`))
	if m.Record.InputTokens != nil || len(m.Record.Weekly) > 0 {
		t.Fatal("accepted non-terminal usage")
	}
	m.Observe([]byte(`{"type":"result","usage":{"input_tokens":2,"cache_creation_input_tokens":10,"cache_read_input_tokens":20,"output_tokens":0}}`))
	if *m.Record.InputTokens != 32 || *m.Record.OutputTokens != 0 {
		t.Fatal(m.Record)
	}
	m.Observe([]byte(`{"type":"rate_limit_event","rate_limit_info":{"unifiedWindows":{"seven_day":{"utilization":0.81,"resetsAt":123},"five_hour":{"utilization":1}}}}`))
	if len(m.Record.Weekly) != 1 || m.Record.Weekly[0].UsedPercent != 81 {
		t.Fatal(m.Record)
	}
}
func TestConcurrentReceiptsAndAlerts(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CONVERGE_USAGE_DIR", dir)
	t.Setenv("CONVERGE_USAGE_WARN_PERCENT", "80")
	t.Setenv("CONVERGE_USAGE_CRITICAL_PERCENT", "95")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			opts := Options{Stderr: &bytes.Buffer{}, Model: "test", Effort: "medium"}
			finish := Monitor("claude", &opts)
			opts.Meter.Observe([]byte(`{"type":"rate_limit_event","rate_limit_info":{"unifiedWindows":{"seven_day":{"utilization":0.96}}}}`))
			finish(NewError(ExitTimeout, "SECRET raw diagnostic"))
		}()
	}
	wg.Wait()
	entries, e := os.ReadDir(dir)
	if e != nil || len(entries) != 8 {
		t.Fatalf("receipts: %v %d", e, len(entries))
	}
	for _, entry := range entries {
		b, e := os.ReadFile(filepath.Join(dir, entry.Name()))
		if e != nil {
			t.Fatal(e)
		}
		var r UsageRecord
		if json.Unmarshal(b, &r) != nil || r.Status != "timeout" || r.FinishedAt == nil {
			t.Fatal(string(b))
		}
		if len(r.Alerts) != 2 || r.Alerts[0] != "weekly_critical:seven_day" {
			t.Fatal(r.Alerts)
		}
		if strings.Contains(string(b), "SECRET") {
			t.Fatal("raw diagnostics leaked")
		}
		st, _ := os.Stat(filepath.Join(dir, entry.Name()))
		if st.Mode().Perm() != 0600 {
			t.Fatal(st.Mode())
		}
	}
	var out bytes.Buffer
	if UsageSnapshot(&out) != nil {
		t.Fatal("history read failed")
	}
	var rows []UsageRecord
	if json.Unmarshal(out.Bytes(), &rows) != nil || len(rows) != 8 {
		t.Fatal("history lost records")
	}
}
func TestMissingUsageAndStorageFailure(t *testing.T) {
	t.Setenv("CONVERGE_USAGE_DIR", filepath.Join(t.TempDir(), "blocked"))
	if e := os.WriteFile(os.Getenv("CONVERGE_USAGE_DIR"), []byte("x"), 0600); e != nil {
		t.Fatal(e)
	}
	var stderr bytes.Buffer
	opts := Options{Stderr: &stderr}
	finish := Monitor("agy", &opts)
	finish(nil)
	if !strings.Contains(stderr.String(), "usage_record_failed") || opts.Meter.Record.InputTokens != nil || len(opts.Meter.Record.Weekly) != 0 {
		t.Fatal(stderr.String())
	}
}

func TestHistoryDoesNotInventFields(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CONVERGE_USAGE_DIR", dir)
	if e := os.WriteFile(filepath.Join(dir, "old.json"), []byte(`{"schema_version":1,"started_at":"2026-11-14T00:00:00Z","weekly":[]}`), 0600); e != nil {
		t.Fatal(e)
	}
	var out bytes.Buffer
	if e := UsageSnapshot(&out); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(out.String(), "warning_percent") || strings.Contains(out.String(), "observed_at") {
		t.Fatal("invented absent metadata", out.String())
	}
}

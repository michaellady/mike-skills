package provider

// Usage is transport metadata, never model judgment. One atomic private file
// per invocation avoids a shared append log or a new orchestration service.
import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"
)

type Weekly struct {
	Bucket      string  `json:"bucket"`
	UsedPercent float64 `json:"used_percent"`
	ResetsAt    *int64  `json:"resets_at"`
	Source      string  `json:"source"`
	ObservedAt  int64   `json:"observed_at"`
}
type UsageRecord struct {
	SchemaVersion   int        `json:"schema_version"`
	ID              string     `json:"id"`
	Provider        string     `json:"provider"`
	Model           string     `json:"requested_model"`
	Effort          string     `json:"requested_effort"`
	StartedAt       time.Time  `json:"started_at"`
	FinishedAt      *time.Time `json:"finished_at"`
	DurationSeconds float64    `json:"duration_seconds"`
	Status          string     `json:"status"`
	InputTokens     *int64     `json:"input_tokens"`
	OutputTokens    *int64     `json:"output_tokens"`
	Weekly          []Weekly   `json:"weekly"`
	WeeklyStatus    string     `json:"weekly_status"`
	Alerts          []string   `json:"alerts"`
	WarningPercent  float64    `json:"warning_percent"`
	CriticalPercent float64    `json:"critical_percent"`
}
type Meter struct {
	Record UsageRecord
	opts   *Options
	path   string
}

var usageOutputMu sync.Mutex

func UsageDir() (string, error) {
	if p := os.Getenv("CONVERGE_USAGE_DIR"); p != "" {
		return p, nil
	}
	p, e := os.UserCacheDir()
	return filepath.Join(p, "converge", "usage"), e
}
func emitUsage(w io.Writer, r UsageRecord) {
	if w == nil {
		w = os.Stderr
	}
	b, _ := json.Marshal(r)
	usageOutputMu.Lock()
	defer usageOutputMu.Unlock()
	fmt.Fprintf(w, "[usage] %s\n", b)
}
func (m *Meter) save() error {
	if m.path == "" {
		return errors.New("usage storage unavailable")
	}
	f, e := os.CreateTemp(filepath.Dir(m.path), ".usage-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	e = json.NewEncoder(f).Encode(m.Record)
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e == nil {
		e = os.Rename(f.Name(), m.path)
	}
	return e
}

// Monitor records starts, failures and completion even with --quiet. It never
// logs prompts, replies, credentials, account identities or raw diagnostics.
// Recording failure warns without changing the model's own result.
func Monitor(name string, opts *Options) func(error) {
	b := make([]byte, 16)
	_, e := rand.Read(b)
	m := &Meter{opts: opts, Record: UsageRecord{SchemaVersion: 1, ID: hex.EncodeToString(b), Provider: name,
		StartedAt: time.Now().UTC(), Status: "running", Weekly: []Weekly{}, WeeklyStatus: "unavailable", Alerts: []string{}}}
	dir, de := UsageDir()
	if e == nil && de == nil {
		if e = os.MkdirAll(dir, 0700); e == nil {
			m.path = filepath.Join(dir, m.Record.ID+".json")
		}
	}
	opts.Meter = m
	m.Record.WarningPercent = threshold("CONVERGE_USAGE_WARN_PERCENT", 80)
	m.Record.CriticalPercent = threshold("CONVERGE_USAGE_CRITICAL_PERCENT", 95)
	if m.Record.CriticalPercent < m.Record.WarningPercent {
		m.Record.Alerts = append(m.Record.Alerts, "invalid_alert_threshold_order")
		m.Record.WarningPercent, m.Record.CriticalPercent = 80, 95
	}
	m.Record.Model, m.Record.Effort = opts.Model, opts.Effort
	if e = m.save(); e != nil {
		m.Record.Alerts = append(m.Record.Alerts, "usage_record_failed")
	}
	emitUsage(opts.Stderr, m.Record)
	return func(runErr error) {
		m.Record.Model, m.Record.Effort = opts.Model, opts.Effort
		if m.Record.Status == "running" {
			m.Record.Status = "completed"
		}
		if runErr != nil {
			m.Record.Status = "failed"
			var pe *Error
			if errors.As(runErr, &pe) && pe.Code == ExitTimeout {
				m.Record.Status = "timeout"
			}
		}
		// These are bounded read-only allowance queries through the native login.
		if name == "codex" {
			m.Record.Weekly, m.Record.WeeklyStatus = codexWeekly()
		}
		if name == "muse" {
			m.Record.Weekly, m.Record.WeeklyStatus = museWeekly()
		}
		now := time.Now().UTC()
		m.Record.FinishedAt = &now
		m.Record.DurationSeconds = now.Sub(m.Record.StartedAt).Seconds()
		warn, critical := m.Record.WarningPercent, m.Record.CriticalPercent
		for _, q := range m.Record.Weekly {
			if q.ResetsAt != nil && *q.ResetsAt <= now.Unix() {
				m.Record.Alerts = append(m.Record.Alerts, "weekly_snapshot_expired:"+q.Bucket)
				continue
			}
			if q.UsedPercent >= critical {
				m.Record.Alerts = append(m.Record.Alerts, "weekly_critical:"+q.Bucket)
			} else if q.UsedPercent >= warn {
				m.Record.Alerts = append(m.Record.Alerts, "weekly_warning:"+q.Bucket)
			}
		}
		if m.Record.Status != "completed" {
			m.Record.Alerts = append(m.Record.Alerts, "provider_"+m.Record.Status)
		}
		if e := m.save(); e != nil {
			m.Record.Alerts = append(m.Record.Alerts, "usage_record_failed")
		}
		emitUsage(opts.Stderr, m.Record)
	}
}
func threshold(key string, def float64) float64 {
	s := os.Getenv(key)
	if s == "" {
		return def
	}
	v, e := strconv.ParseFloat(s, 64)
	if e != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 100 {
		return def
	}
	return v
}
func validPercent(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 && v <= 100 }

// Observe consumes only verified native terminal/limit event shapes, not
// assistant text. Missing fields remain null; a reported zero stays zero.
func (m *Meter) Observe(raw []byte) {
	if m == nil {
		return
	}
	var ev struct {
		Type  string `json:"type"`
		Usage struct {
			Input  *int64 `json:"input_tokens"`
			Output *int64 `json:"output_tokens"`
			Create *int64 `json:"cache_creation_input_tokens"`
			Read   *int64 `json:"cache_read_input_tokens"`
		} `json:"usage"`
		Info struct {
			Windows map[string]struct {
				Utilization *float64 `json:"utilization"`
				Reset       *int64   `json:"resetsAt"`
			} `json:"unifiedWindows"`
		} `json:"rate_limit_info"`
	}
	if json.Unmarshal(raw, &ev) != nil {
		return
	}
	if (m.Record.Provider == "claude" && ev.Type == "result") || (m.Record.Provider == "codex" && ev.Type == "turn.completed") {
		if ev.Usage.Input != nil && *ev.Usage.Input >= 0 {
			n := *ev.Usage.Input
			if m.Record.Provider == "claude" {
				for _, p := range []*int64{ev.Usage.Create, ev.Usage.Read} {
					if p != nil && *p >= 0 {
						n += *p
					}
				}
			}
			m.Record.InputTokens = &n
		}
		if ev.Usage.Output != nil && *ev.Usage.Output >= 0 {
			m.Record.OutputTokens = ev.Usage.Output
		}
	}
	if m.Record.Provider == "claude" && ev.Type == "rate_limit_event" {
		var weekly []Weekly
		for bucket, w := range ev.Info.Windows {
			if bucket != "seven_day" && bucket != "seven_day_overage_included" {
				continue
			}
			if w.Utilization != nil && validPercent(*w.Utilization*100) {
				weekly = append(weekly, Weekly{bucket, *w.Utilization * 100, w.Reset, "claude.rate_limit_event", time.Now().Unix()})
			}
		}
		if len(weekly) > 0 {
			sort.Slice(weekly, func(i, j int) bool { return weekly[i].Bucket < weekly[j].Bucket })
			m.Record.Weekly = weekly
			m.Record.WeeklyStatus = "reported"
		}
	}
}

// codexWeekly uses the CLI's generated app-server protocol. Only initialize and
// account/rateLimits/read are sent: no model turn, reset, credit or login action.
func codexWeekly() ([]Weekly, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "codex", "app-server", "--stdio")
	cmd.Stderr = io.Discard
	out, e := cmd.StdoutPipe()
	if e != nil {
		return nil, "query_failed"
	}
	in, e := cmd.StdinPipe()
	if e != nil {
		return nil, "query_failed"
	}
	if e = cmd.Start(); e != nil {
		return nil, "query_failed"
	}
	defer func() { in.Close(); cmd.Process.Kill(); cmd.Wait() }()
	enc := json.NewEncoder(in)
	if enc.Encode(map[string]any{"id": 1, "method": "initialize", "params": map[string]any{"clientInfo": map[string]any{"name": "converge-usage", "version": "1", "title": "Usage status"}, "capabilities": nil}}) != nil {
		return nil, "query_failed"
	}
	scanner := bufio.NewScanner(out)
	scanner.Buffer(make([]byte, 65536), 1048576)
	for scanner.Scan() {
		var ev struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if json.Unmarshal(scanner.Bytes(), &ev) != nil {
			continue
		}
		if len(ev.Error) > 0 && string(ev.Error) != "null" {
			return nil, "query_failed"
		}
		if ev.ID == 1 {
			if enc.Encode(map[string]any{"method": "initialized"}) != nil {
				return nil, "query_failed"
			}
			if enc.Encode(map[string]any{"id": 2, "method": "account/rateLimits/read"}) != nil {
				return nil, "query_failed"
			}
		}
		if ev.ID == 2 {
			return parseCodexWeekly(ev.Result)
		}
	}
	return nil, "query_failed"
}
func parseCodexWeekly(b []byte) ([]Weekly, string) {
	type window struct {
		Used    *float64 `json:"usedPercent"`
		Minutes *int     `json:"windowDurationMins"`
		Reset   *int64   `json:"resetsAt"`
	}
	type limit struct {
		ID                 string `json:"limitId"`
		Primary, Secondary *window
	}
	var result struct {
		Limits map[string]limit `json:"rateLimitsByLimitId"`
		Legacy limit            `json:"rateLimits"`
	}
	if json.Unmarshal(b, &result) != nil {
		return nil, "query_failed"
	}
	if len(result.Limits) == 0 {
		result.Limits = map[string]limit{"codex": result.Legacy}
	}
	weekly := []Weekly{}
	for name, lim := range result.Limits {
		for _, w := range []*window{lim.Primary, lim.Secondary} {
			if w != nil && w.Minutes != nil && *w.Minutes == 10080 && w.Used != nil && validPercent(*w.Used) {
				weekly = append(weekly, Weekly{name, *w.Used, w.Reset, "codex.account/rateLimits/read", time.Now().Unix()})
			}
		}
	}
	sort.Slice(weekly, func(i, j int) bool { return weekly[i].Bucket < weekly[j].Bucket })
	if len(weekly) == 0 {
		return weekly, "unavailable"
	}
	return weekly, "reported"
}
func museWeekly() ([]Weekly, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "muse", "usage", "--json")
	// Same subscription identity as the Muse adapter, without inspecting secrets.
	for _, v := range os.Environ() {
		if len(v) < 13 || v[:13] != "META_API_KEY=" {
			cmd.Env = append(cmd.Env, v)
		}
	}
	b, e := cmd.Output()
	if e != nil {
		return nil, "query_failed"
	}
	return parseMuseWeekly(b)
}
func parseMuseWeekly(b []byte) ([]Weekly, string) {
	var v struct {
		SchemaVersion int `json:"schemaVersion"`
		Allowance     *struct {
			Weekly *struct {
				Used  *float64 `json:"usedPercent"`
				Reset *int64   `json:"resetsAtMs"`
			} `json:"weekly"`
			AsOf *int64 `json:"asOfMs"`
		} `json:"allowance"`
		Reason string `json:"reason"`
	}
	if json.Unmarshal(b, &v) != nil {
		return nil, "query_failed"
	}
	if v.Reason == "server_sent_no_usage" {
		return nil, "server_sent_no_usage"
	}
	if v.SchemaVersion == 1 && v.Allowance != nil && v.Allowance.Weekly != nil && v.Allowance.Weekly.Used != nil && validPercent(*v.Allowance.Weekly.Used) && v.Allowance.AsOf != nil {
		var reset *int64
		if v.Allowance.Weekly.Reset != nil {
			n := *v.Allowance.Weekly.Reset / 1000
			reset = &n
		}
		return []Weekly{{"weekly", *v.Allowance.Weekly.Used, reset, "muse.usage", *v.Allowance.AsOf / 1000}}, "reported"
	}
	return nil, "unsupported_allowance_shape"
}

// UsageSnapshot lists local receipts only. Percentages are snapshots of entire
// provider accounts, never added together or attributed to this harness.
func UsageSnapshot(w io.Writer) error {
	dir, e := UsageDir()
	if e != nil {
		return e
	}
	entries, e := os.ReadDir(dir)
	if os.IsNotExist(e) {
		return json.NewEncoder(w).Encode([]UsageRecord{})
	}
	if e != nil {
		return e
	}
	records := []UsageRecord{}
	for _, ent := range entries {
		if ent.IsDir() || filepath.Ext(ent.Name()) != ".json" {
			continue
		}
		b, e := os.ReadFile(filepath.Join(dir, ent.Name()))
		if e != nil {
			return e
		}
		var r UsageRecord
		if e = json.Unmarshal(b, &r); e != nil {
			return fmt.Errorf("invalid usage receipt %s", ent.Name())
		}
		records = append(records, r)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].StartedAt.Before(records[j].StartedAt) })
	if len(records) > 20 {
		records = records[len(records)-20:]
	}
	return json.NewEncoder(w).Encode(records)
}

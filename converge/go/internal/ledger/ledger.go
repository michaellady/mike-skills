// Package ledger persists every `converge audit` run to a local SQLite database
// so model-comparison analysis can accumulate over time: which reviewers respond
// vs. skip vs. emit malformed output, which severities each reviewer raises, and
// (once findings are dispositioned) each reviewer's precision (fixed vs.
// false_positive).
//
// The store is intentionally pure-Go: the converge binary builds with
// CGO_ENABLED=0, so the driver is modernc.org/sqlite (driver name "sqlite")
// over database/sql — NOT the cgo mattn/go-sqlite3.
//
// Schema (created on open if absent):
//
//	audits(audit_id PK, ts, label, summary, prompt_sha256, reviewers_requested, duration_ms)
//	reviews(audit_id, model, status, verdict, latency_ms)         status ∈ responded|skipped|parse_error
//	findings(finding_id, audit_id, severity, title, loc, raised_by)
//	dispositions(finding_id, ts, kind, note, commit_sha)          kind ∈ fixed|false_positive|wontfix
//
// All audit writes are best-effort from the caller's perspective: a Record error
// is surfaced to the caller but never alters the audit's own exit code/output.
package ledger

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// ReviewRow is one reviewer's participation in a single audit.
type ReviewRow struct {
	Model     string // canonical reviewer name (claude, codex, grok-build, ...)
	Status    string // responded | skipped | parse_error
	Verdict   string // reviewer's own summary (all_pass/some_fail) when responded, else ""
	LatencyMs int64
}

// FindingRow is one issue raised in a single audit.
type FindingRow struct {
	FindingID string // first 16 hex chars of sha256(loc + "|" + title)
	Severity  string // CRITICAL | HIGH | MEDIUM | LOW | ""
	Title     string
	Loc       string // file:line, or ""
	RaisedBy  string // "claude+codex+grok-build" style attribution, or ""
}

// AuditRecord is one fully-described audit run ready to persist.
type AuditRecord struct {
	AuditID            string
	TS                 string
	Label              string
	Summary            string
	PromptSHA256       string
	ReviewersRequested string
	DurationMs         int64
	Reviews            []ReviewRow
	Findings           []FindingRow
}

// validKinds is the closed set of disposition kinds.
var validKinds = map[string]bool{"fixed": true, "false_positive": true, "wontfix": true}

// NewAuditID returns 16 random bytes as hex — the audit_id primary key.
func NewAuditID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failure is effectively impossible; fall back to a
		// time-derived id rather than panic inside a best-effort writer.
		return fmt.Sprintf("%016x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// FindingID is the stable id for a finding: first 16 hex chars of
// sha256(loc + "|" + title). Stable across audits so the same problem at the
// same location clusters to one disposition.
func FindingID(loc, title string) string {
	sum := sha256.Sum256([]byte(loc + "|" + title))
	return hex.EncodeToString(sum[:])[:16]
}

// dbPath resolves the ledger DB path: $CONVERGE_LEDGER if set, else
// $HOME/.converge/ledger.db.
func dbPath() (string, error) {
	if p := os.Getenv("CONVERGE_LEDGER"); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".converge", "ledger.db"), nil
}

// open resolves the path, creates the parent dir (0700), opens the DB with the
// pure-Go driver, applies pragmas, and ensures the schema exists.
func open() (*sql.DB, error) {
	path, err := dbPath()
	if err != nil {
		return nil, err
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("create ledger dir %q: %w", dir, err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open ledger %q: %w", path, err)
	}
	for _, pragma := range []string{
		"PRAGMA busy_timeout=5000;",
		"PRAGMA journal_mode=WAL;",
	} {
		if _, err := db.Exec(pragma); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("pragma %q: %w", pragma, err)
		}
	}
	if err := ensureSchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func ensureSchema(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS audits(
			audit_id TEXT PRIMARY KEY,
			ts TEXT,
			label TEXT,
			summary TEXT,
			prompt_sha256 TEXT,
			reviewers_requested TEXT,
			duration_ms INTEGER
		);`,
		`CREATE TABLE IF NOT EXISTS reviews(
			audit_id TEXT,
			model TEXT,
			status TEXT,
			verdict TEXT,
			latency_ms INTEGER
		);`,
		`CREATE TABLE IF NOT EXISTS findings(
			finding_id TEXT,
			audit_id TEXT,
			severity TEXT,
			title TEXT,
			loc TEXT,
			raised_by TEXT
		);`,
		`CREATE TABLE IF NOT EXISTS dispositions(
			finding_id TEXT,
			ts TEXT,
			kind TEXT,
			note TEXT,
			commit_sha TEXT
		);`,
		`CREATE INDEX IF NOT EXISTS idx_reviews_model ON reviews(model);`,
		`CREATE INDEX IF NOT EXISTS idx_findings_finding_id ON findings(finding_id);`,
		`CREATE INDEX IF NOT EXISTS idx_dispositions_finding_id ON dispositions(finding_id);`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return fmt.Errorf("ensure schema: %w", err)
		}
	}
	return nil
}

// Record inserts an audit and its reviews + findings in ONE transaction.
func Record(rec AuditRecord) error {
	db, err := open()
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	rollback := func() { _ = tx.Rollback() }

	if _, err := tx.Exec(
		`INSERT INTO audits(audit_id, ts, label, summary, prompt_sha256, reviewers_requested, duration_ms)
		 VALUES(?, ?, ?, ?, ?, ?, ?)`,
		rec.AuditID, rec.TS, rec.Label, rec.Summary, rec.PromptSHA256, rec.ReviewersRequested, rec.DurationMs,
	); err != nil {
		rollback()
		return fmt.Errorf("insert audit: %w", err)
	}

	for _, r := range rec.Reviews {
		if _, err := tx.Exec(
			`INSERT INTO reviews(audit_id, model, status, verdict, latency_ms) VALUES(?, ?, ?, ?, ?)`,
			rec.AuditID, r.Model, r.Status, r.Verdict, r.LatencyMs,
		); err != nil {
			rollback()
			return fmt.Errorf("insert review %q: %w", r.Model, err)
		}
	}

	for _, f := range rec.Findings {
		if _, err := tx.Exec(
			`INSERT INTO findings(finding_id, audit_id, severity, title, loc, raised_by) VALUES(?, ?, ?, ?, ?, ?)`,
			f.FindingID, rec.AuditID, f.Severity, f.Title, f.Loc, f.RaisedBy,
		); err != nil {
			rollback()
			return fmt.Errorf("insert finding %q: %w", f.FindingID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// Disposition records the outcome of a finding (kind ∈ fixed|false_positive|wontfix).
func Disposition(findingID, kind, note, commitSHA string) error {
	findingID = strings.TrimSpace(findingID)
	if findingID == "" {
		return fmt.Errorf("disposition: finding_id is required")
	}
	if !validKinds[kind] {
		return fmt.Errorf("disposition: kind must be fixed|false_positive|wontfix, got %q", kind)
	}
	db, err := open()
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	if _, err := db.Exec(
		`INSERT INTO dispositions(finding_id, ts, kind, note, commit_sha) VALUES(?, ?, ?, ?, ?)`,
		findingID, time.Now().UTC().Format(time.RFC3339), kind, note, commitSHA,
	); err != nil {
		return fmt.Errorf("insert disposition: %w", err)
	}
	return nil
}

// severities is the fixed display order for per-severity counts.
var severities = []string{"CRITICAL", "HIGH", "MEDIUM", "LOW"}

// modelStat accumulates one reviewer's lifetime numbers.
type modelStat struct {
	audits     int // audits the model participated in (any status)
	responded  int
	skipped    int
	parseError int
	bySeverity map[string]int
	fixed      int
	falsePos   int
}

func newModelStat() *modelStat {
	return &modelStat{bySeverity: map[string]int{}}
}

// Stats prints a per-model table: participation, responded/skipped/parse_error
// (+ response rate), findings raised by severity, and precision = fixed /
// (fixed+false_positive). A totals row aggregates across models.
func Stats(w io.Writer) error {
	db, err := open()
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	stats := map[string]*modelStat{}
	order := []string{} // first-seen model order, stable output

	get := func(model string) *modelStat {
		s, ok := stats[model]
		if !ok {
			s = newModelStat()
			stats[model] = s
			order = append(order, model)
		}
		return s
	}

	// Reviews: participation + status counts.
	rows, err := db.Query(`SELECT model, status FROM reviews`)
	if err != nil {
		return fmt.Errorf("query reviews: %w", err)
	}
	for rows.Next() {
		var model, status string
		if err := rows.Scan(&model, &status); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan review: %w", err)
		}
		s := get(model)
		s.audits++
		switch status {
		case "responded":
			s.responded++
		case "skipped":
			s.skipped++
		case "parse_error":
			s.parseError++
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate reviews: %w", err)
	}
	_ = rows.Close()

	// Findings raised by severity, attributed to each model named in raised_by.
	// raised_by clusters reviewers with '+' (e.g. "claude+codex"); a finding
	// counts for every model whose name appears.
	frows, err := db.Query(`SELECT severity, raised_by FROM findings`)
	if err != nil {
		return fmt.Errorf("query findings: %w", err)
	}
	for frows.Next() {
		var severity, raisedBy string
		if err := frows.Scan(&severity, &raisedBy); err != nil {
			_ = frows.Close()
			return fmt.Errorf("scan finding: %w", err)
		}
		for _, m := range splitRaisedBy(raisedBy) {
			s := get(m)
			s.bySeverity[strings.ToUpper(severity)]++
		}
	}
	if err := frows.Err(); err != nil {
		_ = frows.Close()
		return fmt.Errorf("iterate findings: %w", err)
	}
	_ = frows.Close()

	// Precision: for each model, join its findings (raised_by LIKE %model%) to
	// dispositions and count fixed vs. false_positive.
	for _, model := range order {
		fixed, falsePos, err := precisionCounts(db, model)
		if err != nil {
			return err
		}
		s := get(model)
		s.fixed = fixed
		s.falsePos = falsePos
	}
	// A model may appear only in findings (never in reviews) — make sure those
	// also got precision computed. order already includes them via get().

	sort.Strings(order)
	return renderStats(w, order, stats)
}

// precisionCounts returns (fixed, false_positive) over distinct findings raised
// by model that have a disposition. A finding counts once per kind regardless of
// how many disposition rows it has of that kind.
func precisionCounts(db *sql.DB, model string) (int, int, error) {
	q := `
		SELECT
			SUM(CASE WHEN d.kind='fixed' THEN 1 ELSE 0 END),
			SUM(CASE WHEN d.kind='false_positive' THEN 1 ELSE 0 END)
		FROM (
			SELECT DISTINCT f.finding_id
			FROM findings f
			WHERE f.raised_by LIKE '%' || ? || '%'
		) fr
		JOIN (
			SELECT DISTINCT finding_id, kind FROM dispositions
		) d ON d.finding_id = fr.finding_id`
	var fixed, falsePos sql.NullInt64
	if err := db.QueryRow(q, model).Scan(&fixed, &falsePos); err != nil {
		return 0, 0, fmt.Errorf("precision for %q: %w", model, err)
	}
	return int(fixed.Int64), int(falsePos.Int64), nil
}

// splitRaisedBy splits a "claude+codex+grok-build" attribution into model names,
// dropping empties.
func splitRaisedBy(raisedBy string) []string {
	out := []string{}
	for _, p := range strings.Split(raisedBy, "+") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func renderStats(w io.Writer, order []string, stats map[string]*modelStat) error {
	fmt.Fprintf(w, "%-16s %7s %9s %7s %11s %8s %4s %4s %6s %3s %9s\n",
		"MODEL", "AUDITS", "RESPONDED", "SKIP", "PARSE_ERR", "RESP_RATE", "CRIT", "HIGH", "MEDIUM", "LOW", "PRECISION")

	totals := newModelStat()
	for _, model := range order {
		s := stats[model]
		writeStatRow(w, model, s)
		totals.audits += s.audits
		totals.responded += s.responded
		totals.skipped += s.skipped
		totals.parseError += s.parseError
		for _, sev := range severities {
			totals.bySeverity[sev] += s.bySeverity[sev]
		}
		totals.fixed += s.fixed
		totals.falsePos += s.falsePos
	}
	if len(order) > 0 {
		fmt.Fprintln(w, strings.Repeat("-", 100))
	}
	writeStatRow(w, "TOTAL", totals)
	return nil
}

func writeStatRow(w io.Writer, model string, s *modelStat) {
	fmt.Fprintf(w, "%-16s %7d %9d %7d %11d %8s %4d %4d %6d %3d %9s\n",
		model,
		s.audits,
		s.responded,
		s.skipped,
		s.parseError,
		rate(s.responded, s.audits),
		s.bySeverity["CRITICAL"],
		s.bySeverity["HIGH"],
		s.bySeverity["MEDIUM"],
		s.bySeverity["LOW"],
		precision(s.fixed, s.falsePos),
	)
}

// rate renders n/d as a percent, or "-" when d==0.
func rate(n, d int) string {
	if d == 0 {
		return "-"
	}
	return fmt.Sprintf("%.0f%%", 100*float64(n)/float64(d))
}

// precision renders fixed/(fixed+false_positive) as a percent, or "-" when there
// are no dispositioned findings to score.
func precision(fixed, falsePos int) string {
	denom := fixed + falsePos
	if denom == 0 {
		return "-"
	}
	return fmt.Sprintf("%.0f%%", 100*float64(fixed)/float64(denom))
}

// Findings lists the most recent `limit` findings with their current
// disposition (most recent disposition per finding, if any).
func Findings(w io.Writer, limit int) error {
	if limit <= 0 {
		limit = 20
	}
	db, err := open()
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	q := `
		SELECT a.ts, f.severity, f.title, f.loc, f.raised_by, f.finding_id,
		       (SELECT d.kind FROM dispositions d
		        WHERE d.finding_id = f.finding_id
		        ORDER BY d.ts DESC LIMIT 1) AS disposition
		FROM findings f
		JOIN audits a ON a.audit_id = f.audit_id
		ORDER BY a.ts DESC
		LIMIT ?`
	rows, err := db.Query(q, limit)
	if err != nil {
		return fmt.Errorf("query findings: %w", err)
	}
	defer func() { _ = rows.Close() }()

	fmt.Fprintf(w, "%-20s %-8s %-40s %-22s %-22s %-16s %s\n",
		"TS", "SEVERITY", "TITLE", "LOC", "RAISED_BY", "FINDING_ID", "DISPOSITION")
	any := false
	for rows.Next() {
		any = true
		var ts, severity, title, loc, raisedBy, findingID string
		var disposition sql.NullString
		if err := rows.Scan(&ts, &severity, &title, &loc, &raisedBy, &findingID, &disposition); err != nil {
			return fmt.Errorf("scan finding: %w", err)
		}
		disp := "-"
		if disposition.Valid && disposition.String != "" {
			disp = disposition.String
		}
		fmt.Fprintf(w, "%-20s %-8s %-40s %-22s %-22s %-16s %s\n",
			ts, severity, truncate(title, 40), truncate(loc, 22), truncate(raisedBy, 22), findingID, disp)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate findings: %w", err)
	}
	if !any {
		fmt.Fprintln(w, "(no findings recorded yet)")
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

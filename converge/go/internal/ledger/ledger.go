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
	Level              string // review depth preset (light|medium|deep); "" on pre-level rows
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

// ArtifactDir returns the per-run directory for raw reviewer outputs,
// alongside the ledger DB: <ledger-dir>/audits/<audit_id>. Heartbeat lines
// truncate at ~80 chars, so without these files a late or malformed verdict
// is irrecoverable. The caller creates the dir on first write; nothing about
// it is recorded in the DB itself.
func ArtifactDir(auditID string) (string, error) {
	p, err := dbPath()
	if err != nil {
		return "", err
	}
	// Absolute so the path stays meaningful when echoed in skip reasons and
	// stderr notes (a relative $CONVERGE_LEDGER would otherwise print a path
	// only valid from the audit's cwd).
	dir, err := filepath.Abs(filepath.Join(filepath.Dir(p), "audits", auditID))
	if err != nil {
		return "", err
	}
	return dir, nil
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
	// Additive migrations: CREATE TABLE IF NOT EXISTS never adds columns to
	// an existing DB, so new columns are ensured individually.
	// audits.level (2026-08): the review-depth preset, for per-level model
	// evaluation via `ledger stats --by-level`.
	if err := ensureColumn(db, "audits", "level", "TEXT"); err != nil {
		return err
	}
	return nil
}

// ensureColumn adds table.col if it doesn't exist yet (SQLite has no ADD
// COLUMN IF NOT EXISTS).
func ensureColumn(db *sql.DB, table, col, typ string) error {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return fmt.Errorf("table_info %s: %w", table, err)
	}
	found := false
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan table_info %s: %w", table, err)
		}
		if name == col {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate table_info %s: %w", table, err)
	}
	_ = rows.Close()
	if found {
		return nil
	}
	if _, err := db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, col, typ)); err != nil {
		return fmt.Errorf("add column %s.%s: %w", table, col, err)
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
		`INSERT INTO audits(audit_id, ts, label, summary, level, prompt_sha256, reviewers_requested, duration_ms)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.AuditID, rec.TS, rec.Label, rec.Summary, rec.Level, rec.PromptSHA256, rec.ReviewersRequested, rec.DurationMs,
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

// statKey identifies one stats row: a model, optionally broken out by the
// review level of the audit each review/finding belonged to.
type statKey struct {
	model string
	level string // "" when not breaking out, or for pre-level audit rows
}

// Stats prints a per-model table: participation, responded/skipped/parse_error
// (+ response rate), findings raised by severity, and precision = fixed /
// (fixed+false_positive). A totals row aggregates across models. With byLevel,
// rows break out per (model, level) — level comes from the audit each
// review/finding belongs to ("-" for rows recorded before levels existed) —
// so model families can be evaluated per review depth.
func Stats(w io.Writer, byLevel bool) error {
	db, err := open()
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	stats := map[statKey]*modelStat{}
	order := []statKey{} // first-seen order; sorted before render

	get := func(k statKey) *modelStat {
		s, ok := stats[k]
		if !ok {
			s = newModelStat()
			stats[k] = s
			order = append(order, k)
		}
		return s
	}

	// Reviews: participation + status counts (joined to audits for the level
	// when breaking out).
	reviewsQ := `SELECT model, '', status FROM reviews`
	if byLevel {
		reviewsQ = `SELECT r.model, COALESCE(a.level, ''), r.status
			FROM reviews r LEFT JOIN audits a ON a.audit_id = r.audit_id`
	}
	rows, err := db.Query(reviewsQ)
	if err != nil {
		return fmt.Errorf("query reviews: %w", err)
	}
	for rows.Next() {
		var model, level, status string
		if err := rows.Scan(&model, &level, &status); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan review: %w", err)
		}
		s := get(statKey{model, level})
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
	findingsQ := `SELECT severity, raised_by, '' FROM findings`
	if byLevel {
		findingsQ = `SELECT f.severity, f.raised_by, COALESCE(a.level, '')
			FROM findings f LEFT JOIN audits a ON a.audit_id = f.audit_id`
	}
	frows, err := db.Query(findingsQ)
	if err != nil {
		return fmt.Errorf("query findings: %w", err)
	}
	for frows.Next() {
		var severity, raisedBy, level string
		if err := frows.Scan(&severity, &raisedBy, &level); err != nil {
			_ = frows.Close()
			return fmt.Errorf("scan finding: %w", err)
		}
		for _, m := range splitRaisedBy(raisedBy) {
			s := get(statKey{m, level})
			s.bySeverity[strings.ToUpper(severity)]++
		}
	}
	if err := frows.Err(); err != nil {
		_ = frows.Close()
		return fmt.Errorf("iterate findings: %w", err)
	}
	_ = frows.Close()

	// Precision: for each row, join its findings (raised_by LIKE %model%,
	// level-filtered when breaking out) to dispositions.
	for _, k := range order {
		fixed, falsePos, err := precisionCounts(db, k.model, k.level, byLevel)
		if err != nil {
			return err
		}
		s := get(k)
		s.fixed = fixed
		s.falsePos = falsePos
	}
	// A model may appear only in findings (never in reviews) — make sure those
	// also got precision computed. order already includes them via get().

	sort.Slice(order, func(i, j int) bool {
		if order[i].model != order[j].model {
			return order[i].model < order[j].model
		}
		return levelRank(order[i].level) < levelRank(order[j].level)
	})
	return renderStats(w, order, stats, byLevel)
}

// levelRank orders light < medium < deep < anything else (incl. pre-level "").
func levelRank(l string) int {
	switch l {
	case "light":
		return 0
	case "medium":
		return 1
	case "deep":
		return 2
	}
	return 3
}

// precisionCounts returns (fixed, false_positive) over distinct findings raised
// by model that have a disposition — filtered to one audit level when byLevel.
// A finding counts once per kind regardless of how many disposition rows it has
// of that kind.
func precisionCounts(db *sql.DB, model, level string, byLevel bool) (int, int, error) {
	join, extra := "", ""
	args := []any{model}
	if byLevel {
		join = "LEFT JOIN audits a ON a.audit_id = f.audit_id"
		extra = "AND COALESCE(a.level, '') = ?"
		args = append(args, level)
	}
	q := fmt.Sprintf(`
		SELECT
			SUM(CASE WHEN d.kind='fixed' THEN 1 ELSE 0 END),
			SUM(CASE WHEN d.kind='false_positive' THEN 1 ELSE 0 END)
		FROM (
			SELECT DISTINCT f.finding_id
			FROM findings f
			%s
			WHERE f.raised_by LIKE '%%' || ? || '%%' %s
		) fr
		JOIN (
			SELECT DISTINCT finding_id, kind FROM dispositions
		) d ON d.finding_id = fr.finding_id`, join, extra)
	var fixed, falsePos sql.NullInt64
	if err := db.QueryRow(q, args...).Scan(&fixed, &falsePos); err != nil {
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

func renderStats(w io.Writer, order []statKey, stats map[statKey]*modelStat, byLevel bool) error {
	if byLevel {
		fmt.Fprintf(w, "%-16s %-8s %7s %9s %7s %11s %8s %4s %4s %6s %3s %9s\n",
			"MODEL", "LEVEL", "AUDITS", "RESPONDED", "SKIP", "PARSE_ERR", "RESP_RATE", "CRIT", "HIGH", "MEDIUM", "LOW", "PRECISION")
	} else {
		fmt.Fprintf(w, "%-16s %7s %9s %7s %11s %8s %4s %4s %6s %3s %9s\n",
			"MODEL", "AUDITS", "RESPONDED", "SKIP", "PARSE_ERR", "RESP_RATE", "CRIT", "HIGH", "MEDIUM", "LOW", "PRECISION")
	}

	totals := newModelStat()
	for _, k := range order {
		s := stats[k]
		writeStatRow(w, k.model, k.level, byLevel, s)
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
		fmt.Fprintln(w, strings.Repeat("-", 109))
	}
	writeStatRow(w, "TOTAL", "", byLevel, totals)
	return nil
}

func writeStatRow(w io.Writer, model, level string, byLevel bool, s *modelStat) {
	if byLevel {
		if level == "" {
			level = "-"
		}
		if model == "TOTAL" {
			level = ""
		}
		fmt.Fprintf(w, "%-16s %-8s ", model, level)
	} else {
		fmt.Fprintf(w, "%-16s ", model)
	}
	fmt.Fprintf(w, "%7d %9d %7d %11d %8s %4d %4d %6d %3d %9s\n",
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

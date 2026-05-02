// Package logwriter manages CONVERGE LOG sections and REVIEW.md files.
package logwriter

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const header = `
## CONVERGE LOG

| Round | Author | Verdict | Issues raised | Issues conceded |
|-------|--------|---------|---------------|-----------------|
`

// Init ensures the file has the standard CONVERGE LOG header and appends a
// dated `### Run YYYY-MM-DD HH:MM` subsection so prior runs aren't
// overwritten.
func Init(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), "## CONVERGE LOG") {
		if err := appendString(path, header); err != nil {
			return err
		}
	}
	return appendString(path, fmt.Sprintf("\n### Run %s\n\n", time.Now().Format("2006-01-02 15:04")))
}

// Row appends one row to the table. Empty fields render as "(none)".
func Row(path string, round int, author, verdict, issues, conceded string) error {
	def := func(s string) string {
		if s == "" {
			return "(none)"
		}
		return s
	}
	return appendString(path, fmt.Sprintf("| %d | %s | %s | %s | %s |\n",
		round, author, verdict, def(issues), def(conceded)))
}

// Smoke appends a `Smoke check: <result>` line.
func Smoke(path, result string) error {
	return appendString(path, fmt.Sprintf("Smoke check: %s\n", result))
}

// Note appends free-form text followed by a newline.
func Note(path, text string) error {
	return appendString(path, text+"\n")
}

func appendString(path, s string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(s)
	return err
}

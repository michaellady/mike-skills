package plan

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Resolve picks the plan file for `/converge plan`. Precedence:
//
//  1. explicit path argument (must exist)
//  2. $CONVERGE_ACTIVE_PLAN env var
//  3. most-recently-modified *.md in $CLAUDE_PLANS_DIR / ~/.claude/plans/
//     whose path contains the current repo's basename
//  4. most-recently-modified *.md in that dir, regardless of name
func Resolve(explicit string) (string, error) {
	if explicit != "" {
		if _, err := os.Stat(explicit); err == nil {
			return filepath.Abs(explicit)
		}
		return "", fmt.Errorf("explicit path not found: %s", explicit)
	}
	if p := os.Getenv("CONVERGE_ACTIVE_PLAN"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return filepath.Abs(p)
		}
	}

	plansDir := os.Getenv("CLAUDE_PLANS_DIR")
	if plansDir == "" {
		h, _ := os.UserHomeDir()
		plansDir = filepath.Join(h, ".claude", "plans")
	}
	if _, err := os.Stat(plansDir); err != nil {
		return "", fmt.Errorf("no plans dir at %s", plansDir)
	}

	var slug string
	if out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output(); err == nil {
		slug = strings.ToLower(filepath.Base(strings.TrimSpace(string(out))))
	}

	type cand struct {
		path string
		mod  int64
	}
	var matched, all []cand
	_ = filepath.Walk(plansDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}
		c := cand{path: path, mod: info.ModTime().UnixNano()}
		all = append(all, c)
		if slug != "" && strings.Contains(strings.ToLower(path), slug) {
			matched = append(matched, c)
		}
		return nil
	})

	pick := func(xs []cand) string {
		sort.Slice(xs, func(i, j int) bool { return xs[i].mod > xs[j].mod })
		if len(xs) == 0 {
			return ""
		}
		return xs[0].path
	}
	if p := pick(matched); p != "" {
		return p, nil
	}
	if p := pick(all); p != "" {
		return p, nil
	}
	return "", fmt.Errorf("no .md files in %s", plansDir)
}

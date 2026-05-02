// Package cleanup removes per-round payloads written under /tmp.
package cleanup

import (
	"os"
	"path/filepath"
)

// Run deletes /tmp/converge-claude-r*.json, /tmp/converge-codex-r*.json,
// /tmp/converge-prompt-*.txt, and /tmp/converge-thread-*.txt. The CONVERGE
// LOG / REVIEW.md files are deliverables and are left alone.
func Run() (int, error) {
	patterns := []string{
		"/tmp/converge-claude-r*.json",
		"/tmp/converge-codex-r*.json",
		"/tmp/converge-prompt-*.txt",
		"/tmp/converge-thread-*.txt",
	}
	count := 0
	for _, p := range patterns {
		matches, err := filepath.Glob(p)
		if err != nil {
			return count, err
		}
		for _, m := range matches {
			if err := os.Remove(m); err == nil {
				count++
			}
		}
	}
	return count, nil
}

package gitops

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// DetectBaseBranch resolves the base branch for `/converge review`.
// Order: gh pr view (if pr != "") → gh repo view default → origin/HEAD →
// origin/main → origin/master.
func DetectBaseBranch(pr string) (string, error) {
	if pr != "" {
		if out, err := runOK("gh", "pr", "view", pr, "--json", "baseRefName", "-q", ".baseRefName"); err == nil && out != "" {
			return out, nil
		}
	}
	if out, err := runOK("gh", "repo", "view", "--json", "defaultBranchRef", "-q", ".defaultBranchRef.name"); err == nil && out != "" {
		return out, nil
	}
	if out, err := runOK("git", "symbolic-ref", "refs/remotes/origin/HEAD"); err == nil && out != "" {
		return strings.TrimPrefix(out, "refs/remotes/origin/"), nil
	}
	if err := exec.Command("git", "rev-parse", "--verify", "origin/main").Run(); err == nil {
		return "main", nil
	}
	if err := exec.Command("git", "rev-parse", "--verify", "origin/master").Run(); err == nil {
		return "master", nil
	}
	return "", fmt.Errorf("could not determine base branch")
}

// GetDiff returns base...HEAD (or `gh pr diff <pr>` when pr != ""), truncated
// to maxBytes. If the diff was truncated, a marker line is appended.
// maxBytes <= 0 means no truncation.
func GetDiff(base, pr string, maxBytes int) (string, error) {
	if maxBytes <= 0 {
		if v := os.Getenv("CONVERGE_DIFF_MAX_BYTES"); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				maxBytes = n
			}
		}
		if maxBytes == 0 {
			maxBytes = 51200
		}
	}

	var out []byte
	var err error
	if pr != "" {
		out, err = exec.Command("gh", "pr", "diff", pr).Output()
		if err != nil {
			return "", fmt.Errorf("gh pr diff %s failed: %w", pr, err)
		}
	} else {
		if base == "" {
			return "", fmt.Errorf("base branch is required when pr is empty")
		}
		out, err = exec.Command("git", "diff", base+"...HEAD").Output()
		if err != nil {
			return "", fmt.Errorf("git diff %s...HEAD failed: %w", base, err)
		}
	}

	if len(out) > maxBytes {
		full := len(out)
		out = out[:maxBytes]
		out = append(out, []byte(fmt.Sprintf("\n[diff truncated at %d bytes; full size %d bytes]\n", maxBytes, full))...)
	}
	return string(out), nil
}

func runOK(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}

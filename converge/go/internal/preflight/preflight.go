package preflight

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// Run executes the pre-run check and writes a summary to w. Returns nil on
// PASS and a non-nil error listing failures on FAIL. Warnings always print
// to w but do not cause failure.
func Run(mode string, w io.Writer) error {
	switch mode {
	case "", "plan", "implement", "verify", "review":
	default:
		return fmt.Errorf("unknown mode: %s", mode)
	}

	var fails []string
	var warns []string

	// codex CLI
	if _, err := exec.LookPath("codex"); err != nil {
		fails = append(fails, "codex CLI not on PATH (install: npm install -g @openai/codex)")
	} else if err := exec.Command("codex", "--version").Run(); err != nil {
		fails = append(fails, "codex --version failed; binary may be broken")
	}

	// codex auth — file-based check; avoids burning a real call.
	authOK := false
	if os.Getenv("OPENAI_API_KEY") != "" {
		authOK = true
	} else {
		home := os.Getenv("CODEX_HOME")
		if home == "" {
			h, _ := os.UserHomeDir()
			home = filepath.Join(h, ".codex")
		}
		if _, err := os.Stat(filepath.Join(home, "auth.json")); err == nil {
			authOK = true
		}
	}
	if !authOK {
		fails = append(fails, "codex not authenticated (run `codex login` or set OPENAI_API_KEY)")
	}

	// git for modes that need it
	if mode == "implement" || mode == "verify" || mode == "review" {
		if err := exec.Command("git", "rev-parse", "--show-toplevel").Run(); err != nil {
			fails = append(fails, fmt.Sprintf("not inside a git repository (mode=%s requires one)", mode))
		}
	}
	if mode == "review" {
		if _, err := exec.LookPath("gh"); err != nil {
			warns = append(warns, "gh CLI not on PATH — PR-number form will not work; base-branch detection falls back to origin/HEAD")
		}
	}

	if len(fails) > 0 {
		fmt.Fprintln(w, "preflight: FAIL")
		for _, f := range fails {
			fmt.Fprintln(w, "  -", f)
		}
		for _, x := range warns {
			fmt.Fprintln(w, "  ~", x)
		}
		return fmt.Errorf("preflight failed (%d issue(s))", len(fails))
	}

	label := mode
	if label == "" {
		label = "unspecified"
	}
	fmt.Fprintf(w, "preflight: PASS (mode=%s)\n", label)
	for _, x := range warns {
		fmt.Fprintln(w, "  ~", x)
	}
	return nil
}

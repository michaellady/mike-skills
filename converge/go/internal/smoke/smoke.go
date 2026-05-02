// Package smoke runs a project-appropriate build or test command and prints
// PASS / FAIL.
package smoke

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
)

// Mode is "build" or "test".
type Mode string

const (
	Build Mode = "build"
	Test  Mode = "test"
)

// Run detects the project type and runs the matching command. It writes
// PASS/FAIL to stdoutW and the failing tail to stderrW. Returns nil on PASS,
// a non-nil error on FAIL or when no project type is detected.
func Run(mode Mode, stdoutW, stderrW io.Writer) error {
	cmd, err := commandFor(mode)
	if err != nil {
		return err
	}

	c := exec.Command("bash", "-c", cmd)
	var combined bytes.Buffer
	c.Stdout = &combined
	c.Stderr = &combined
	runErr := c.Run()

	if runErr == nil {
		fmt.Fprintf(stdoutW, "PASS (cmd: %s)\n", cmd)
		return nil
	}
	rc := -1
	if exitErr, ok := runErr.(*exec.ExitError); ok {
		rc = exitErr.ExitCode()
	}
	fmt.Fprintf(stdoutW, "FAIL (cmd: %s, exit: %d)\n", cmd, rc)
	fmt.Fprintln(stderrW, "--- last lines ---")
	tail(combined.Bytes(), 40, stderrW)
	return fmt.Errorf("smoke check failed")
}

func commandFor(mode Mode) (string, error) {
	if v := os.Getenv("CONVERGE_SMOKE_BUILD"); v != "" && mode == Build {
		return v, nil
	}
	if v := os.Getenv("CONVERGE_SMOKE_TEST"); v != "" && mode == Test {
		return v, nil
	}
	if exists("go.mod") {
		if mode == Build {
			return "go build ./...", nil
		}
		return "go test ./...", nil
	}
	if exists("Cargo.toml") {
		if mode == Build {
			return "cargo check", nil
		}
		return "cargo test", nil
	}
	if exists("package.json") {
		if cmd := npmScript(mode); cmd != "" {
			return cmd, nil
		}
		if mode == Build && exists("tsconfig.json") {
			return "npx tsc --noEmit", nil
		}
		return "", fmt.Errorf("package.json present but no `%s` script", mode)
	}
	if exists("pyproject.toml") || exists("setup.py") {
		if mode == Build {
			return "python -m compileall -q .", nil
		}
		return "pytest -q", nil
	}
	return "", fmt.Errorf("no recognized project type (go.mod, Cargo.toml, package.json, pyproject.toml)")
}

func npmScript(mode Mode) string {
	b, err := os.ReadFile("package.json")
	if err != nil {
		return ""
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if json.Unmarshal(b, &pkg) != nil {
		return ""
	}
	if mode == Build {
		if _, ok := pkg.Scripts["build"]; ok {
			return "npm run build"
		}
	} else {
		if _, ok := pkg.Scripts["test"]; ok {
			return "npm test"
		}
	}
	return ""
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func tail(b []byte, n int, w io.Writer) {
	lines := bytes.Split(b, []byte("\n"))
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	w.Write(bytes.Join(lines, []byte("\n")))
	if len(b) > 0 && b[len(b)-1] != '\n' {
		fmt.Fprintln(w)
	}
}

// Package muse implements the Muse Code CLI provider.
//
// It runs `muse exec --json --prompt-file <file>` (headless, non-interactive),
// parses the MSP wire-schema JSONL event stream on stdout, captures the
// session id (so runs stay attributable via the CLI's own session log), and
// writes the run-terminal text to stdout.
//
// Wire shape (probed against `muse exec --provider echo --json`):
//
//	{"stream":{"kind":"session","id":"<session-uuid>"},"payload_type":"run.output.delta",
//	 "payload":{"kind":"run_output_delta","text":"<chunk>"}}
//	{"stream":{...},"payload_type":"run.terminal.completed",
//	 "payload":{"kind":"run_terminal","terminal":"completed","text":"<final>","reason":null}}
//
// One-shot only: `muse resume` is interactive (session picker), so unlike
// codex/claude this provider ignores opts.ResumeID — converge treats muse the
// same as agy/composer-2.5/grok-build and carries the round delta in the
// prompt on later rounds.
package muse

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/michaellady/mike-skills/llm-provider/provider"
)

// Provider satisfies provider.Provider for the Muse Code CLI.
type Provider struct{}

// New returns a fresh muse provider.
func New() *Provider { return &Provider{} }

// Name implements provider.Provider.
func (*Provider) Name() string { return "muse" }

// Run implements provider.Provider.
func (*Provider) Run(ctx context.Context, opts provider.Options) error {
	if opts.PromptFile == "" {
		return provider.NewError(provider.ExitBadArgs, "prompt file is required")
	}
	if _, err := os.Stat(opts.PromptFile); err != nil {
		return provider.NewError(provider.ExitBadArgs, "prompt file not found: %s", opts.PromptFile)
	}
	if _, err := exec.LookPath("muse"); err != nil {
		return provider.NewError(provider.ExitBadArgs, "muse CLI not on PATH (install Muse Code first)")
	}
	if opts.Timeout == 0 {
		opts.Timeout = 5 * time.Minute
		if v := os.Getenv("CONVERGE_MUSE_TIMEOUT"); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				opts.Timeout = time.Duration(n) * time.Second
			}
		}
	}
	if opts.HeartbeatS == 0 {
		opts.HeartbeatS = 5
		if v := os.Getenv("CONVERGE_HEARTBEAT_S"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				opts.HeartbeatS = n
			}
		}
	}
	if !opts.Quiet && os.Getenv("CONVERGE_QUIET") != "" && os.Getenv("CONVERGE_QUIET") != "0" {
		opts.Quiet = true
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.ThreadOut == "" {
		opts.ThreadOut = fmt.Sprintf("/tmp/converge-thread-%d.txt", os.Getpid())
	}
	// Model selection: explicit opts.Model > $CONVERGE_MUSE_MODEL > the CLI's
	// own default. Deliberately no baked-in default — model ids are
	// probe-before-wire values, and the CLI default is the live one.
	model := opts.Model
	if model == "" {
		model = os.Getenv("CONVERGE_MUSE_MODEL")
	}
	// Effort maps 1:1 onto the CLI's --reasoning-effort
	// (none|minimal|low|medium|high|xhigh|ultra); xhigh matches the
	// codex/claude providers' default rigor.
	if opts.Effort == "" {
		opts.Effort = "xhigh"
	}
	effort := opts.Effort

	// Fresh session id per run, passed explicitly so the captured id is known
	// upfront (the event stream echoes it back as stream.id).
	sessionID := uuidV4()
	args := []string{
		"exec",
		"--json",
		"--prompt-file", opts.PromptFile,
		"--session-id", sessionID,
		"--reasoning-effort", effort,
		"--disable-approval",
	}
	if model != "" {
		args = append(args, "--model", model)
	}

	cctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, "muse", args...)
	cmd.Stdin = nil
	// Kill muse's whole process group (not just the direct pid) on
	// cancellation, and bound cmd.Wait() so a straggler holding the stdout
	// pipe can't wedge the fan-out past the merge.
	harden(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return provider.NewError(provider.ExitBadArgs, "stdout pipe: %v", err)
	}
	var errBuf strings.Builder
	cmd.Stderr = &errBuf

	if err := cmd.Start(); err != nil {
		return provider.NewError(provider.ExitBadArgs, "start muse: %v", err)
	}

	final, failed := streamFilter(stdout, opts)

	waitErr := cmd.Wait()

	if errors.Is(cctx.Err(), context.DeadlineExceeded) {
		return provider.NewError(provider.ExitTimeout, "muse timed out after %s", opts.Timeout)
	}
	stderr := errBuf.String()
	if failed.auth || isAuthError(stderr) {
		return provider.NewError(provider.ExitAuthError, "muse auth error — run `muse login` or set META_API_KEY")
	}
	if final == "" {
		msg := "no run-terminal text in muse JSONL stream"
		if failed.reason != "" {
			msg += " (terminal: " + trim(failed.reason, 300) + ")"
		} else if errBuf.Len() > 0 {
			tail := stderr
			if len(tail) > 500 {
				tail = tail[:500]
			}
			msg += " (stderr: " + tail + ")"
		}
		return provider.NewError(provider.ExitNoFinalMsg, "%s", msg)
	}
	if waitErr != nil {
		fmt.Fprintln(opts.Stderr, "[muse] note: exited non-zero:", waitErr)
	}

	_ = os.MkdirAll(filepath.Dir(opts.ThreadOut), 0o755)
	_ = os.WriteFile(opts.ThreadOut, []byte(sessionID), 0o644)

	_, _ = io.WriteString(opts.Stdout, final)
	return nil
}

// terminalFailure records a non-completed run terminal.
type terminalFailure struct {
	auth   bool
	reason string
}

// streamFilter parses muse's --json MSP event stream. Heartbeat lines go to
// stderr; the run-terminal text is the final answer. Unknown lines and event
// shapes are skipped — the schema may grow without breaking this parser.
func streamFilter(r io.Reader, opts provider.Options) (final string, failed terminalFailure) {
	start := time.Now()
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024) // up to 8MB lines

	logf := func(format string, args ...any) {
		if opts.Quiet {
			return
		}
		fmt.Fprintf(opts.Stderr, "[muse %ds] ", int(time.Since(start).Seconds()))
		fmt.Fprintf(opts.Stderr, format+"\n", args...)
	}
	logf("starting (effort=%s, timeout=%s)", opts.Effort, opts.Timeout)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var ev event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		switch ev.PayloadType {
		case "run.output.delta":
			if t := ev.Payload.Text; t != "" {
				logf("output: %s", trim(t, 80))
			}
		case "run.terminal.completed":
			switch ev.Payload.Terminal {
			case "completed":
				if ev.Payload.Text != "" {
					final = ev.Payload.Text
					logf("done (final message: %d chars)", len(final))
				}
			default:
				reason := ev.Payload.Reason
				if reason == "" {
					reason = ev.Payload.Text
				}
				if reason == "" {
					reason = "terminal=" + ev.Payload.Terminal
				}
				failed.reason = reason
				if isAuthError(reason) {
					failed.auth = true
				}
				logf("terminal %s: %s", ev.Payload.Terminal, trim(reason, 200))
			}
		case "run.lifecycle.started":
			logf("run started")
		}
	}
	if final == "" && failed.reason == "" {
		logf("ERROR: no run-terminal event in stream")
	}
	return final, failed
}

// event covers the MSP envelope fields this provider reads. Everything else
// is ignored so schema additions stay non-fatal.
type event struct {
	PayloadType string `json:"payload_type"`
	Payload     struct {
		Kind     string `json:"kind"`
		Text     string `json:"text"`
		Terminal string `json:"terminal"`
		Reason   string `json:"reason"`
	} `json:"payload"`
}

func trim(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func isAuthError(s string) bool {
	low := strings.ToLower(s)
	for _, k := range []string{
		"missing meta credentials",
		"muse login",
		"meta_api_key",
		"not authenticated",
		"unauthor",
		"authentication",
		"401",
		"403",
	} {
		if strings.Contains(low, k) {
			return true
		}
	}
	return false
}

// uuidV4 returns a randomly generated RFC 4122 v4 UUID, passed as
// --session-id so the session is known upfront (mirrors the claude provider).
func uuidV4() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

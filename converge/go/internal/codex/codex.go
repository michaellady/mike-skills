// Package codex runs `codex exec` (optionally with `resume <thread>`),
// streams JSONL events to stderr in human-readable form so the caller can
// see codex is alive, captures the thread id from `thread.started`, and
// emits the final assistant message on stdout.
package codex

import (
	"bufio"
	"context"
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
)

// Options configures a single codex critique call.
type Options struct {
	PromptFile  string
	Effort      string        // default "xhigh"
	ResumeID    string        // empty = fresh thread
	Timeout     time.Duration // default 5 min, overridable via env
	Quiet       bool          // suppress stderr heartbeat
	HeartbeatS  int           // min seconds between idle heartbeats (default 5)
	ThreadOut   string        // path to write captured thread id (round 1 only)
	Stderr      io.Writer
	Stdout      io.Writer
}

// Common exit codes returned by Run via *Error.
const (
	ExitBadArgs      = 2
	ExitAuthError    = 3
	ExitTimeout      = 4
	ExitNoFinalMsg   = 5
)

// Error carries an exit code so the CLI layer can map it through.
type Error struct {
	Code int
	Err  error
}

func (e *Error) Error() string { return e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

func newErr(code int, format string, args ...any) error {
	return &Error{Code: code, Err: fmt.Errorf(format, args...)}
}

// Run executes the codex call described by opts. On success the final
// assistant message is written to opts.Stdout. The thread id (when
// captured) is written to opts.ThreadOut for resume in later rounds.
func Run(ctx context.Context, opts Options) error {
	if opts.PromptFile == "" {
		return newErr(ExitBadArgs, "prompt file is required")
	}
	if _, err := os.Stat(opts.PromptFile); err != nil {
		return newErr(ExitBadArgs, "prompt file not found: %s", opts.PromptFile)
	}
	if _, err := exec.LookPath("codex"); err != nil {
		return newErr(ExitBadArgs, "codex CLI not on PATH (npm install -g @openai/codex)")
	}
	if opts.Effort == "" {
		opts.Effort = "xhigh"
	}
	if opts.Timeout == 0 {
		opts.Timeout = 5 * time.Minute
		if v := os.Getenv("CONVERGE_CODEX_TIMEOUT"); v != "" {
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

	prompt, err := os.ReadFile(opts.PromptFile)
	if err != nil {
		return newErr(ExitBadArgs, "cannot read prompt: %w", err)
	}

	args := []string{"exec"}
	if opts.ResumeID != "" {
		args = append(args, "resume", opts.ResumeID)
	}
	args = append(args,
		"--skip-git-repo-check",
		string(prompt),
		"-s", "read-only",
		"-c", fmt.Sprintf(`model_reasoning_effort="%s"`, opts.Effort),
		"--enable", "web_search_cached",
		"--json",
	)

	cctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, "codex", args...)
	cmd.Stdin = nil

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return newErr(ExitBadArgs, "stdout pipe: %w", err)
	}
	var errBuf strings.Builder
	cmd.Stderr = &errBuf

	if err := cmd.Start(); err != nil {
		return newErr(ExitBadArgs, "start codex: %w", err)
	}

	final, threadID := streamFilter(stdout, opts)

	waitErr := cmd.Wait()

	if errors.Is(cctx.Err(), context.DeadlineExceeded) {
		return newErr(ExitTimeout, "codex timed out after %s", opts.Timeout)
	}
	if isAuthError(errBuf.String()) {
		return newErr(ExitAuthError, "codex auth error — run `codex login`")
	}
	if final == "" {
		msg := "no final assistant message in JSONL stream"
		if errBuf.Len() > 0 {
			tail := errBuf.String()
			if len(tail) > 500 {
				tail = tail[:500]
			}
			msg += " (stderr: " + tail + ")"
		}
		return newErr(ExitNoFinalMsg, "%s", msg)
	}
	if waitErr != nil {
		// codex exited non-zero but we still got a final message — surface
		// stderr to the caller's stderr but keep the exit successful.
		fmt.Fprintln(opts.Stderr, "[codex] note: exited non-zero:", waitErr)
	}

	if opts.ResumeID == "" && threadID != "" {
		_ = os.MkdirAll(filepath.Dir(opts.ThreadOut), 0o755)
		_ = os.WriteFile(opts.ThreadOut, []byte(threadID), 0o644)
	}

	_, _ = io.WriteString(opts.Stdout, final)
	return nil
}

func streamFilter(r io.Reader, opts Options) (final, threadID string) {
	start := time.Now()
	lastLog := start
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024) // up to 4MB lines

	logf := func(format string, args ...any) {
		if opts.Quiet {
			return
		}
		fmt.Fprintf(opts.Stderr, "[codex %ds] ", int(time.Since(start).Seconds()))
		fmt.Fprintf(opts.Stderr, format+"\n", args...)
	}
	mode := "fresh"
	if opts.ResumeID != "" {
		mode = "resume"
	}
	logf("starting (%s, effort=%s, timeout=%s)", mode, opts.Effort, opts.Timeout)

	heartbeat := time.Duration(opts.HeartbeatS) * time.Second

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			if !opts.Quiet && time.Since(lastLog) >= heartbeat {
				logf("…still running")
				lastLog = time.Now()
			}
			continue
		}
		var ev event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		switch {
		case ev.Type == "thread.started":
			if ev.ThreadID != "" && threadID == "" {
				threadID = ev.ThreadID
			}
			tid := threadID
			if len(tid) > 8 {
				tid = tid[:8]
			}
			logf("thread %s started", tid)
		case ev.Type == "turn.started":
			logf("turn started")
		case ev.Item.ItemType == "reasoning" || strings.HasSuffix(ev.Type, "reasoning"):
			text := ev.Item.Text
			if text == "" {
				text = ev.Text
			}
			if text == "" {
				text = ev.Item.Summary
			}
			if text != "" {
				logf("reasoning: %s", trim(text, 80))
			}
		case (ev.Type == "tool_call" || ev.Type == "item.started") && (ev.Item.ItemType == "tool_call" || ev.Item.ItemType == "command_execution"):
			name := ev.Item.Tool
			if name == "" {
				name = ev.Item.Name
			}
			if name == "" {
				name = "tool"
			}
			a := ev.Item.Arguments
			if a == "" {
				a = ev.Item.Command
			}
			logf("tool: %s %s", name, trim(a, 60))
		case ev.Type == "agent_message" || ev.Type == "assistant_message" || ev.Type == "message":
			if ev.Text != "" {
				final = ev.Text
				logf("message: %s", trim(final, 80))
			}
		case ev.Type == "item.completed" && (ev.Item.ItemType == "agent_message" || ev.Item.ItemType == "assistant_message"):
			if ev.Item.Text != "" {
				final = ev.Item.Text
				logf("message: %s", trim(final, 80))
			}
		case ev.Type == "turn.completed" || ev.Type == "thread.completed":
			logf("turn complete")
		}
		lastLog = time.Now()
	}
	if final == "" {
		logf("ERROR: no final assistant message")
	} else {
		logf("done (final message: %d chars)", len(final))
	}
	return final, threadID
}

type event struct {
	Type     string `json:"type"`
	ThreadID string `json:"thread_id,omitempty"`
	Text     string `json:"text,omitempty"`
	Item     struct {
		ItemType  string `json:"type"`
		Text      string `json:"text"`
		Summary   string `json:"summary"`
		Tool      string `json:"tool"`
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
		Command   string `json:"command"`
	} `json:"item,omitempty"`
}

func trim(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func isAuthError(stderr string) bool {
	low := strings.ToLower(stderr)
	for _, k := range []string{"not authenticated", "401", "unauthor"} {
		if strings.Contains(low, k) {
			return true
		}
	}
	return false
}

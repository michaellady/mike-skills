// Package codex implements the codex-CLI provider.
// It runs `codex exec` (optionally `resume <thread>`), streams JSONL events
// to stderr in human-readable form, captures the thread id from
// `thread.started`, and writes the final assistant message to stdout.
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

	"github.com/michaellady/mike-skills/llm-provider/provider"
)

// Provider satisfies provider.Provider for the OpenAI Codex CLI.
type Provider struct{}

// New returns a fresh codex provider.
func New() *Provider { return &Provider{} }

// Name implements provider.Provider.
func (*Provider) Name() string { return "codex" }

// Run implements provider.Provider.
func (*Provider) Run(ctx context.Context, opts provider.Options) error {
	if opts.PromptFile == "" {
		return provider.NewError(provider.ExitBadArgs, "prompt file is required")
	}
	if _, err := os.Stat(opts.PromptFile); err != nil {
		return provider.NewError(provider.ExitBadArgs, "prompt file not found: %s", opts.PromptFile)
	}
	if _, err := exec.LookPath("codex"); err != nil {
		return provider.NewError(provider.ExitBadArgs, "codex CLI not on PATH (npm install -g @openai/codex)")
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
		return provider.NewError(provider.ExitBadArgs, "cannot read prompt: %v", err)
	}

	args := []string{"exec"}
	if opts.ResumeID != "" {
		args = append(args, "resume", opts.ResumeID)
	}
	args = append(args,
		"--skip-git-repo-check",
		string(prompt),
	)
	// `-s/--sandbox` is exec-fresh-only; `codex exec resume` inherits sandbox
	// from the original session and rejects -s with "unexpected argument '-s'".
	if opts.ResumeID == "" {
		args = append(args, "-s", "read-only")
	}
	args = append(args,
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
		return provider.NewError(provider.ExitBadArgs, "stdout pipe: %v", err)
	}
	var errBuf strings.Builder
	cmd.Stderr = &errBuf

	if err := cmd.Start(); err != nil {
		return provider.NewError(provider.ExitBadArgs, "start codex: %v", err)
	}

	final, threadID := streamFilter(stdout, opts)

	waitErr := cmd.Wait()

	if errors.Is(cctx.Err(), context.DeadlineExceeded) {
		return provider.NewError(provider.ExitTimeout, "codex timed out after %s", opts.Timeout)
	}
	if isAuthError(errBuf.String()) {
		return provider.NewError(provider.ExitAuthError, "codex auth error — run `codex login`")
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
		return provider.NewError(provider.ExitNoFinalMsg, "%s", msg)
	}
	if waitErr != nil {
		fmt.Fprintln(opts.Stderr, "[codex] note: exited non-zero:", waitErr)
	}

	if opts.ResumeID == "" && threadID != "" {
		_ = os.MkdirAll(filepath.Dir(opts.ThreadOut), 0o755)
		_ = os.WriteFile(opts.ThreadOut, []byte(threadID), 0o644)
	}

	_, _ = io.WriteString(opts.Stdout, final)
	return nil
}

func streamFilter(r io.Reader, opts provider.Options) (final, threadID string) {
	start := time.Now()
	lastLog := start
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

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

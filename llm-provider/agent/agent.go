// Package agent implements the Cursor `agent` CLI provider.
// It runs `agent --print --output-format text <prompt>` and captures
// the final stdout as the assistant's reply.
//
// Cursor agent's stream-json format is richer (includes chat-id events
// for resume support) but text mode is enough for one-shot critique calls.
// Switch to stream-json + JSONL parsing if/when resume becomes important.
package agent

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/michaellady/mike-skills/llm-provider/provider"
)

// Provider satisfies provider.Provider for the Cursor agent CLI.
type Provider struct{}

// New returns a fresh agent provider.
func New() *Provider { return &Provider{} }

// Name implements provider.Provider.
func (*Provider) Name() string { return "agent" }

// Run implements provider.Provider.
func (*Provider) Run(ctx context.Context, opts provider.Options) error {
	if opts.PromptFile == "" {
		return provider.NewError(provider.ExitBadArgs, "prompt file is required")
	}
	if _, err := os.Stat(opts.PromptFile); err != nil {
		return provider.NewError(provider.ExitBadArgs, "prompt file not found: %s", opts.PromptFile)
	}
	if _, err := exec.LookPath("agent"); err != nil {
		return provider.NewError(provider.ExitBadArgs, "agent CLI not on PATH (install Cursor agent)")
	}
	if opts.Timeout == 0 {
		opts.Timeout = 5 * time.Minute
		if v := os.Getenv("CONVERGE_AGENT_TIMEOUT"); v != "" {
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

	prompt, err := os.ReadFile(opts.PromptFile)
	if err != nil {
		return provider.NewError(provider.ExitBadArgs, "cannot read prompt: %v", err)
	}

	args := []string{"--print", "--output-format", "text", "--trust"}
	model := opts.Model
	if model == "" {
		model = os.Getenv("CONVERGE_AGENT_MODEL")
	}
	if model == "" {
		// Cursor free plans reject any "named model" and require explicit
		// --model auto. Paid plans accept named models too. "auto" is the
		// safest default — works on both.
		model = "auto"
	}
	args = append(args, "--model", model)
	if opts.ResumeID != "" {
		args = append(args, "--resume", opts.ResumeID)
	}
	args = append(args, string(prompt))

	cctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, "agent", args...)
	cmd.Stdin = nil

	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	if !opts.Quiet {
		fmt.Fprintf(opts.Stderr, "[agent] starting (timeout=%s)\n", opts.Timeout)
	}

	runErr := cmd.Run()

	if cctx.Err() == context.DeadlineExceeded {
		return provider.NewError(provider.ExitTimeout, "agent timed out after %s", opts.Timeout)
	}
	if isAuthError(errBuf.String()) {
		return provider.NewError(provider.ExitAuthError, "agent auth error — run `agent login`")
	}

	final := strings.TrimSpace(outBuf.String())
	if final == "" {
		msg := "no output from agent"
		if errBuf.Len() > 0 {
			tail := errBuf.String()
			if len(tail) > 500 {
				tail = tail[:500]
			}
			msg += " (stderr: " + tail + ")"
		}
		return provider.NewError(provider.ExitNoFinalMsg, "%s", msg)
	}
	if runErr != nil {
		fmt.Fprintln(opts.Stderr, "[agent] note: exited non-zero:", runErr)
	}

	if !opts.Quiet {
		fmt.Fprintf(opts.Stderr, "[agent] done (final message: %d chars)\n", len(final))
	}

	_, _ = io.WriteString(opts.Stdout, final)
	return nil
}

func isAuthError(stderr string) bool {
	low := strings.ToLower(stderr)
	for _, k := range []string{
		"not authenticated",
		"401",
		"unauthor",
		"please log in",
		"login required",
	} {
		if strings.Contains(low, k) {
			return true
		}
	}
	return false
}

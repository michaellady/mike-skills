// Package agy implements the `agy` CLI provider — a non-interactive agent CLI
// invoked as `agy --print <prompt> --dangerously-skip-permissions`, capturing
// stdout as the assistant's reply.
//
// This replaces the deprecated `gemini` provider. As of this migration no
// in-repo consumer wires gemini anymore (converge and adversarial-review both
// use agy); the gemini package remains in llm-provider only for any external
// consumers, and new consumers should prefer agy.
package agy

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

// Provider satisfies provider.Provider for the `agy` CLI.
type Provider struct{}

// New returns a fresh agy provider.
func New() *Provider { return &Provider{} }

// Name implements provider.Provider.
func (*Provider) Name() string { return "agy" }

// Run implements provider.Provider.
func (*Provider) Run(ctx context.Context, opts provider.Options) error {
	if opts.PromptFile == "" {
		return provider.NewError(provider.ExitBadArgs, "prompt file is required")
	}
	if _, err := os.Stat(opts.PromptFile); err != nil {
		return provider.NewError(provider.ExitBadArgs, "prompt file not found: %s", opts.PromptFile)
	}
	if _, err := exec.LookPath("agy"); err != nil {
		return provider.NewError(provider.ExitBadArgs, "agy CLI not on PATH")
	}
	if opts.Timeout == 0 {
		opts.Timeout = 5 * time.Minute
		if v := os.Getenv("CONVERGE_AGY_TIMEOUT"); v != "" {
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

	// `agy --print <prompt>` runs a single prompt non-interactively and prints
	// the response to stdout; --dangerously-skip-permissions avoids interactive
	// tool-permission prompts that would otherwise block headless use.
	args := []string{"--print", string(prompt), "--dangerously-skip-permissions"}

	cctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, "agy", args...)
	cmd.Stdin = nil

	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	if !opts.Quiet {
		fmt.Fprintf(opts.Stderr, "[agy] starting (timeout=%s)\n", opts.Timeout)
	}

	runErr := cmd.Run()

	if cctx.Err() == context.DeadlineExceeded {
		return provider.NewError(provider.ExitTimeout, "agy timed out after %s", opts.Timeout)
	}
	if isAuthError(errBuf.String()) {
		return provider.NewError(provider.ExitAuthError, "agy auth error — run `agy` once interactively to authenticate")
	}

	final := strings.TrimSpace(outBuf.String())
	if final == "" {
		msg := "no output from agy"
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
		fmt.Fprintln(opts.Stderr, "[agy] note: exited non-zero:", runErr)
	}

	if !opts.Quiet {
		fmt.Fprintf(opts.Stderr, "[agy] done (final message: %d chars)\n", len(final))
	}

	_, _ = io.WriteString(opts.Stdout, final)
	return nil
}

func isAuthError(stderr string) bool {
	low := strings.ToLower(stderr)
	for _, k := range []string{
		"not authenticated",
		"401",
		"403",
		"unauthor",
		"please log in",
		"login required",
		"api key",
		"missing credentials",
	} {
		if strings.Contains(low, k) {
			return true
		}
	}
	return false
}

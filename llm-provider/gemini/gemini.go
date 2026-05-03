// Package gemini implements the Google `gemini` CLI provider.
// It runs `gemini --prompt <prompt> --output-format text --skip-trust` and
// captures the final stdout as the assistant's reply.
//
// The CLI also supports `--output-format stream-json` for richer telemetry
// (tool calls, intermediate reasoning); switch to that mode if heartbeat
// output ever becomes important.
package gemini

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

// Provider satisfies provider.Provider for the Google Gemini CLI.
type Provider struct{}

// New returns a fresh gemini provider.
func New() *Provider { return &Provider{} }

// Name implements provider.Provider.
func (*Provider) Name() string { return "gemini" }

// Run implements provider.Provider.
func (*Provider) Run(ctx context.Context, opts provider.Options) error {
	if opts.PromptFile == "" {
		return provider.NewError(provider.ExitBadArgs, "prompt file is required")
	}
	if _, err := os.Stat(opts.PromptFile); err != nil {
		return provider.NewError(provider.ExitBadArgs, "prompt file not found: %s", opts.PromptFile)
	}
	if _, err := exec.LookPath("gemini"); err != nil {
		return provider.NewError(provider.ExitBadArgs, "gemini CLI not on PATH (install via npm i -g @google/gemini-cli)")
	}
	if opts.Timeout == 0 {
		opts.Timeout = 5 * time.Minute
		if v := os.Getenv("CONVERGE_GEMINI_TIMEOUT"); v != "" {
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

	args := []string{"--prompt", string(prompt), "--output-format", "text", "--skip-trust"}
	model := opts.Model
	if model == "" {
		model = os.Getenv("CONVERGE_GEMINI_MODEL")
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	if opts.ResumeID != "" {
		args = append(args, "--resume", opts.ResumeID)
	}

	cctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, "gemini", args...)
	cmd.Stdin = nil

	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	if !opts.Quiet {
		fmt.Fprintf(opts.Stderr, "[gemini] starting (timeout=%s)\n", opts.Timeout)
	}

	runErr := cmd.Run()

	if cctx.Err() == context.DeadlineExceeded {
		return provider.NewError(provider.ExitTimeout, "gemini timed out after %s", opts.Timeout)
	}
	if isAuthError(errBuf.String()) {
		return provider.NewError(provider.ExitAuthError, "gemini auth error — run `gemini` once interactively to authenticate or set GEMINI_API_KEY")
	}

	final := strings.TrimSpace(outBuf.String())
	if final == "" {
		msg := "no output from gemini"
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
		fmt.Fprintln(opts.Stderr, "[gemini] note: exited non-zero:", runErr)
	}

	if !opts.Quiet {
		fmt.Fprintf(opts.Stderr, "[gemini] done (final message: %d chars)\n", len(final))
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

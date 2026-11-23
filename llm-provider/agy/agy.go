// Package agy implements the `agy` CLI provider — a non-interactive agent CLI
// invoked as `agy --print <prompt> --mode plan --sandbox`, capturing
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
		// xhigh-effort Gemini reviews measured ~10 min; 5 min timed out.
		opts.Timeout = 15 * time.Minute
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
	// the response to stdout. Reviews retain permissions and sandbox protection.
	//
	// Headless agy cannot prompt for tool permission: the first shell command it
	// tries is auto-denied and the whole turn then ends with an EMPTY response
	// ("a tool required the \"command\" permission ..."), which surfaced here as
	// ExitNoFinalMsg on every real review. Two guards, neither of which widens
	// what the reviewer may do:
	//   1. headlessPreamble tells the model not to call the shell at all and to
	//      answer from the prompt (plus a few file reads).
	//   2. A session-only allowlist (--settings, never saved) of read-only
	//      inspection commands, so a stray `ls`/`git status` is not fatal.
	// Set CONVERGE_AGY_NO_PREAMBLE=1 to send the prompt verbatim.
	body := string(prompt)
	if os.Getenv("CONVERGE_AGY_NO_PREAMBLE") == "" {
		body = headlessPreamble + body
	}
	args := []string{"--print", body, "--mode", "plan", "--sandbox", "--settings", readOnlySettings}
	// Model selection: explicit opts.Model > $CONVERGE_AGY_MODEL > agy's own session
	// default. The id form (e.g. "gemini-3.1-pro-high") is stable; `agy models` lists them.
	model := opts.Model
	if model == "" {
		model = os.Getenv("CONVERGE_AGY_MODEL")
	}
	if model == "" {
		model = "gemini-4-argon-high"
	}
	if model != "" {
		args = append(args, "--model", model)
	}

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

// headlessPreamble is prepended to every prompt sent to agy in print mode.
const headlessPreamble = `HEADLESS REVIEW RULES (these override any habit of exploring the repository): You are running non-interactively. Do NOT call run_command or any shell tool at all: in this mode a single denied command aborts the whole run with no output. The complete artifact under review is included in this prompt. You may call read_file at most 4 times if you must check a specific cited file. Do not build, test, install, or write anything. Then give your final answer; a run that ends without one is a failure.

`

// readOnlySettings is passed via --settings (session-only). It allowlists
// read-only inspection commands so one of them does not abort a headless run.
const readOnlySettings = `{"permissions":{"allow":["command(git status)","command(git diff)","command(git log)","command(git show)","command(git ls-files)","command(git grep)","command(ls)","command(find)","command(cat)","command(head)","command(tail)","command(sed -n)","command(wc)","command(grep)","command(rg)","command(echo)","command(true)"]}}`

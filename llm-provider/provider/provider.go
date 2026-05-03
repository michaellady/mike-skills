// Package provider defines the LLM transport interface used by callers
// that need to drive the Claude or Codex CLI from Go. Implementations
// live in sibling subpackages (claude, codex, ...) of this module.
//
// Originally extracted from mike-skills/converge so that other skills
// (adversarial-review, etc.) can share the same CLI dispatch transport
// without depending on converge.
//
// Adding a new provider:
//  1. Create <module>/<name>/<name>.go with `type Provider struct{}`
//     satisfying the Provider interface (Name, Run).
//  2. Have the caller register it in its own dispatch table.
package provider

import (
	"context"
	"fmt"
	"io"
	"time"
)

// Options configures a single critique call. Fields shared across providers.
// Provider-specific knobs (codex's `model_reasoning_effort`, claude's
// `--effort`, model overrides, etc.) are translated by each implementation.
type Options struct {
	PromptFile string
	Effort     string        // provider-specific; e.g. codex/claude both accept low|medium|high|xhigh
	ResumeID   string        // codex: thread id; claude: session uuid; "" = fresh session
	Timeout    time.Duration // 0 = provider default
	Quiet      bool          // suppress stderr heartbeat lines
	HeartbeatS int           // min seconds between idle heartbeat lines
	ThreadOut  string        // path to write captured session/thread id (round 1 only)
	Model      string        // optional provider-specific model override
	Stderr     io.Writer
	Stdout     io.Writer
}

// Provider is the contract every LLM transport implements.
type Provider interface {
	// Name returns the canonical provider name ("codex", "claude").
	Name() string
	// Run executes one critique call. On success the final assistant
	// message is written to opts.Stdout. The captured session/thread id
	// (when fresh, ResumeID == "") is written to opts.ThreadOut.
	Run(ctx context.Context, opts Options) error
}

// Common exit codes returned by Run via *Error so the CLI layer maps them
// through to the process exit code.
const (
	ExitBadArgs    = 2
	ExitAuthError  = 3
	ExitTimeout    = 4
	ExitNoFinalMsg = 5
)

// Error carries an exit code so the CLI layer can map it through.
type Error struct {
	Code int
	Err  error
}

func (e *Error) Error() string { return e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

// NewError builds an *Error with the given exit code and formatted message.
func NewError(code int, format string, args ...any) error {
	return &Error{Code: code, Err: fmt.Errorf(format, args...)}
}

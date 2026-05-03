// Package claude implements the Claude Code CLI provider.
// It runs `claude -p <prompt> --output-format stream-json`, streams the
// JSONL event stream to stderr in human-readable form, captures the session
// id (so the orchestrator can resume in later rounds), and writes the final
// assistant message to stdout.
package claude

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

	"github.com/michaellady/mike-skills/converge/internal/provider"
)

// Provider satisfies provider.Provider for the Claude Code CLI.
type Provider struct{}

// New returns a fresh claude provider.
func New() *Provider { return &Provider{} }

// Name implements provider.Provider.
func (*Provider) Name() string { return "claude" }

// Run implements provider.Provider.
func (*Provider) Run(ctx context.Context, opts provider.Options) error {
	if opts.PromptFile == "" {
		return provider.NewError(provider.ExitBadArgs, "prompt file is required")
	}
	if _, err := os.Stat(opts.PromptFile); err != nil {
		return provider.NewError(provider.ExitBadArgs, "prompt file not found: %s", opts.PromptFile)
	}
	if _, err := exec.LookPath("claude"); err != nil {
		return provider.NewError(provider.ExitBadArgs, "claude CLI not on PATH (install Claude Code first)")
	}
	if opts.Timeout == 0 {
		opts.Timeout = 5 * time.Minute
		if v := os.Getenv("CONVERGE_CLAUDE_TIMEOUT"); v != "" {
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
	model := opts.Model
	if model == "" {
		model = os.Getenv("CONVERGE_CLAUDE_MODEL")
	}
	if model == "" {
		model = "opus"
	}
	effort := opts.Effort
	if effort == "" {
		effort = "xhigh"
	}

	prompt, err := os.ReadFile(opts.PromptFile)
	if err != nil {
		return provider.NewError(provider.ExitBadArgs, "cannot read prompt: %v", err)
	}

	args := []string{"-p"}
	// Session id management: --resume to continue, otherwise --session-id with
	// a fresh UUID we generate so we can capture it for later resume.
	var sessionID string
	if opts.ResumeID != "" {
		args = append(args, "--resume", opts.ResumeID)
		sessionID = opts.ResumeID
	} else {
		sessionID = uuidV4()
		args = append(args, "--session-id", sessionID)
	}
	args = append(args,
		"--output-format", "stream-json",
		"--verbose", // required for stream-json output
		"--model", model,
		"--effort", effort,
	)
	// Prompt is positional after flags.
	args = append(args, string(prompt))

	cctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, "claude", args...)
	cmd.Stdin = nil

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return provider.NewError(provider.ExitBadArgs, "stdout pipe: %v", err)
	}
	var errBuf strings.Builder
	cmd.Stderr = &errBuf

	if err := cmd.Start(); err != nil {
		return provider.NewError(provider.ExitBadArgs, "start claude: %v", err)
	}

	final, capturedID, sawAuthErr := streamFilter(stdout, opts)
	if capturedID != "" {
		sessionID = capturedID
	}

	waitErr := cmd.Wait()

	if errors.Is(cctx.Err(), context.DeadlineExceeded) {
		return provider.NewError(provider.ExitTimeout, "claude timed out after %s", opts.Timeout)
	}
	if sawAuthErr || isAuthError(errBuf.String()) {
		return provider.NewError(provider.ExitAuthError, "claude auth error — run `claude auth` or set ANTHROPIC_API_KEY")
	}
	if final == "" {
		msg := "no final assistant message in stream-json output"
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
		fmt.Fprintln(opts.Stderr, "[claude] note: exited non-zero:", waitErr)
	}

	if opts.ResumeID == "" && sessionID != "" {
		_ = os.MkdirAll(filepath.Dir(opts.ThreadOut), 0o755)
		_ = os.WriteFile(opts.ThreadOut, []byte(sessionID), 0o644)
	}

	_, _ = io.WriteString(opts.Stdout, final)
	return nil
}

// streamFilter parses claude's stream-json output. Each line is a JSON
// envelope with a `type` field. We surface human-readable heartbeat lines
// to stderr and capture the final assistant text.
func streamFilter(r io.Reader, opts provider.Options) (final, sessionID string, authErr bool) {
	start := time.Now()
	lastLog := start
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024) // up to 8MB lines

	logf := func(format string, args ...any) {
		if opts.Quiet {
			return
		}
		fmt.Fprintf(opts.Stderr, "[claude %ds] ", int(time.Since(start).Seconds()))
		fmt.Fprintf(opts.Stderr, format+"\n", args...)
	}
	mode := "fresh"
	if opts.ResumeID != "" {
		mode = "resume"
	}
	logf("starting (%s, model=%s, effort=%s, timeout=%s)", mode, opts.Model, opts.Effort, opts.Timeout)

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
		switch ev.Type {
		case "system":
			if ev.Subtype == "init" && ev.SessionID != "" && sessionID == "" {
				sessionID = ev.SessionID
				sid := sessionID
				if len(sid) > 8 {
					sid = sid[:8]
				}
				logf("session %s started", sid)
			}
		case "assistant":
			// Streaming chunks of the assistant turn. We capture text
			// progressively but rely on the `result` event for the final
			// authoritative answer.
			if t := assistantText(ev); t != "" {
				logf("assistant: %s", trim(t, 80))
			}
		case "user":
			// Tool results / user-side events; usually quiet.
		case "result":
			if ev.IsError {
				low := strings.ToLower(ev.Result + " " + ev.Subtype)
				if strings.Contains(low, "auth") || strings.Contains(low, "401") || strings.Contains(low, "credential") {
					authErr = true
				}
				logf("ERROR: %s", trim(ev.Result, 200))
			} else if ev.Result != "" {
				final = ev.Result
				logf("done (final result: %d chars)", len(final))
			}
		}
		lastLog = time.Now()
	}
	if final == "" && !authErr {
		logf("ERROR: no result event in stream")
	}
	return final, sessionID, authErr
}

// event covers the fields we read from claude's stream-json output. Other
// fields are ignored; new event types are non-fatal.
type event struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
	Result    string `json:"result,omitempty"`
	Message   struct {
		Role    string `json:"role,omitempty"`
		Content json.RawMessage `json:"content,omitempty"`
	} `json:"message,omitempty"`
}

// assistantText extracts the first textual chunk from an assistant event's
// message.content array. Content can be a string or an array of typed blocks
// ([{type:"text", text:"..."}, ...]).
func assistantText(ev event) string {
	if len(ev.Message.Content) == 0 {
		return ""
	}
	// Try string form first.
	var s string
	if err := json.Unmarshal(ev.Message.Content, &s); err == nil {
		return s
	}
	// Fall back to array form.
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(ev.Message.Content, &blocks); err == nil {
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				return b.Text
			}
		}
	}
	return ""
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
	for _, k := range []string{"not authenticated", "401", "unauthor", "invalid api key", "authentication"} {
		if strings.Contains(low, k) {
			return true
		}
	}
	return false
}

// uuidV4 returns a randomly generated RFC 4122 v4 UUID. Used for
// --session-id when starting a fresh claude session so we know the id ahead
// of time and can write it to ThreadOut without parsing init events.
func uuidV4() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

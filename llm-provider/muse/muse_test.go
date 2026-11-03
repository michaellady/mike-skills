package muse

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michaellady/mike-skills/llm-provider/provider"
)

// stubScript is a fake `muse` CLI: its behavior switches on $STUB_MODE so
// each test drives one transport path without network or credentials.
const stubScript = `#!/bin/sh
if [ -n "$STUB_ARGS_FILE" ]; then
  printf '%s\n' "$@" > "$STUB_ARGS_FILE"
fi
case "$STUB_MODE" in
  ok)
    printf '%s\n' \
      '{"stream":{"kind":"session","id":"s-1"},"payload_type":"run.lifecycle.started","payload":{"kind":"run_started"}}' \
      '{"stream":{"kind":"session","id":"s-1"},"payload_type":"run.output.delta","payload":{"kind":"run_output_delta","text":"working…"}}' \
      '{"stream":{"kind":"session","id":"s-1"},"payload_type":"run.terminal.completed","payload":{"kind":"run_terminal","terminal":"completed","text":"STUB_FINAL","reason":null}}'
    ;;
  auth)
    echo "missing meta credentials: run 'muse login' or set META_API_KEY" >&2
    exit 1
    ;;
  failed)
    printf '%s\n' '{"stream":{"kind":"session","id":"s-1"},"payload_type":"run.terminal.completed","payload":{"kind":"run_terminal","terminal":"failed","text":"","reason":"quota exhausted"}}'
    exit 1
    ;;
  hang)
    sleep 30
    ;;
esac
`

// withStub puts the fake muse on PATH for the duration of the test and
// returns a prompt file plus stdout/stderr buffers via opts patching.
func withStub(t *testing.T, mode string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	stub := filepath.Join(dir, "muse")
	if err := os.WriteFile(stub, []byte(stubScript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("STUB_MODE", mode)
	prompt := filepath.Join(dir, "prompt.txt")
	if err := os.WriteFile(prompt, []byte("review this"), 0o644); err != nil {
		t.Fatal(err)
	}
	threadOut := filepath.Join(dir, "thread.txt")
	return prompt, threadOut
}

func TestRun_OK(t *testing.T) {
	prompt, threadOut := withStub(t, "ok")
	var out, errLog strings.Builder
	p := New()
	if err := p.Run(context.Background(), provider.Options{
		PromptFile: prompt,
		ThreadOut:  threadOut,
		Quiet:      true,
		Stdout:     &out,
		Stderr:     &errLog,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.String() != "STUB_FINAL" {
		t.Fatalf("stdout = %q, want %q", out.String(), "STUB_FINAL")
	}
	sid, err := os.ReadFile(threadOut)
	if err != nil {
		t.Fatalf("ThreadOut not written: %v", err)
	}
	if len(strings.TrimSpace(string(sid))) < 8 {
		t.Fatalf("ThreadOut = %q, want a session id", string(sid))
	}
	if p.Name() != "muse" {
		t.Fatalf("Name = %q, want muse", p.Name())
	}
}

// Check the transport contract: use the subscribed provider, retain guards,
// select the verified default, and honor explicit/environment overrides.
func TestRun_ModelAndPermissions(t *testing.T) {
	for _, tc := range []struct{ name, explicit, env, want string }{
		{"default", "", "", "muse-wonder-1.0"},
		{"environment", "", "env-model", "env-model"},
		{"explicit", "explicit-model", "env-model", "explicit-model"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prompt, thread := withStub(t, "ok")
			argsFile := filepath.Join(t.TempDir(), "args")
			t.Setenv("STUB_ARGS_FILE", argsFile)
			t.Setenv("CONVERGE_MUSE_MODEL", tc.env)
			err := New().Run(context.Background(), provider.Options{
				PromptFile: prompt, ThreadOut: thread, Model: tc.explicit,
				Quiet: true, Stdout: &strings.Builder{}, Stderr: &strings.Builder{},
			})
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(argsFile)
			if err != nil {
				t.Fatal(err)
			}
			args := "\n" + string(data)
			for _, want := range []string{"--provider\nmeta", "--model\n" + tc.want,
				"--disable-write", "--disable-shell", "--disable-web-tools", "--no-foreign-personal-context"} {
				if !strings.Contains(args, "\n"+want+"\n") {
					t.Fatalf("missing %q in args %q", want, args)
				}
			}
			for _, forbidden := range []string{"--disable-approval", "--disable-sandbox", "--yolo"} {
				if strings.Contains(args, "\n"+forbidden+"\n") {
					t.Fatalf("unsafe flag %q", forbidden)
				}
			}
		})
	}
}

func TestRun_AuthError(t *testing.T) {
	prompt, threadOut := withStub(t, "auth")
	var out strings.Builder
	err := New().Run(context.Background(), provider.Options{
		PromptFile: prompt,
		ThreadOut:  threadOut,
		Quiet:      true,
		Stdout:     &out,
		Stderr:     &strings.Builder{},
	})
	var pe *provider.Error
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v, want *provider.Error", err)
	}
	if pe.Code != provider.ExitAuthError {
		t.Fatalf("code = %d, want %d", pe.Code, provider.ExitAuthError)
	}
}

func TestRun_FailedTerminal(t *testing.T) {
	prompt, threadOut := withStub(t, "failed")
	var out strings.Builder
	err := New().Run(context.Background(), provider.Options{
		PromptFile: prompt,
		ThreadOut:  threadOut,
		Quiet:      true,
		Stdout:     &out,
		Stderr:     &strings.Builder{},
	})
	var pe *provider.Error
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v, want *provider.Error", err)
	}
	if pe.Code != provider.ExitNoFinalMsg {
		t.Fatalf("code = %d, want %d", pe.Code, provider.ExitNoFinalMsg)
	}
}

func TestRun_Timeout(t *testing.T) {
	prompt, threadOut := withStub(t, "hang")
	var out strings.Builder
	err := New().Run(context.Background(), provider.Options{
		PromptFile: prompt,
		ThreadOut:  threadOut,
		Quiet:      true,
		Timeout:    300 * time.Millisecond,
		Stdout:     &out,
		Stderr:     &strings.Builder{},
	})
	var pe *provider.Error
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v, want *provider.Error", err)
	}
	if pe.Code != provider.ExitTimeout {
		t.Fatalf("code = %d, want %d", pe.Code, provider.ExitTimeout)
	}
}

func TestRun_MissingPrompt(t *testing.T) {
	err := New().Run(context.Background(), provider.Options{
		PromptFile: "/nonexistent/prompt.txt",
		Stdout:     &strings.Builder{},
		Stderr:     &strings.Builder{},
	})
	var pe *provider.Error
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v, want *provider.Error", err)
	}
	if pe.Code != provider.ExitBadArgs {
		t.Fatalf("code = %d, want %d", pe.Code, provider.ExitBadArgs)
	}
}

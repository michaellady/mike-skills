// Package cli wires every subcommand into a single binary.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/michaellady/mike-skills/converge/internal/cleanup"
	"github.com/michaellady/mike-skills/converge/internal/dispatch"
	"github.com/michaellady/mike-skills/converge/internal/embedded"
	"github.com/michaellady/mike-skills/converge/internal/gitops"
	"github.com/michaellady/mike-skills/converge/internal/logwriter"
	"github.com/michaellady/mike-skills/converge/internal/plan"
	"github.com/michaellady/mike-skills/converge/internal/preflight"
	"github.com/michaellady/mike-skills/converge/internal/provider"
	"github.com/michaellady/mike-skills/converge/internal/schema"
	"github.com/michaellady/mike-skills/converge/internal/smoke"
	"github.com/michaellady/mike-skills/converge/internal/status"
	"github.com/michaellady/mike-skills/converge/internal/tmpl"
)

// Run dispatches one invocation. Returns the desired process exit code.
func Run(args []string) int {
	if len(args) == 0 {
		usage(os.Stderr)
		return 2
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "preflight":
		return runPreflight(rest)
	case "resolve-plan":
		return runResolvePlan(rest)
	case "detect-base-branch":
		return runDetectBase(rest)
	case "get-diff":
		return runGetDiff(rest)
	case "smoke-check":
		return runSmoke(rest)
	case "log":
		return runLog(rest)
	case "cleanup":
		return runCleanup(rest)
	case "render-prompt":
		return runRender(rest)
	case "validate-critique":
		return runValidate(rest)
	case "status":
		return runStatus(rest)
	case "codex-critique":
		return runLLM(rest, "codex")
	case "claude-critique":
		return runLLM(rest, "claude")
	case "llm-critique":
		return runLLM(rest, "")
	case "list-providers":
		for _, n := range dispatch.Names() {
			fmt.Println(n)
		}
		return 0
	case "list-modes":
		for _, m := range embedded.ListEmbeddedTemplates() {
			fmt.Println(m)
		}
		return 0
	case "-h", "--help", "help":
		usage(os.Stdout)
		return 0
	}
	fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", cmd)
	usage(os.Stderr)
	return 2
}

func usage(w io.Writer) {
	fmt.Fprint(w, `usage: converge <subcommand> [args]

Pre-run check / setup
  preflight <mode>                       Verify codex, auth, git, deps

Plan / branch / diff resolution
  resolve-plan [path]                    Find plan file
  detect-base-branch [pr#]               Detect git base branch
  get-diff <base> [pr#]                  Print truncated base...HEAD or PR diff

Smoke check / cleanup
  smoke-check {build|test}               Run project-appropriate smoke check
  cleanup                                Remove /tmp/converge-* per-round files

Log / status writers
  log {init|row|smoke|note} <file> ...   CONVERGE LOG / REVIEW.md writer
  status {start|round|thread|verdict|end|path|show} <session-id> [...]

Prompt + schema
  render-prompt <mode-or-path> KEY=… ...
                                         Render embedded mode template ("plan",
                                         "implement", "verify", "review") or
                                         a literal file path. Each KEY is a
                                         literal value or "@/path/to/file".
                                         IF_RESUME blocks toggle on RESUME=1.
  validate-critique <json>               Schema-validate critique payload
                                         (set CONVERGE_REQUIRE_EVIDENCE=1 for
                                         implement/verify/review)

LLM transport (codex or claude)
  llm-critique --provider {codex|claude} [--resume <id>] [--model <m>]
               <prompt-file> [effort]    Run the chosen LLM, stream events to
                                         stderr, write final message to stdout.
                                         Captures session/thread id on round 1.
  codex-critique [--resume <thread-id>] [--model <m>] <prompt-file> [effort]
                                         Alias for llm-critique --provider codex.
                                         Backward-compatible with pre-refactor
                                         callers.
  claude-critique [--resume <session-id>] [--model <m>] <prompt-file> [effort]
                                         Alias for llm-critique --provider claude.

Inspection
  list-modes                             List embedded prompt template modes
  list-providers                         List available LLM providers
  help                                   This message

Env vars: CONVERGE_CODEX_TIMEOUT, CONVERGE_CLAUDE_TIMEOUT, CONVERGE_CLAUDE_MODEL,
CONVERGE_QUIET, CONVERGE_HEARTBEAT_S, CONVERGE_THREAD_OUT, CONVERGE_DIFF_MAX_BYTES,
CONVERGE_REQUIRE_EVIDENCE, CONVERGE_SCHEMA, CONVERGE_PROMPTS_DIR,
CONVERGE_STATUS_DIR, CONVERGE_ACTIVE_PLAN, CONVERGE_SMOKE_BUILD,
CONVERGE_SMOKE_TEST, CLAUDE_PLANS_DIR, CODEX_HOME.
`)
}

// ----- subcommand handlers -----

func runPreflight(args []string) int {
	mode := ""
	if len(args) > 0 {
		mode = args[0]
	}
	if err := preflight.Run(mode, os.Stdout); err != nil {
		return 1
	}
	return 0
}

func runResolvePlan(args []string) int {
	explicit := ""
	if len(args) > 0 {
		explicit = args[0]
	}
	p, err := plan.Resolve(explicit)
	if err != nil {
		fmt.Fprintln(os.Stderr, "resolve-plan:", err)
		return 1
	}
	fmt.Println(p)
	return 0
}

func runDetectBase(args []string) int {
	pr := ""
	if len(args) > 0 {
		pr = args[0]
	}
	b, err := gitops.DetectBaseBranch(pr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "detect-base-branch:", err)
		return 1
	}
	fmt.Println(b)
	return 0
}

func runGetDiff(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: get-diff <base> [pr#]")
		return 2
	}
	base := args[0]
	pr := ""
	if len(args) > 1 {
		pr = args[1]
	}
	d, err := gitops.GetDiff(base, pr, 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, "get-diff:", err)
		return 1
	}
	fmt.Print(d)
	return 0
}

func runSmoke(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: smoke-check build|test")
		return 2
	}
	mode := smoke.Mode(args[0])
	if mode != smoke.Build && mode != smoke.Test {
		fmt.Fprintln(os.Stderr, "smoke-check: mode must be build|test")
		return 2
	}
	if err := smoke.Run(mode, os.Stdout, os.Stderr); err != nil {
		// PASS/FAIL line is already on stdout. Map to exit codes.
		if err.Error() == "smoke check failed" {
			return 1
		}
		fmt.Fprintln(os.Stderr, "smoke-check:", err)
		return 2
	}
	return 0
}

func runLog(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: log init|row|smoke|note <file> ...")
		return 2
	}
	sub, file, rest := args[0], args[1], args[2:]
	switch sub {
	case "init":
		if err := logwriter.Init(file); err != nil {
			fmt.Fprintln(os.Stderr, "log init:", err)
			return 1
		}
	case "row":
		if len(rest) < 4 {
			fmt.Fprintln(os.Stderr, "usage: log row <file> <round> <author> <verdict> <issues> <conceded>")
			return 2
		}
		round, err := strconv.Atoi(rest[0])
		if err != nil {
			fmt.Fprintln(os.Stderr, "log row: round must be int")
			return 2
		}
		issues, conceded := "", ""
		if len(rest) > 3 {
			issues = rest[3]
		}
		if len(rest) > 4 {
			conceded = rest[4]
		}
		if err := logwriter.Row(file, round, rest[1], rest[2], issues, conceded); err != nil {
			fmt.Fprintln(os.Stderr, "log row:", err)
			return 1
		}
	case "smoke":
		if len(rest) < 1 {
			fmt.Fprintln(os.Stderr, "usage: log smoke <file> <result>")
			return 2
		}
		if err := logwriter.Smoke(file, rest[0]); err != nil {
			fmt.Fprintln(os.Stderr, "log smoke:", err)
			return 1
		}
	case "note":
		if len(rest) < 1 {
			fmt.Fprintln(os.Stderr, "usage: log note <file> <text>")
			return 2
		}
		if err := logwriter.Note(file, joinSpace(rest)); err != nil {
			fmt.Fprintln(os.Stderr, "log note:", err)
			return 1
		}
	default:
		fmt.Fprintln(os.Stderr, "log: unknown subcommand", sub)
		return 2
	}
	return 0
}

func runCleanup(args []string) int {
	n, err := cleanup.Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "cleanup:", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "cleanup: removed %d file(s)\n", n)
	return 0
}

func runRender(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: render-prompt <mode-or-path> KEY=val ...")
		return 2
	}
	target := args[0]
	var text []byte
	if _, err := os.Stat(target); err == nil {
		text, err = os.ReadFile(target)
		if err != nil {
			fmt.Fprintln(os.Stderr, "render-prompt:", err)
			return 1
		}
	} else {
		b, err := embedded.TemplateBytes(target)
		if err != nil {
			fmt.Fprintln(os.Stderr, "render-prompt:", err)
			return 2
		}
		text = b
	}
	vals, err := tmpl.Parse(args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "render-prompt:", err)
		return 2
	}
	out, missing := tmpl.Render(string(text), vals)
	if len(missing) > 0 {
		fmt.Fprintln(os.Stderr, "render-prompt: warning: unfilled placeholders:", missing)
	}
	fmt.Print(out)
	return 0
}

func runValidate(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: validate-critique <json>")
		return 2
	}
	payload, err := os.ReadFile(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "validate-critique:", err)
		return 1
	}
	sch, err := embedded.SchemaBytes()
	if err != nil {
		fmt.Fprintln(os.Stderr, "validate-critique:", err)
		return 1
	}
	req := os.Getenv("CONVERGE_REQUIRE_EVIDENCE") == "1"
	errs, err := schema.Validate(payload, sch, req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "validate-critique:", err)
		return 1
	}
	if len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintln(os.Stderr, e)
		}
		return 1
	}
	return 0
}

func runStatus(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: status start|round|thread|verdict|end|path|show <session-id> [...]")
		return 2
	}
	sub, sid, rest := args[0], args[1], args[2:]
	switch sub {
	case "start":
		if len(rest) < 2 {
			fmt.Fprintln(os.Stderr, "usage: status start <sid> <mode> <max-rounds>")
			return 2
		}
		max, _ := strconv.Atoi(rest[1])
		if err := status.Start(sid, rest[0], max); err != nil {
			fmt.Fprintln(os.Stderr, "status:", err)
			return 1
		}
	case "round":
		if len(rest) < 2 {
			fmt.Fprintln(os.Stderr, "usage: status round <sid> <round> <phase>")
			return 2
		}
		r, _ := strconv.Atoi(rest[0])
		if err := status.Round(sid, r, rest[1]); err != nil {
			fmt.Fprintln(os.Stderr, "status:", err)
			return 1
		}
	case "thread":
		if len(rest) < 1 {
			fmt.Fprintln(os.Stderr, "usage: status thread <sid> <thread-id>")
			return 2
		}
		if err := status.Thread(sid, rest[0]); err != nil {
			fmt.Fprintln(os.Stderr, "status:", err)
			return 1
		}
	case "verdict":
		if len(rest) < 3 {
			fmt.Fprintln(os.Stderr, "usage: status verdict <sid> <author> <verdict> <issues>")
			return 2
		}
		issues, _ := strconv.Atoi(rest[2])
		if err := status.AddVerdict(sid, rest[0], rest[1], issues); err != nil {
			fmt.Fprintln(os.Stderr, "status:", err)
			return 1
		}
	case "end":
		if len(rest) < 1 {
			fmt.Fprintln(os.Stderr, "usage: status end <sid> <outcome>")
			return 2
		}
		if err := status.End(sid, rest[0]); err != nil {
			fmt.Fprintln(os.Stderr, "status:", err)
			return 1
		}
	case "path":
		fmt.Println(status.Path(sid))
	case "show":
		b, err := os.ReadFile(status.Path(sid))
		if err != nil {
			fmt.Fprintln(os.Stderr, "status: no snapshot for", sid)
			return 1
		}
		os.Stdout.Write(b)
	default:
		fmt.Fprintln(os.Stderr, "status: unknown subcommand", sub)
		return 2
	}
	return 0
}

// runLLM dispatches a critique call to the chosen provider. providerHint is
// the provider name implied by the subcommand alias (codex-critique →
// "codex", claude-critique → "claude", llm-critique → "" meaning require
// --provider).
func runLLM(args []string, providerHint string) int {
	subcmd := providerHint + "-critique"
	if providerHint == "" {
		subcmd = "llm-critique"
	}
	opts := provider.Options{}
	providerName := providerHint

	for len(args) > 0 {
		switch args[0] {
		case "--provider":
			if len(args) < 2 {
				fmt.Fprintf(os.Stderr, "usage: %s --provider {codex|claude} [--resume <id>] [--model <m>] <prompt-file> [effort]\n", subcmd)
				return 2
			}
			providerName = args[1]
			args = args[2:]
		case "--resume":
			if len(args) < 2 {
				fmt.Fprintf(os.Stderr, "usage: %s [--resume <id>] [--model <m>] <prompt-file> [effort]\n", subcmd)
				return 2
			}
			opts.ResumeID = args[1]
			args = args[2:]
		case "--model":
			if len(args) < 2 {
				fmt.Fprintf(os.Stderr, "usage: %s [--resume <id>] [--model <m>] <prompt-file> [effort]\n", subcmd)
				return 2
			}
			opts.Model = args[1]
			args = args[2:]
		default:
			// First non-flag arg ends the option block.
			goto positional
		}
	}
positional:
	if providerName == "" {
		fmt.Fprintln(os.Stderr, "llm-critique: --provider is required (codex|claude); or use codex-critique / claude-critique")
		return 2
	}
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "usage: %s [--resume <id>] [--model <m>] <prompt-file> [effort]\n", subcmd)
		return 2
	}
	opts.PromptFile = args[0]
	if len(args) > 1 {
		opts.Effort = args[1]
	}

	prov, err := dispatch.Get(providerName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", subcmd, err)
		return 2
	}
	err = prov.Run(context.Background(), opts)
	if err == nil {
		return 0
	}
	var pe *provider.Error
	if errors.As(err, &pe) {
		fmt.Fprintf(os.Stderr, "%s: %v\n", subcmd, pe.Err)
		return pe.Code
	}
	fmt.Fprintf(os.Stderr, "%s: %v\n", subcmd, err)
	return 1
}

func joinSpace(xs []string) string {
	out := ""
	for i, s := range xs {
		if i > 0 {
			out += " "
		}
		out += s
	}
	return out
}

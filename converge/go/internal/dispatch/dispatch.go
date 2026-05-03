// Package dispatch maps a provider name to a concrete provider.Provider
// implementation. Lives outside internal/provider/ to avoid an import
// cycle (the implementations import the provider interface).
package dispatch

import (
	"fmt"

	"github.com/michaellady/mike-skills/llm-provider/agent"
	"github.com/michaellady/mike-skills/llm-provider/claude"
	"github.com/michaellady/mike-skills/llm-provider/codex"
	"github.com/michaellady/mike-skills/llm-provider/gemini"
	"github.com/michaellady/mike-skills/llm-provider/provider"
)

// Get returns the provider for the given name. An empty name resolves to
// "codex" for backward compatibility with pre-refactor callers.
func Get(name string) (provider.Provider, error) {
	switch name {
	case "", "codex":
		return codex.New(), nil
	case "claude":
		return claude.New(), nil
	case "agent":
		return agent.New(), nil
	case "gemini":
		return gemini.New(), nil
	default:
		return nil, fmt.Errorf("unknown provider %q (supported: codex, claude, agent, gemini)", name)
	}
}

// Names lists the registered provider names.
func Names() []string {
	return []string{"codex", "claude", "agent", "gemini"}
}

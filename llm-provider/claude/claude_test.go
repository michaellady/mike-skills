package claude

import (
	"io"
	"strings"
	"testing"

	"github.com/michaellady/mike-skills/llm-provider/provider"
)

func quietOpts() provider.Options {
	return provider.Options{Quiet: true, Stderr: io.Discard, HeartbeatS: 5}
}

// A complete stream: init → assistant → result. Final comes from the result
// event; the assistant text is retained as the partial.
func TestStreamFilter_CompleteStream(t *testing.T) {
	in := strings.Join([]string{
		`{"type":"system","subtype":"init","session_id":"abcd1234-0000-0000-0000-000000000000"}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"{\"verdict\":\"pass\"}"}]}}`,
		`{"type":"result","result":"{\"verdict\":\"pass\",\"findings\":[]}"}`,
	}, "\n")
	final, partial, sessionID, authErr := streamFilter(strings.NewReader(in), quietOpts())
	if authErr {
		t.Fatal("unexpected authErr")
	}
	if final != `{"verdict":"pass","findings":[]}` {
		t.Fatalf("final = %q", final)
	}
	if partial != `{"verdict":"pass"}` {
		t.Fatalf("partial = %q", partial)
	}
	if sessionID != "abcd1234-0000-0000-0000-000000000000" {
		t.Fatalf("sessionID = %q", sessionID)
	}
}

// The defect-2 shape: the stream is cut (timeout kill) before any result
// event. The last assistant text must survive as the partial so the caller
// can deliver/persist it instead of losing everything.
func TestStreamFilter_TruncatedStreamKeepsPartial(t *testing.T) {
	in := strings.Join([]string{
		`{"type":"system","subtype":"init","session_id":"abcd1234-0000-0000-0000-000000000000"}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"working on it"}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"the near-final answer"}]}}`,
	}, "\n")
	final, partial, _, authErr := streamFilter(strings.NewReader(in), quietOpts())
	if authErr {
		t.Fatal("unexpected authErr")
	}
	if final != "" {
		t.Fatalf("final should be empty without a result event, got %q", final)
	}
	if partial != "the near-final answer" {
		t.Fatalf("partial = %q, want the LAST assistant text", partial)
	}
}

// Multi-block assistant content is joined, not truncated to the first block.
func TestAssistantText_JoinsBlocks(t *testing.T) {
	var ev event
	ev.Message.Content = []byte(`[{"type":"text","text":"part one"},{"type":"tool_use"},{"type":"text","text":"part two"}]`)
	if got := assistantText(ev); got != "part one\npart two" {
		t.Fatalf("assistantText = %q", got)
	}
}

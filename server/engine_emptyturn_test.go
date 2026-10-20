package server

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/Autumn-27/norma/llm"
)

// Recognition and continuation of an empty turn (thinking only, with no text or tools); see steerHooks.Stop.

func assistantThinking(text string) llm.Message {
	return llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{
		{Type: llm.BlockThinking, Thinking: text, Signature: "sig"},
	}}
}

func TestIsThinkingOnlyTurn(t *testing.T) {
	toolUse := llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{
		{Type: llm.BlockThinking, Thinking: "先扫端口"},
		{Type: llm.BlockToolUse, ID: "t1", Name: "run_nuclei"},
	}}
	cases := []struct {
		name string
		msgs []llm.Message
		want bool
	}{
		{"thinking only", []llm.Message{llm.UserText("开始"), assistantThinking("想想")}, true},
		{"thinking and tool", []llm.Message{llm.UserText("开始"), toolUse}, false},
		{"thinking and text", []llm.Message{assistantThinking("想想"), {
			Role:    llm.RoleAssistant,
			Content: []llm.ContentBlock{{Type: llm.BlockThinking, Thinking: "x"}, llm.TextBlock("结论")},
		}}, false},
		{"whitespace-only text", []llm.Message{{
			Role:    llm.RoleAssistant,
			Content: []llm.ContentBlock{{Type: llm.BlockThinking, Thinking: "x"}, llm.TextBlock("  \n ")},
		}}, true},
		{"completely empty assistant turn", []llm.Message{{Role: llm.RoleAssistant}}, true},
		// A tool result has the user role; the check must look back to the preceding assistant turn.
		{"last message is a tool result", []llm.Message{toolUse, {
			Role:    llm.RoleUser,
			Content: []llm.ContentBlock{{Type: llm.BlockToolResult, ToolUseID: "t1"}},
		}}, false},
		{"no assistant message", []llm.Message{llm.UserText("开始")}, false},
		{"empty history", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isThinkingOnlyTurn(c.msgs); got != c.want {
				t.Fatalf("isThinkingOnlyTurn = %v, want %v", got, c.want)
			}
		})
	}
}

// fakeHooks is a programmable inner HookRunner used to verify that steerHooks respects its decisions.
type fakeHooks struct {
	prevent  bool
	blocking []string
	msg      string
}

func (f fakeHooks) PreToolUse(context.Context, string, []byte) (bool, string, []byte) {
	return false, "", nil
}
func (f fakeHooks) PostToolUse(context.Context, string, []byte, []byte, bool) {}
func (f fakeHooks) Stop(context.Context, []llm.Message) (bool, []string, string) {
	return f.prevent, f.blocking, f.msg
}

func TestSteerHooksStopNudgesEmptyTurn(t *testing.T) {
	empty := []llm.Message{assistantThinking("我应该先枚举子域名")}

	t.Run("empty turn injects a continuation instruction", func(t *testing.T) {
		h := steerHooks{nudges: &atomic.Int64{}, limit: defaultEmptyTurnNudges, label: "worker-1 · #1"}
		prevent, blocking, _ := h.Stop(context.Background(), empty)
		if prevent {
			t.Fatal("an empty turn should not cause a hard stop")
		}
		if len(blocking) != 1 || blocking[0] != emptyTurnNudge {
			t.Fatalf("blocking = %v, want [emptyTurnNudge]", blocking)
		}
	})

	t.Run("text or tool output does not trigger an intervention", func(t *testing.T) {
		h := steerHooks{nudges: &atomic.Int64{}, limit: defaultEmptyTurnNudges}
		normal := []llm.Message{{
			Role:    llm.RoleAssistant,
			Content: []llm.ContentBlock{llm.TextBlock("已完成扫描，未发现开放端口")},
		}}
		if _, blocking, _ := h.Stop(context.Background(), normal); blocking != nil {
			t.Fatalf("normal completion was mistaken for an empty turn: %v", blocking)
		}
		if n := h.nudges.Load(); n != 0 {
			t.Fatalf("should not count a turn without intervention, got %d", n)
		}
	})

	t.Run("allow completion after reaching the limit", func(t *testing.T) {
		const limit = 5 // The user configured five empty response retries.
		h := steerHooks{nudges: &atomic.Int64{}, limit: limit}
		for i := 1; i <= limit; i++ {
			if _, blocking, _ := h.Stop(context.Background(), empty); len(blocking) != 1 {
				t.Fatalf("attempt %d should still be within the limit, blocking = %v", i, blocking)
			}
		}
		if _, blocking, _ := h.Stop(context.Background(), empty); blocking != nil {
			t.Fatalf("still injecting after exceeding the limit: %v", blocking)
		}
	})

	// Setting empty response retries to -1 disables this layer; emptyTurnNudgeLimit resolves to 0.
	t.Run("disabled configuration does not intervene", func(t *testing.T) {
		h := steerHooks{nudges: &atomic.Int64{}, limit: 0}
		if _, blocking, _ := h.Stop(context.Background(), empty); blocking != nil {
			t.Fatalf("still injecting when disabled: %v", blocking)
		}
	})

	t.Run("do not override an inner hard stop", func(t *testing.T) {
		h := steerHooks{inner: fakeHooks{prevent: true, msg: "guard 拒绝收场"}, nudges: &atomic.Int64{}, limit: defaultEmptyTurnNudges}
		prevent, blocking, msg := h.Stop(context.Background(), empty)
		if !prevent || msg != "guard 拒绝收场" || blocking != nil {
			t.Fatalf("inner hard stop was overwritten: prevent=%v blocking=%v msg=%q", prevent, blocking, msg)
		}
		if n := h.nudges.Load(); n != 0 {
			t.Fatalf("deferring to inner should not consume the limit, got %d", n)
		}
	})

	t.Run("do not add to an inner continuation", func(t *testing.T) {
		h := steerHooks{inner: fakeHooks{blocking: []string{"guard 的续跑理由"}}, nudges: &atomic.Int64{}, limit: defaultEmptyTurnNudges}
		_, blocking, _ := h.Stop(context.Background(), empty)
		if len(blocking) != 1 || blocking[0] != "guard 的续跑理由" {
			t.Fatalf("inner continuation message was overwritten: %v", blocking)
		}
	})

	t.Run("behavior is unchanged without a counter", func(t *testing.T) {
		h := steerHooks{limit: defaultEmptyTurnNudges} // A future caller may omit nudges.
		if _, blocking, _ := h.Stop(context.Background(), empty); blocking != nil {
			t.Fatalf("should not inject without a counter: %v", blocking)
		}
	})
}

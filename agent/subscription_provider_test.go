package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Autumn-27/norma/llm"
)

func TestChatGPTToolRoundTripAndWireContract(t *testing.T) {
	stream := `data: {"type":"response.output_item.done","item":{"type":"function_call","namespace":"artex","call_id":"call_1","name":"inspect","arguments":"{\"target\":\"local\"}"}}

data: {"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":10,"output_tokens":5}}}

`
	a := llm.NewAccumulator()
	if err := readChatGPTStream(strings.NewReader(stream), func(e llm.StreamEvent, err error) bool { a.Add(e); return true }); err != nil {
		t.Fatal(err)
	}
	msg := a.Message()
	if len(msg.ToolUses()) != 1 || a.StopReason != "tool_use" || a.Usage.OutputTokens != 5 {
		t.Fatal("tool stream was not assembled")
	}
	req := llm.CompletionRequest{System: []string{"test"}, Messages: []llm.Message{llm.UserText("inspect local"), msg, {Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultText("call_1", "done", false)}}}, Tools: []llm.ToolSchema{{Name: "inspect", InputSchema: map[string]any{"type": "object"}}}, MaxTokens: 100}
	body, err := chatGPTBody("account-model", "high", req)
	if err != nil {
		t.Fatal(err)
	}
	var b map[string]any
	json.Unmarshal(body, &b)
	if b["stream"] != true || b["store"] != false || b["max_output_tokens"] != nil || b["temperature"] != nil {
		t.Fatalf("unsupported request: %s", body)
	}
	items := b["input"].([]any)
	call := items[1].(map[string]any)
	result := items[2].(map[string]any)
	if call["namespace"] != "artex" || call["call_id"] != result["call_id"] || result["output"] != "done" {
		t.Fatal("tool continuity lost")
	}
	ns := b["tools"].([]any)[0].(map[string]any)
	if ns["type"] != "namespace" {
		t.Fatal("tools are not namespaced")
	}
}

func TestChatGPTStreamRejectsIncompleteResults(t *testing.T) {
	for _, input := range []string{"", "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n", "data: {\"type\":\"response.failed\"}\n\n", "data: {\"type\":\"response.incomplete\"}\n\n", "data: not-json\n\n", "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"failed\"}}\n\n"} {
		if err := readChatGPTStream(strings.NewReader(input), func(llm.StreamEvent, error) bool { return true }); err == nil {
			t.Errorf("accepted incomplete stream %q", input)
		}
	}
}

func TestClaudeToolRoundTripAndValidation(t *testing.T) {
	tools := []llm.ToolSchema{{Name: "inspect", InputSchema: map[string]any{"type": "object"}}}
	raw := `{"subtype":"success","structured_output":{"text":"Checking.","tool_calls":[{"name":"inspect","arguments":"{\"target\":\"local\"}"}]},"usage":{"input_tokens":2,"output_tokens":3}}`
	msg, u, err := decodeClaude([]byte(raw), tools)
	if err != nil {
		t.Fatal(err)
	}
	if u.OutputTokens != 3 || len(msg.ToolUses()) != 1 || msg.ToolUses()[0].Name != "inspect" {
		t.Fatal("Claude tool output lost")
	}
	req := llm.CompletionRequest{Messages: []llm.Message{msg, {Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultText(msg.ToolUses()[0].ID, "result", false)}}}, Tools: tools}
	b, _ := json.Marshal(req)
	if !strings.Contains(string(b), msg.ToolUses()[0].ID) || !strings.Contains(string(b), "tool_result") {
		t.Fatal("tool results not replayable")
	}
	for _, bad := range []string{`{"subtype":"success"}`, strings.Replace(raw, "inspect", "unknown", 1), strings.Replace(raw, `{\"target\":\"local\"}`, "null", 1), strings.Replace(raw, `"success"`, `"error_max_turns"`, 1), `{"subtype":"success","is_error":true,"structured_output":{"text":"bad","tool_calls":[]}}`} {
		if _, _, err := decodeClaude([]byte(bad), tools); err == nil {
			t.Errorf("accepted invalid Claude result %s", bad)
		}
	}
}

func TestSubscriptionProfilesIgnoreAPISecretsAndEndpoints(t *testing.T) {
	for _, format := range []string{"chatgpt", "claude-code"} {
		c := ConfigFrom(format, "model", "https://example.invalid", "secret", "http://example.invalid")
		if c.Provider() != format || c.APIKey != "" || c.BaseURL != "" || c.Proxy != "" {
			t.Fatal("subscription accepted API credentials")
		}
		if _, err := c.NewProvider(); err == nil {
			t.Fatal("provider accepted missing connection manager")
		}
	}
}

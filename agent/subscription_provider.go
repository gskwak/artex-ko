package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"strings"
	"time"

	"github.com/Autumn-27/artex/subscription"
	"github.com/Autumn-27/norma/llm"
	"github.com/google/uuid"
)

func IsSubscription(format string) bool { return format == "chatgpt" || format == "claude-code" }

type subscriptionProvider struct {
	cfg     Config
	manager *subscription.Manager
	client  *http.Client
}

func (p *subscriptionProvider) Complete(ctx context.Context, req llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
	a := llm.NewAccumulator()
	for ev, err := range p.Stream(ctx, req) {
		if err != nil {
			return llm.Message{}, "", llm.Usage{}, err
		}
		a.Add(ev)
	}
	return a.Message(), a.StopReason, a.Usage, nil
}
func (p *subscriptionProvider) Stream(ctx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	return func(yield func(llm.StreamEvent, error) bool) {
		if p.cfg.Provider() == "claude-code" {
			p.claude(ctx, req, yield)
			return
		}
		p.chatgpt(ctx, req, yield)
	}
}

// Build only the parameters accepted by ChatGPT plan inference. Full history
// is supplied each turn; ARTEX retains ownership of tools and orchestration.
func chatGPTBody(model, effort string, req llm.CompletionRequest) ([]byte, error) {
	input := []any{}
	for _, m := range req.Messages {
		for _, b := range m.Content {
			switch b.Type {
			case llm.BlockText:
				input = append(input, map[string]any{"role": m.Role, "content": b.Text})
			case llm.BlockToolUse:
				if !json.Valid(b.Input) {
					return nil, errors.New("invalid tool arguments in conversation history")
				}
				input = append(input, map[string]any{"type": "function_call", "call_id": b.ID, "name": b.Name, "namespace": "artex", "arguments": string(b.Input)})
			case llm.BlockToolResult:
				var text strings.Builder
				for _, c := range b.Content {
					if c.Type == llm.BlockText {
						text.WriteString(c.Text)
					}
				}
				input = append(input, map[string]any{"type": "function_call_output", "call_id": b.ToolUseID, "output": text.String()})
			}
		}
	}
	body := map[string]any{"model": model, "instructions": strings.Join(req.System, "\n\n"), "input": input, "stream": true, "store": false}
	if effort != "" && req.Thinking != "disabled" {
		body["reasoning"] = map[string]string{"effort": effort}
	}
	if len(req.Tools) > 0 {
		tools := []any{}
		for _, t := range req.Tools {
			tools = append(tools, map[string]any{"type": "function", "name": t.Name, "description": t.Description, "parameters": t.InputSchema, "strict": false})
		}
		body["tools"] = []any{map[string]any{"type": "namespace", "name": "artex", "description": "Tools executed by the ARTEX host", "tools": tools}}
		body["parallel_tool_calls"] = false
	}
	return json.Marshal(body)
}
func (p *subscriptionProvider) chatgpt(ctx context.Context, req llm.CompletionRequest, yield func(llm.StreamEvent, error) bool) {
	fail := func(err error) { yield(llm.StreamEvent{}, err) }
	token, err := p.manager.AccessToken(ctx)
	if err != nil {
		fail(err)
		return
	}
	body, err := chatGPTBody(p.cfg.Model, p.cfg.ReasoningEffort, req)
	if err != nil {
		fail(err)
		return
	}
	r, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/responses", bytes.NewReader(body))
	if err != nil {
		fail(err)
		return
	}
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "text/event-stream")
	res, err := p.client.Do(r)
	if err != nil {
		fail(errors.New("ChatGPT inference request failed; check connectivity"))
		return
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		var failure struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&failure)
		if failure.Error.Code == "subscription_sharing_usage_limit_exceeded" {
			fail(errors.New("ChatGPT subscription_sharing_usage_limit_exceeded; wait for your plan limit to reset"))
			return
		}
		fail(fmt.Errorf("ChatGPT inference HTTP %d; check account access, model availability and plan limits", res.StatusCode))
		return
	}
	if err := readChatGPTStream(res.Body, yield); err != nil {
		fail(err)
	}
}

func readChatGPTStream(body io.Reader, yield func(llm.StreamEvent, error) bool) error {
	scan := bufio.NewScanner(body)
	scan.Buffer(make([]byte, 64*1024), 4<<20)
	var data []string
	hasTool, completed := false, false
	emit := func() (bool, error) {
		if len(data) == 0 {
			return true, nil
		}
		raw := strings.Join(data, "\n")
		data = nil
		if raw == "[DONE]" {
			return true, nil
		}
		var f struct {
			Type  string `json:"type"`
			Delta string `json:"delta"`
			Item  struct {
				Type, Name, Namespace string
				CallID                string `json:"call_id"`
				Arguments             string
			} `json:"item"`
			Response struct {
				Status string
				Error  *struct{ Code string } `json:"error"`
				Usage  struct {
					Input   int `json:"input_tokens"`
					Output  int `json:"output_tokens"`
					Details struct {
						Cached int `json:"cached_tokens"`
					} `json:"input_tokens_details"`
				}
			} `json:"response"`
		}
		if err := json.Unmarshal([]byte(raw), &f); err != nil {
			return false, errors.New("invalid ChatGPT stream event")
		}
		switch f.Type {
		case "response.output_text.delta":
			return yield(llm.StreamEvent{Type: llm.SETextDelta, Text: f.Delta}, nil), nil
		case "response.reasoning_summary_text.delta":
			return yield(llm.StreamEvent{Type: llm.SEThinkingDelta, Text: f.Delta}, nil), nil
		case "response.output_item.done":
			// Emit complete tool arguments together, even if the wire interleaves calls.
			if f.Item.Type == "function_call" {
				if f.Item.Namespace != "" && f.Item.Namespace != "artex" {
					return false, errors.New("unexpected ChatGPT tool namespace")
				}
				if f.Item.CallID == "" || f.Item.Name == "" || !json.Valid([]byte(f.Item.Arguments)) {
					return false, errors.New("invalid ChatGPT tool call")
				}
				hasTool = true
				if !yield(llm.StreamEvent{Type: llm.SEToolUseStart, ToolID: f.Item.CallID, ToolName: f.Item.Name}, nil) {
					return false, nil
				}
				return yield(llm.StreamEvent{Type: llm.SEToolInputJSON, Text: f.Item.Arguments}, nil), nil
			}
		case "response.failed", "response.incomplete", "error":
			if f.Response.Error != nil && f.Response.Error.Code == "subscription_sharing_usage_limit_exceeded" {
				return false, errors.New("ChatGPT subscription_sharing_usage_limit_exceeded; wait for your plan limit to reset")
			}
			return false, errors.New("ChatGPT response failed or was incomplete; check plan limits and retry")
		case "response.completed":
			if f.Response.Status != "completed" {
				return false, errors.New("ChatGPT response did not complete")
			}
			completed = true
			stop := "end_turn"
			if hasTool {
				stop = "tool_use"
			}
			if !yield(llm.StreamEvent{Type: llm.SEMessageDelta, StopReason: stop, Usage: llm.Usage{InputTokens: f.Response.Usage.Input, OutputTokens: f.Response.Usage.Output, CacheReadTokens: f.Response.Usage.Details.Cached}}, nil) {
				return false, nil
			}
			yield(llm.StreamEvent{Type: llm.SEMessageStop}, nil)
			return false, nil
		}
		return true, nil
	}
	for scan.Scan() {
		line := scan.Text()
		if line == "" {
			more, err := emit()
			if err != nil {
				return err
			}
			if !more {
				return nil
			}
		} else if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := scan.Err(); err != nil {
		return err
	}
	if len(data) > 0 {
		more, err := emit()
		if err != nil {
			return err
		}
		if !more {
			return nil
		}
	}
	if !completed {
		return errors.New("ChatGPT stream ended before response.completed")
	}
	return nil
}

// Claude Code performs inference only. Structured tool intents are returned to
// ARTEX's normal tool dispatcher, preserving its permissions and result history.
const claudeOutputSchema = `{"type":"object","properties":{"text":{"type":"string"},"tool_calls":{"type":"array","items":{"type":"object","properties":{"name":{"type":"string"},"arguments":{"type":"string"}},"required":["name","arguments"],"additionalProperties":false}}},"required":["text","tool_calls"],"additionalProperties":false}`
const claudeBridgePrompt = "Continue the ARTEX conversation supplied as JSON on stdin. Follow its System instructions and Messages. Return your assistant text and any tool calls using the required structured output. Each tool call's arguments must be a JSON-encoded object matching the named tool's InputSchema. Request only tools listed in Tools. Do not execute tools yourself. ARTEX will execute them and send the results in the next conversation request. Use an empty tool_calls array for a final answer."

type boundedBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (b *boundedBuffer) Bytes() []byte { return b.buffer.Bytes() }

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if b.buffer.Len()+len(p) > b.limit {
		return 0, errors.New("Claude Code output exceeded limit")
	}
	return b.buffer.Write(p)
}
func (p *subscriptionProvider) claude(ctx context.Context, req llm.CompletionRequest, yield func(llm.StreamEvent, error) bool) {
	fail := func(err error) { yield(llm.StreamEvent{}, err) }
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if !p.manager.ClaudeStatus(ctx).Connected {
		fail(errors.New("sign in to a Claude subscription with claude auth login in the ARTEX runtime"))
		return
	}
	args := []string{"--safe-mode", "-p", "--output-format", "json", "--json-schema", claudeOutputSchema, "--tools", "", "--disallowedTools", "mcp__*", "--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`, "--no-session-persistence", "--max-turns", "3", "--model", p.cfg.Model, "--system-prompt", claudeBridgePrompt}
	cmd, err := p.manager.ClaudeCommand(ctx, args...)
	if err != nil {
		fail(err)
		return
	}
	b, err := json.Marshal(req)
	if err != nil {
		fail(err)
		return
	}
	cmd.Stdin = bytes.NewReader(b)
	stdout := &boundedBuffer{limit: 8 << 20}
	stderr := &boundedBuffer{limit: 64 << 10}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			fail(ctx.Err())
		} else {
			fail(errors.New("Claude Code request failed; check subscription login, model and usage limits"))
		}
		return
	}
	msg, usage, err := decodeClaude(stdout.Bytes(), req.Tools)
	if err != nil {
		fail(err)
		return
	}
	if !yield(llm.StreamEvent{Type: llm.SEMessageStart}, nil) {
		return
	}
	stop := "end_turn"
	for _, b := range msg.Content {
		if b.Type == llm.BlockText {
			if !yield(llm.StreamEvent{Type: llm.SETextDelta, Text: b.Text}, nil) {
				return
			}
		} else {
			stop = "tool_use"
			if !yield(llm.StreamEvent{Type: llm.SEToolUseStart, ToolID: b.ID, ToolName: b.Name}, nil) {
				return
			}
			if !yield(llm.StreamEvent{Type: llm.SEToolInputJSON, Text: string(b.Input)}, nil) {
				return
			}
		}
	}
	if !yield(llm.StreamEvent{Type: llm.SEMessageDelta, StopReason: stop, Usage: usage}, nil) {
		return
	}
	yield(llm.StreamEvent{Type: llm.SEMessageStop}, nil)
}
func decodeClaude(raw []byte, tools []llm.ToolSchema) (llm.Message, llm.Usage, error) {
	var out struct {
		IsError    bool   `json:"is_error"`
		Subtype    string `json:"subtype"`
		Structured *struct {
			Text  string `json:"text"`
			Calls []struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"tool_calls"`
		} `json:"structured_output"`
		Usage struct {
			Input  int `json:"input_tokens"`
			Output int `json:"output_tokens"`
			Read   int `json:"cache_read_input_tokens"`
			Write  int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	}
	msg := llm.Message{Role: llm.RoleAssistant}
	if json.Unmarshal(raw, &out) != nil || out.IsError || out.Subtype != "success" || out.Structured == nil {
		return msg, llm.Usage{}, errors.New("Claude Code did not return a successful structured response")
	}
	if out.Structured.Text != "" {
		msg.Content = append(msg.Content, llm.TextBlock(out.Structured.Text))
	}
	allowed := map[string]bool{}
	for _, t := range tools {
		allowed[t.Name] = true
	}
	for _, call := range out.Structured.Calls {
		var args map[string]any
		if !allowed[call.Name] || json.Unmarshal([]byte(call.Arguments), &args) != nil || args == nil {
			return msg, llm.Usage{}, errors.New("Claude Code returned an invalid or unknown tool call")
		}
		msg.Content = append(msg.Content, llm.ContentBlock{Type: llm.BlockToolUse, ID: "claude_" + uuid.NewString(), Name: call.Name, Input: json.RawMessage(call.Arguments)})
	}
	if len(msg.Content) == 0 {
		return msg, llm.Usage{}, errors.New("Claude Code returned an empty response")
	}
	return msg, llm.Usage{InputTokens: out.Usage.Input, OutputTokens: out.Usage.Output, CacheReadTokens: out.Usage.Read, CacheWriteTokens: out.Usage.Write}, nil
}

package server

import (
	"context"
	"encoding/json"
	"iter"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/norma/llm"
)

type blueChatTestProvider struct {
	mu       sync.Mutex
	requests []llm.CompletionRequest
	toolCall bool
}

func (*blueChatTestProvider) Stream(context.Context, llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	return func(func(llm.StreamEvent, error) bool) {}
}

func (p *blueChatTestProvider) Complete(_ context.Context, req llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
	p.mu.Lock()
	p.requests = append(p.requests, req)
	toolCall := p.toolCall
	p.mu.Unlock()
	if toolCall {
		return llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{{Type: llm.BlockToolUse,
				ID: "blocked-tool", Name: "Bash", Input: json.RawMessage(`{"command":"echo unsafe"}`)}}},
			"tool_use", llm.Usage{}, nil
	}
	return llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.TextBlock("Defensive answer based on supplied context.")}},
		"end_turn", llm.Usage{InputTokens: 5, OutputTokens: 7}, nil
}

func (p *blueChatTestProvider) snapshot() []llm.CompletionRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]llm.CompletionRequest(nil), p.requests...)
}

func TestBlueChatRequiresJWT(t *testing.T) {
	s := &Server{m: &Manager{}, jwtKey: []byte(strings.Repeat("j", 32))}
	h := s.Handler()
	blueAPIRequest(t, h, "", http.MethodGet, "/api/blue/conversations", nil, http.StatusUnauthorized, nil)
	blueAPIRequest(t, h, "", http.MethodPost, "/api/blue/conversations", map[string]any{}, http.StatusUnauthorized, nil)
	blueAPIRequest(t, h, "", http.MethodPost, "/api/blue/conversations/1/messages", map[string]any{"message": "hi"}, http.StatusUnauthorized, nil)
}

func TestBlueChatConversationProfileOverridesAnalystBinding(t *testing.T) {
	pinned := &blueChatTestProvider{}
	profileID, boundID := int64(11), int64(12)
	s := &Server{provByProfile: map[int64]*provEntry{profileID: {prov: pinned, cfg: agent.Config{MaxTokens: 700}}}}
	prov, maxTokens, ok := s.blueChatProvider(&db.Conversation{LLMProfileID: &profileID}, &db.Agent{LLMProfileID: &boundID})
	if !ok || prov != pinned || maxTokens != 700 {
		t.Fatalf("conversation selection did not override analyst binding: provider=%T tokens=%d ready=%v", prov, maxTokens, ok)
	}
}

func TestBlueChatIsScopedAndToolFreePG(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ARTEX_BLUE_TEST_PG_DSN"))
	if dsn == "" {
		t.Skip("set ARTEX_BLUE_TEST_PG_DSN to a disposable PostgreSQL test database")
	}
	pg, err := db.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pg.Close() })
	actor, err := pg.GetAgentByKey(blueChatAgentKey)
	if err != nil || actor == nil || !actor.Enabled {
		t.Fatalf("blue analyst seed unavailable: actor=%+v err=%v", actor, err)
	}
	profile := actor.LLMProfileID
	if err := pg.SetAgentLLMProfile(blueChatAgentKey, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pg.SetAgentLLMProfile(blueChatAgentKey, profile) })
	red, err := pg.CreateConversation("mainagent", "red-chat-test", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pg.DeleteConversation(red.ID) })
	provider := &blueChatTestProvider{}
	s := &Server{m: &Manager{pg: pg}, ctx: context.Background(), jwtKey: []byte(strings.Repeat("c", 32)),
		llmOn: true, llmProv: provider}
	h := s.Handler()
	token, err := signJWT(s.signingKey(s.readPassHash()))
	if err != nil {
		t.Fatal(err)
	}
	var blue db.Conversation
	blueAPIRequest(t, h, token, http.MethodPost, "/api/blue/conversations", map[string]any{"title": "blue-chat-test"}, http.StatusOK, &blue)
	if blue.ID <= 0 || blue.AgentKey != blueChatAgentKey {
		t.Fatalf("unexpected blue conversation: %+v", blue)
	}
	t.Cleanup(func() { _ = pg.DeleteConversation(blue.ID) })
	blueAPIRequest(t, h, token, http.MethodGet, "/api/blue/conversations/"+i64s(red.ID), nil, http.StatusNotFound, nil)
	for _, tc := range []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodGet, "/api/conversations/" + i64s(blue.ID) + "/messages", nil},
		{http.MethodPost, "/api/conversations/" + i64s(blue.ID) + "/messages", map[string]any{"message": "unsafe"}},
		{http.MethodPatch, "/api/conversations/" + i64s(blue.ID), map[string]any{"title": "red rename"}},
		{http.MethodPatch, "/api/conversations/" + i64s(blue.ID) + "/profile", map[string]any{"llm_profile_id": nil}},
		{http.MethodPost, "/api/conversations/" + i64s(blue.ID) + "/stop", nil},
		{http.MethodDelete, "/api/conversations/" + i64s(blue.ID), nil},
		{http.MethodPost, "/api/conversations/delete/batch", map[string]any{"ids": []int64{red.ID, blue.ID}}},
	} {
		blueAPIRequest(t, h, token, tc.method, tc.path, tc.body, http.StatusNotFound, nil)
	}
	if stillThere, err := pg.GetConversation(blue.ID); err != nil || stillThere == nil {
		t.Fatalf("red API removed blue conversation: %+v, %v", stillThere, err)
	}
	var listed struct {
		Conversations []db.Conversation `json:"conversations"`
	}
	blueAPIRequest(t, h, token, http.MethodGet, "/api/blue/conversations", nil, http.StatusOK, &listed)
	for _, c := range listed.Conversations {
		if c.AgentKey != blueChatAgentKey {
			t.Fatalf("red conversation leaked into blue list: %+v", c)
		}
	}
	var redListed struct {
		Conversations []db.Conversation `json:"conversations"`
	}
	blueAPIRequest(t, h, token, http.MethodGet, "/api/conversations", nil, http.StatusOK, &redListed)
	for _, c := range redListed.Conversations {
		if c.ID == blue.ID {
			t.Fatal("blue conversation leaked into generic red list")
		}
	}
	path := "/api/blue/conversations/" + i64s(blue.ID)
	blueAPIRequest(t, h, token, http.MethodPost, path+"/messages", map[string]any{"message": "How should I triage this alert?"}, http.StatusAccepted, nil)
	waitBlueChatIdle(t, h, token, path)
	blueAPIRequest(t, h, token, http.MethodPost, path+"/messages", map[string]any{"message": "What evidence is still missing?"}, http.StatusAccepted, nil)
	waitBlueChatIdle(t, h, token, path)
	requests := provider.snapshot()
	if len(requests) != 2 {
		t.Fatalf("expected two model turns, got %d", len(requests))
	}
	for _, req := range requests {
		if len(req.Tools) != 0 || len(req.System) == 0 || !strings.Contains(req.System[0], "defensive security assistant") {
			t.Fatalf("blue chat lost its tool-free defensive contract: %+v", req)
		}
	}
	if len(requests[1].Messages) < 3 || requests[1].Messages[0].Text() != "How should I triage this alert?" ||
		requests[1].Messages[1].Text() != "Defensive answer based on supplied context." {
		t.Fatalf("second turn lost history: %+v", requests[1].Messages)
	}
	var messages struct {
		Items []ActivityDTO `json:"items"`
	}
	blueAPIRequest(t, h, token, http.MethodGet, path+"/messages", nil, http.StatusOK, &messages)
	if len(messages.Items) < 5 || messages.Items[0].Kind != "user" || messages.Items[1].Kind != "text" {
		t.Fatalf("blue chat activities not persisted: %+v", messages.Items)
	}
	var detail struct {
		Detail string `json:"detail"`
	}
	blueAPIRequest(t, h, token, http.MethodGet, path+"/messages/"+i64s(messages.Items[1].Seq), nil, http.StatusOK, &detail)
	if detail.Detail != "Defensive answer based on supplied context." {
		t.Fatalf("blue chat detail mismatch: %q", detail.Detail)
	}
	provider.mu.Lock()
	provider.toolCall = true
	provider.mu.Unlock()
	blueAPIRequest(t, h, token, http.MethodPost, path+"/messages", map[string]any{"message": "Try a shell command"}, http.StatusAccepted, nil)
	waitBlueChatIdle(t, h, token, path)
	blueAPIRequest(t, h, token, http.MethodGet, path+"/messages", nil, http.StatusOK, &messages)
	last := messages.Items[len(messages.Items)-1]
	if !last.IsError || last.Kind != "text" {
		t.Fatalf("tool call was not rejected: %+v", last)
	}
	for _, item := range messages.Items {
		if item.Kind == "tool_use" || item.Kind == "tool_result" {
			t.Fatalf("Blue Chat persisted an executable tool step: %+v", item)
		}
	}
}

func waitBlueChatIdle(t *testing.T, h http.Handler, token, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var status struct {
			Running bool `json:"running"`
		}
		blueAPIRequest(t, h, token, http.MethodGet, path+"/messages?since=0", nil, http.StatusOK, &status)
		if !status.Running {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("blue chat did not finish")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

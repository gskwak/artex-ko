package server

import (
	"context"
	"encoding/json"
	"fmt"
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
	caseItem, err := pg.CreateBlueCase(context.Background(), db.BlueCase{
		Title: "Investigate lab request", Description: "Review the linked HTTP event", Status: "open", Severity: "medium",
	})
	if err != nil {
		t.Fatal(err)
	}
	otherCase, err := pg.CreateBlueCase(context.Background(), db.BlueCase{
		Title: "Separate private case", Description: "Do not attach these records", Status: "open", Severity: "medium",
	})
	if err != nil {
		t.Fatal(err)
	}
	caseID, otherCaseID := caseItem.ID, otherCase.ID
	imported, err := pg.ImportBlueEvents(context.Background(), []db.BlueEvent{
		{CaseID: &caseID, Source: "blue-chat-test", EventType: "http", Severity: "medium",
			Message: "GET /lab/grounded-event", OccurredAt: time.Now().UTC(), Raw: json.RawMessage(`{"method":"GET"}`)},
		{CaseID: &otherCaseID, Source: "blue-chat-test", EventType: "http", Severity: "medium",
			Message: "PRIVATE-OTHER-CASE-EVENT", OccurredAt: time.Now().UTC(), Raw: json.RawMessage(`{"method":"POST"}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, event := range imported {
			_, _ = pg.Exec(`DELETE FROM blue_events WHERE id=$1`, event.ID)
		}
		_, _ = pg.Exec(`DELETE FROM blue_cases WHERE id IN ($1,$2)`, caseID, otherCaseID)
	})
	blueAPIRequest(t, h, token, http.MethodPost, path+"/messages", map[string]any{"message": "Bad case", "case_id": -1}, http.StatusBadRequest, nil)
	blueAPIRequest(t, h, token, http.MethodPost, path+"/messages", map[string]any{"message": "Missing case", "case_id": int64(9223372036854775807)}, http.StatusNotFound, nil)
	blueAPIRequest(t, h, token, http.MethodPost, path+"/messages", map[string]any{"message": "Review this case", "case_id": caseID}, http.StatusAccepted, nil)
	waitBlueChatIdle(t, h, token, path)
	grounded := provider.snapshot()
	if len(grounded) != 3 || len(grounded[2].Tools) != 0 {
		t.Fatalf("grounded chat must complete without tools: %+v", grounded)
	}
	attachedText := ""
	for _, message := range grounded[2].Messages {
		attachedText += message.Text()
	}
	if !strings.Contains(attachedText, "GET /lab/grounded-event") || !strings.Contains(attachedText, "Review this case") ||
		strings.Contains(attachedText, "PRIVATE-OTHER-CASE-EVENT") {
		t.Fatalf("case evidence was missing or crossed case boundaries: %q", attachedText)
	}
	blueAPIRequest(t, h, token, http.MethodGet, path+"/messages", nil, http.StatusOK, &messages)
	var attachedMetadata struct {
		BlueCase struct {
			ID       int64   `json:"id"`
			EventIDs []int64 `json:"event_ids"`
		} `json:"blue_case"`
	}
	for _, item := range messages.Items {
		if item.Kind == "user" && item.Summary == "Review this case" {
			if err := json.Unmarshal(item.Metadata, &attachedMetadata); err != nil {
				t.Fatal(err)
			}
		}
	}
	if attachedMetadata.BlueCase.ID != caseID || len(attachedMetadata.BlueCase.EventIDs) != 1 ||
		attachedMetadata.BlueCase.EventIDs[0] != imported[0].ID {
		t.Fatalf("chat evidence provenance not persisted: %+v", attachedMetadata)
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

func TestBlueChatSelectsOlderCaseEvidencePG(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ARTEX_BLUE_TEST_PG_DSN"))
	if dsn == "" {
		t.Skip("set ARTEX_BLUE_TEST_PG_DSN to a disposable PostgreSQL test database")
	}
	pg, err := db.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pg.Close() })
	ctx := context.Background()
	caseItem, err := pg.CreateBlueCase(ctx, db.BlueCase{Title: "Older evidence selection", Status: "open", Severity: "medium"})
	if err != nil {
		t.Fatal(err)
	}
	otherCase, err := pg.CreateBlueCase(ctx, db.BlueCase{Title: "Other evidence selection", Status: "open", Severity: "medium"})
	if err != nil {
		t.Fatal(err)
	}
	rule, err := pg.CreateBlueRule(ctx, db.BlueRule{Name: "Older evidence selection", Field: "event_type", Value: "http", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	var allEvents []db.BlueEvent
	t.Cleanup(func() {
		_, _ = pg.Exec(`DELETE FROM blue_alerts WHERE rule_id=$1`, rule.ID)
		_, _ = pg.Exec(`DELETE FROM blue_rules WHERE id=$1`, rule.ID)
		for _, event := range allEvents {
			_, _ = pg.Exec(`DELETE FROM blue_events WHERE id=$1`, event.ID)
		}
		_, _ = pg.Exec(`DELETE FROM blue_cases WHERE id IN ($1,$2)`, caseItem.ID, otherCase.ID)
	})
	base := time.Now().UTC().Add(-time.Hour)
	eventsToImport := []db.BlueEvent{
		{CaseID: &caseItem.ID, Source: "selected-chat-evidence", EventType: "http", Severity: "medium",
			Message: "OLD-SELECTED-EVENT", OccurredAt: base, Raw: json.RawMessage(`{"path":"/old"}`)},
		{CaseID: &otherCase.ID, Source: "selected-chat-evidence", EventType: "http", Severity: "medium",
			Message: "PRIVATE-OTHER-CASE-EVENT", OccurredAt: base.Add(time.Second), Raw: json.RawMessage(`{"path":"/private"}`)},
	}
	for i := 0; i < blueChatEvidenceEvents+2; i++ {
		eventsToImport = append(eventsToImport, db.BlueEvent{CaseID: &caseItem.ID, Source: "selected-chat-evidence", EventType: "http",
			Severity: "info", Message: fmt.Sprintf("newer event %d", i), OccurredAt: base.Add(time.Duration(i+2) * time.Second),
			Raw: json.RawMessage(`{"path":"/recent"}`)})
	}
	allEvents, err = pg.ImportBlueEvents(ctx, eventsToImport)
	if err != nil {
		t.Fatal(err)
	}
	alerts := make([]*db.BlueAlert, 0, len(allEvents))
	for _, event := range allEvents {
		alert, err := pg.UpsertBlueAlert(ctx, db.BlueAlert{RuleID: rule.ID, EventID: event.ID, CaseID: event.CaseID,
			Reason: "alert for " + event.Message})
		if err != nil {
			t.Fatal(err)
		}
		alerts = append(alerts, alert)
	}
	provider := &blueChatTestProvider{}
	s := &Server{m: &Manager{pg: pg}, ctx: ctx, jwtKey: []byte(strings.Repeat("s", 32)), llmOn: true, llmProv: provider}
	h := s.Handler()
	token, err := signJWT(s.signingKey(s.readPassHash()))
	if err != nil {
		t.Fatal(err)
	}
	var conv db.Conversation
	blueAPIRequest(t, h, token, http.MethodPost, "/api/blue/conversations", map[string]any{"title": "selected-evidence-test"}, http.StatusOK, &conv)
	t.Cleanup(func() { _ = pg.DeleteConversation(conv.ID) })
	path := "/api/blue/conversations/" + i64s(conv.ID) + "/messages"
	blueAPIRequest(t, h, token, http.MethodPost, path, map[string]any{"message": "No case", "event_ids": []int64{allEvents[0].ID}}, http.StatusBadRequest, nil)
	blueAPIRequest(t, h, token, http.MethodPost, path, map[string]any{"message": "Wrong event", "case_id": caseItem.ID, "event_ids": []int64{allEvents[1].ID}}, http.StatusBadRequest, nil)
	blueAPIRequest(t, h, token, http.MethodPost, path, map[string]any{"message": "Wrong alert", "case_id": caseItem.ID, "alert_ids": []int64{alerts[1].ID}}, http.StatusBadRequest, nil)
	blueAPIRequest(t, h, token, http.MethodPost, path, map[string]any{"message": "Duplicate", "case_id": caseItem.ID, "event_ids": []int64{allEvents[0].ID, allEvents[0].ID}}, http.StatusBadRequest, nil)
	blueAPIRequest(t, h, token, http.MethodPost, path, map[string]any{"message": "Too many", "case_id": caseItem.ID, "event_ids": make([]int64, blueChatEvidenceEvents+1)}, http.StatusBadRequest, nil)
	if len(provider.snapshot()) != 0 {
		t.Fatal("invalid evidence reached the model")
	}
	blueAPIRequest(t, h, token, http.MethodPost, path, map[string]any{"message": "Investigate this old record", "case_id": caseItem.ID,
		"event_ids": []int64{allEvents[0].ID}, "alert_ids": []int64{alerts[0].ID}}, http.StatusAccepted, nil)
	waitBlueChatIdle(t, h, token, "/api/blue/conversations/"+i64s(conv.ID))
	requests := provider.snapshot()
	if len(requests) != 1 {
		t.Fatalf("expected one model request, got %d", len(requests))
	}
	var prompt string
	for _, message := range requests[0].Messages {
		prompt += message.Text()
	}
	if !strings.Contains(prompt, "OLD-SELECTED-EVENT") || !strings.Contains(prompt, "alert for OLD-SELECTED-EVENT") ||
		strings.Contains(prompt, "PRIVATE-OTHER-CASE-EVENT") || !strings.Contains(prompt, `"selected_event_ids"`) {
		t.Fatalf("selected case evidence missing or crossed case boundary: %q", prompt)
	}
	var messages struct {
		Items []ActivityDTO `json:"items"`
	}
	blueAPIRequest(t, h, token, http.MethodGet, path, nil, http.StatusOK, &messages)
	var selected struct {
		BlueCase struct {
			EventIDs         []int64 `json:"event_ids"`
			AlertIDs         []int64 `json:"alert_ids"`
			SelectedEventIDs []int64 `json:"selected_event_ids"`
			SelectedAlertIDs []int64 `json:"selected_alert_ids"`
		} `json:"blue_case"`
	}
	for _, item := range messages.Items {
		if item.Kind == "user" && item.Summary == "Investigate this old record" {
			if err := json.Unmarshal(item.Metadata, &selected); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(selected.BlueCase.SelectedEventIDs) != 1 || selected.BlueCase.SelectedEventIDs[0] != allEvents[0].ID ||
		len(selected.BlueCase.SelectedAlertIDs) != 1 || selected.BlueCase.SelectedAlertIDs[0] != alerts[0].ID ||
		len(selected.BlueCase.EventIDs) != blueChatEvidenceEvents || len(selected.BlueCase.AlertIDs) != blueChatEvidenceAlerts {
		t.Fatalf("selected provenance missing: %+v", selected)
	}
	largeRaw, err := json.Marshal(map[string]string{"blob": strings.Repeat("界", 600)})
	if err != nil {
		t.Fatal(err)
	}
	largeRecords := make([]db.BlueEvent, 0, blueChatEvidenceEvents)
	for i := 0; i < blueChatEvidenceEvents; i++ {
		largeRecords = append(largeRecords, db.BlueEvent{CaseID: &caseItem.ID, Source: "selected-chat-evidence", EventType: "http",
			Severity: "info", Message: strings.Repeat("界", 600), OccurredAt: base.Add(time.Duration(i+100) * time.Second), Raw: largeRaw})
	}
	largeStored, err := pg.ImportBlueEvents(ctx, largeRecords)
	if err != nil {
		t.Fatal(err)
	}
	allEvents = append(allEvents, largeStored...)
	largeIDs := make([]int64, 0, len(largeStored))
	for _, event := range largeStored {
		largeIDs = append(largeIDs, event.ID)
	}
	blueAPIRequest(t, h, token, http.MethodPost, path, map[string]any{"message": "Do not silently drop my selected evidence", "case_id": caseItem.ID,
		"event_ids": largeIDs}, http.StatusBadRequest, nil)
	if len(provider.snapshot()) != 1 {
		t.Fatal("oversized selected evidence reached the model")
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

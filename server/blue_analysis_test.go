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

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/norma/llm"
)

type heldBlueProvider struct {
	started chan struct{}
	release chan struct{}
	output  string
}

func (p *heldBlueProvider) Stream(context.Context, llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	return func(func(llm.StreamEvent, error) bool) {}
}

func (p *heldBlueProvider) Complete(ctx context.Context, req llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
	if len(req.Tools) != 0 {
		return llm.Message{}, "", llm.Usage{}, fmt.Errorf("blue analyst received %d tools", len(req.Tools))
	}
	close(p.started)
	select {
	case <-p.release:
	case <-ctx.Done():
		return llm.Message{}, "", llm.Usage{}, ctx.Err()
	}
	return llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.TextBlock(p.output)}}, "end_turn", llm.Usage{}, nil
}

func TestBlueAnalysisAsyncEvidenceAndHistoryPG(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ARTEX_BLUE_TEST_PG_DSN"))
	if dsn == "" {
		t.Skip("set ARTEX_BLUE_TEST_PG_DSN to a disposable PostgreSQL test database")
	}
	pg, err := db.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pg.Close() })
	actor, err := pg.GetAgentByKey("blue_analyst")
	if err != nil || actor == nil {
		t.Fatalf("blue analyst seed missing: %v", err)
	}
	if err := pg.SetAgentLLMProfile(actor.Key, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pg.SetAgentLLMProfile(actor.Key, actor.LLMProfileID) })
	var caseID, eventID int64
	fixture := fmt.Sprintf("blue-analysis-%d", time.Now().UnixNano())
	if err := pg.QueryRow(`INSERT INTO blue_cases (title) VALUES ($1) RETURNING id`, fixture).Scan(&caseID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pg.Exec(`DELETE FROM blue_cases WHERE id=$1`, caseID)
		_, _ = pg.Exec(`DELETE FROM blue_events WHERE id=$1`, eventID)
	})
	if err := pg.QueryRow(`INSERT INTO blue_events (case_id,source,event_type,message,occurred_at,raw)
		VALUES ($1,'test','auth','login failure',now(),'{}'::jsonb) RETURNING id`, caseID).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	provider := &heldBlueProvider{started: make(chan struct{}), release: make(chan struct{})}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(provider.release) }) }
	t.Cleanup(release)
	provider.output = fmt.Sprintf(`{"summary":"One login failure needs review","verdict":"inconclusive","confidence":"medium","severity":"unknown","facts":[{"text":"A login failure was recorded","evidence":[{"kind":"event","id":%d}]}],"hypotheses":[],"unknowns":["Account context was not supplied"],"detection_ideas":[],"response_recommendations":[]}`, eventID)
	s := &Server{m: &Manager{pg: pg}, ctx: context.Background(), jwtKey: []byte(strings.Repeat("j", 32)),
		llmOn: true, llmProv: provider}
	h := s.Handler()
	token, err := signJWT(s.signingKey(s.readPassHash()))
	if err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/api/blue/cases/%d", caseID)
	var queued struct {
		Run db.BlueAnalysisRun `json:"run"`
	}
	blueAPIRequest(t, h, token, http.MethodPost, path+"/analyze", nil, http.StatusAccepted, &queued)
	if queued.Run.ID <= 0 || queued.Run.CaseID != caseID {
		t.Fatalf("invalid queued run: %+v", queued.Run)
	}
	select {
	case <-provider.started:
	case <-time.After(5 * time.Second):
		t.Fatal("background analysis did not start")
	}
	blueAPIRequest(t, h, token, http.MethodPost, path+"/analyze", nil, http.StatusConflict, nil)
	release()
	var result struct {
		Run db.BlueAnalysisRun `json:"run"`
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		blueAPIRequest(t, h, token, http.MethodGet, fmt.Sprintf("/api/blue/analyses/%d", queued.Run.ID), nil, http.StatusOK, &result)
		if result.Run.Status == "completed" {
			break
		}
		if result.Run.Status == "failed" || time.Now().After(deadline) {
			t.Fatalf("analysis did not complete: %+v", result.Run)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if result.Run.ObservationID == nil {
		t.Fatalf("completed run lacks observation: %+v", result.Run)
	}
	var history struct {
		Runs []db.BlueAnalysisRun `json:"runs"`
	}
	blueAPIRequest(t, h, token, http.MethodGet, path+"/analyses", nil, http.StatusOK, &history)
	if len(history.Runs) == 0 || history.Runs[0].ID != queued.Run.ID {
		t.Fatalf("analysis history missing run: %+v", history.Runs)
	}
	var observations []db.BlueObservation
	blueAPIRequest(t, h, token, http.MethodGet, path+"/observations", nil, http.StatusOK, &observations)
	if len(observations) == 0 || observations[0].ID != *result.Run.ObservationID ||
		len(observations[0].EvidenceEventIDs) != 1 || observations[0].EvidenceEventIDs[0] != eventID {
		t.Fatalf("analysis observation lacks verified evidence: %+v", observations)
	}
	var assessment map[string]any
	if err := json.Unmarshal(observations[0].Assessment, &assessment); err != nil {
		t.Fatal(err)
	}
	if assessment["verdict"] != "inconclusive" || assessment["confidence"] != "medium" || assessment["severity"] != "unknown" {
		t.Fatalf("structured assessment was not persisted: %+v", assessment)
	}
}

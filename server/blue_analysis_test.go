package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Autumn-27/artex/blue/analysis"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/norma/llm"
)

type heldBlueProvider struct {
	started  chan struct{}
	release  chan struct{}
	captured chan llm.CompletionRequest
	output   string
}

func TestReadBlueEvidenceSelectionRejectsInvalidIDs(t *testing.T) {
	for _, body := range []string{
		`{"event_ids":[0]}`,
		`{"event_ids":[2,2]}`,
		`{"alert_ids":[-1]}`,
		`{"event_ids":[1,2,3,4,5,6,7,8,9,10,11,12,13]}`,
	} {
		r := httptest.NewRequest(http.MethodPost, "/api/blue/cases/1/analyze", strings.NewReader(body))
		w := httptest.NewRecorder()
		if _, ok := readBlueEvidenceSelection(w, r); ok || w.Code != http.StatusBadRequest {
			t.Fatalf("accepted invalid selected evidence %s: status=%d", body, w.Code)
		}
	}
}

func TestBlueAnalysisInputIncludesSelectedOlderCaseEvidencePG(t *testing.T) {
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
	var caseID, otherCaseID, oldEventID, otherEventID, ruleID int64
	name := fmt.Sprintf("blue-selected-%d", time.Now().UnixNano())
	for i, destination := range []*int64{&caseID, &otherCaseID} {
		if err := pg.QueryRow(`INSERT INTO blue_cases (title) VALUES ($1) RETURNING id`, fmt.Sprintf("%s-%d", name, i)).Scan(destination); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = pg.Exec(`DELETE FROM blue_events WHERE source=$1`, name)
		_, _ = pg.Exec(`DELETE FROM blue_rules WHERE id=$1`, ruleID)
		_, _ = pg.Exec(`DELETE FROM blue_cases WHERE id IN ($1,$2)`, caseID, otherCaseID)
	})
	for i := 0; i < analysis.MaxEvents+5; i++ {
		var eventID int64
		if err := pg.QueryRow(`INSERT INTO blue_events (case_id,source,event_type,message,occurred_at,raw)
			VALUES ($1,$2,'auth',$3,now(),'{}'::jsonb) RETURNING id`, caseID, name, fmt.Sprintf("event %d", i)).Scan(&eventID); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			oldEventID = eventID
		}
	}
	if err := pg.QueryRow(`INSERT INTO blue_events (case_id,source,event_type,message,occurred_at,raw)
		VALUES ($1,$2,'auth','other case',now(),'{}'::jsonb) RETURNING id`, otherCaseID, name).Scan(&otherEventID); err != nil {
		t.Fatal(err)
	}
	item, err := pg.GetBlueCase(ctx, caseID)
	if err != nil || item == nil {
		t.Fatalf("load case: %v, %+v", err, item)
	}
	in, err := blueAnalysisInput(ctx, pg, item, blueEvidenceSelection{EventIDs: []int64{oldEventID}})
	if err != nil {
		t.Fatal(err)
	}
	if len(in.Events) != analysis.MaxEvents || in.Events[0].ID != oldEventID || !in.EventsTruncated ||
		len(in.SelectedEventIDs) != 1 || in.SelectedEventIDs[0] != oldEventID || !analysis.FitsSelectedEvidence(in) {
		t.Fatalf("old selected evidence was not prioritized: first=%d total=%d truncated=%t", in.Events[0].ID, len(in.Events), in.EventsTruncated)
	}
	run, err := pg.CreateBlueAnalysisRunWithSelection(ctx, caseID, nil, []int64{oldEventID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := pg.GetBlueAnalysisRun(ctx, run.ID)
	if err != nil || stored == nil || len(stored.SelectedEventIDs) != 1 || stored.SelectedEventIDs[0] != oldEventID {
		t.Fatalf("analysis focus was not persisted: %v, %+v", err, stored)
	}
	if _, err := blueAnalysisInput(ctx, pg, item, blueEvidenceSelection{EventIDs: []int64{otherEventID}}); !errors.Is(err, errSelectedBlueEvidenceUnavailable) {
		t.Fatalf("accepted cross-case event: %v", err)
	}
	rule, err := pg.CreateBlueRule(ctx, db.BlueRule{Name: name, Field: "message", Operator: "contains", Value: "event", Severity: "medium", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	ruleID = rule.ID
	alert, err := pg.UpsertBlueAlert(ctx, db.BlueAlert{RuleID: ruleID, EventID: oldEventID, CaseID: &caseID})
	if err != nil {
		t.Fatal(err)
	}
	otherAlert, err := pg.UpsertBlueAlert(ctx, db.BlueAlert{RuleID: ruleID, EventID: otherEventID, CaseID: &otherCaseID})
	if err != nil {
		t.Fatal(err)
	}
	in, err = blueAnalysisInput(ctx, pg, item, blueEvidenceSelection{AlertIDs: []int64{alert.ID}})
	if err != nil || len(in.Alerts) == 0 || in.Alerts[0].ID != alert.ID {
		t.Fatalf("selected case alert unavailable: %v, %+v", err, in.Alerts)
	}
	if _, err := blueAnalysisInput(ctx, pg, item, blueEvidenceSelection{AlertIDs: []int64{otherAlert.ID}}); !errors.Is(err, errSelectedBlueEvidenceUnavailable) {
		t.Fatalf("accepted cross-case alert: %v", err)
	}
}

func (p *heldBlueProvider) Stream(context.Context, llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	return func(func(llm.StreamEvent, error) bool) {}
}

func (p *heldBlueProvider) Complete(ctx context.Context, req llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
	if len(req.Tools) != 0 {
		return llm.Message{}, "", llm.Usage{}, fmt.Errorf("blue analyst received %d tools", len(req.Tools))
	}
	if p.captured != nil {
		p.captured <- req
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

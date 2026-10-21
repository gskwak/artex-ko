package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Autumn-27/artex/db"
)

func TestBlueTaskStatusAndPriority(t *testing.T) {
	for _, value := range []string{"open", "in_progress", "completed", "blocked"} {
		if !validBlueTaskStatus(value) {
			t.Fatalf("valid task status %q rejected", value)
		}
	}
	for _, value := range []string{"low", "medium", "high", "critical"} {
		if !validBlueTaskPriority(value) {
			t.Fatalf("valid task priority %q rejected", value)
		}
	}
	if validBlueTaskStatus("running") || validBlueTaskPriority("urgent") {
		t.Fatal("invalid task value accepted")
	}
}

func TestBlueTaskWorkflowPG(t *testing.T) {
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

	var caseID, eventID, taskID int64
	fixture := fmt.Sprintf("blue-task-%d", time.Now().UnixNano())
	if err := pg.QueryRow(`INSERT INTO blue_cases (title) VALUES ($1) RETURNING id`, fixture).Scan(&caseID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if taskID != 0 {
			_, _ = pg.Exec(`DELETE FROM blue_tasks WHERE id=$1`, taskID)
		}
		if eventID != 0 {
			_, _ = pg.Exec(`DELETE FROM blue_events WHERE id=$1`, eventID)
		}
		_, _ = pg.Exec(`DELETE FROM blue_cases WHERE id=$1`, caseID)
	})
	if err := pg.QueryRow(`INSERT INTO blue_events (case_id,source,event_type,message,occurred_at,raw)
		VALUES ($1,'test','auth','failed login',now(),'{}'::jsonb) RETURNING id`, caseID).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	provider := &heldBlueProvider{started: make(chan struct{}), release: make(chan struct{})}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(provider.release) }) }
	t.Cleanup(release)
	provider.output = fmt.Sprintf(`{"summary":"Investigate the failed login","verdict":"inconclusive","confidence":"medium","severity":"unknown","facts":[{"text":"A failed login was recorded","evidence":[{"kind":"event","id":%d}]}],"hypotheses":[],"unknowns":[],"detection_ideas":[],"response_recommendations":[]}`, eventID)
	s := &Server{m: &Manager{pg: pg}, ctx: context.Background(), jwtKey: []byte(strings.Repeat("k", 32)),
		llmOn: true, llmProv: provider}
	h := s.Handler()
	token, err := signJWT(s.signingKey(s.readPassHash()))
	if err != nil {
		t.Fatal(err)
	}
	blueAPIRequest(t, h, "", http.MethodGet, "/api/blue/tasks", nil, http.StatusUnauthorized, nil)
	blueAPIRequest(t, h, token, http.MethodPost, "/api/blue/tasks",
		map[string]any{"title": " ", "case_id": caseID}, http.StatusBadRequest, nil)
	blueAPIRequest(t, h, token, http.MethodPost, "/api/blue/tasks",
		map[string]any{"title": fixture, "case_id": caseID + 99999999}, http.StatusNotFound, nil)

	var task db.BlueTask
	blueAPIRequest(t, h, token, http.MethodPost, "/api/blue/tasks",
		map[string]any{"title": fixture, "description": "Review authentication evidence", "priority": "high"},
		http.StatusCreated, &task)
	taskID = task.ID
	if task.ID <= 0 || task.CaseID != nil || task.Status != "open" || task.Priority != "high" || task.Result != "" {
		t.Fatalf("unexpected task creation: %+v", task)
	}
	path := fmt.Sprintf("/api/blue/tasks/%d", taskID)
	blueAPIRequest(t, h, token, http.MethodPost, path+"/run", nil, http.StatusConflict, nil)
	blueAPIRequest(t, h, token, http.MethodPatch, path,
		map[string]any{"case_id": caseID, "priority": "critical"}, http.StatusOK, &task)
	if task.CaseID == nil || *task.CaseID != caseID || task.Priority != "critical" {
		t.Fatalf("task patch did not persist case and priority: %+v", task)
	}
	var listed []db.BlueTask
	blueAPIRequest(t, h, token, http.MethodGet,
		fmt.Sprintf("/api/blue/tasks?case_id=%d&status=open", caseID), nil, http.StatusOK, &listed)
	if len(listed) != 1 || listed[0].ID != taskID {
		t.Fatalf("task filtering omitted linked work item: %+v", listed)
	}

	var queued struct {
		Task db.BlueTask        `json:"task"`
		Run  db.BlueAnalysisRun `json:"run"`
	}
	blueAPIRequest(t, h, token, http.MethodPost, path+"/run", nil, http.StatusAccepted, &queued)
	if queued.Task.Status != "in_progress" || queued.Task.AnalysisRunID == nil ||
		*queued.Task.AnalysisRunID != queued.Run.ID || queued.Run.CaseID != caseID {
		t.Fatalf("task was not bound to queued analysis: %+v", queued)
	}
	select {
	case <-provider.started:
	case <-time.After(5 * time.Second):
		t.Fatal("task analysis did not start")
	}
	blueAPIRequest(t, h, token, http.MethodPatch, path,
		map[string]any{"status": "completed"}, http.StatusConflict, nil)
	blueAPIRequest(t, h, token, http.MethodPost, path+"/run", nil, http.StatusConflict, nil)
	release()
	deadline := time.Now().Add(5 * time.Second)
	for {
		blueAPIRequest(t, h, token, http.MethodGet, path, nil, http.StatusOK, &task)
		if task.Status == "completed" {
			break
		}
		if task.Status == "blocked" || time.Now().After(deadline) {
			t.Fatalf("task analysis did not complete: %+v", task)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if task.CompletedAt == nil || !strings.Contains(task.Result, "Investigate the failed login") {
		t.Fatalf("completed task lacks analyst result: %+v", task)
	}
	task = db.BlueTask{}
	blueAPIRequest(t, h, token, http.MethodPatch, path,
		map[string]any{"status": "open", "result": "Follow up with IAM"}, http.StatusOK, &task)
	if task.Status != "open" || task.Result != "Follow up with IAM" || task.CompletedAt != nil {
		t.Fatalf("manual follow-up did not persist: %+v", task)
	}

	// A server restart fails a queued in-process analysis. Its linked task must
	// leave in_progress and surface the interruption for a later retry.
	interrupted, err := pg.CreateBlueAnalysisRun(context.Background(), caseID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pg.AttachBlueTaskRun(context.Background(), taskID, interrupted.ID); err != nil {
		t.Fatal(err)
	}
	if err := pg.FailInterruptedBlueAnalysisRuns(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := pg.SyncFinishedBlueTaskRuns(context.Background()); err != nil {
		t.Fatal(err)
	}
	task = db.BlueTask{}
	blueAPIRequest(t, h, token, http.MethodGet, path, nil, http.StatusOK, &task)
	if task.Status != "blocked" || !strings.Contains(task.Result, "interrupted") {
		t.Fatalf("recovered task did not show interruption: %+v", task)
	}
}

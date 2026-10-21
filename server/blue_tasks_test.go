package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Autumn-27/artex/blue/analysis"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/norma/llm"
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
	provider := &heldBlueProvider{started: make(chan struct{}), release: make(chan struct{}), captured: make(chan llm.CompletionRequest, 1)}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(provider.release) }) }
	t.Cleanup(release)
	provider.output = fmt.Sprintf(`{"summary":"Investigate the failed login","verdict":"inconclusive","confidence":"medium","severity":"unknown","facts":[{"text":"A failed login was recorded","evidence":[{"kind":"event","id":%d}]}],"hypotheses":[],"unknowns":[],"detection_ideas":[],"response_recommendations":[],"task_outcome":{"status":"answered","summary":"The requested authentication review found one failed login.","evidence":[{"kind":"event","id":%d}]}}`, eventID, eventID)
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
	var input analysis.Input
	select {
	case request := <-provider.captured:
		if len(request.System) != 3 || request.System[2] != analysis.TaskRuntimeContract || len(request.Tools) != 0 {
			t.Fatalf("task-specific bounded contract missing: %+v", request)
		}
		if err := json.Unmarshal([]byte(request.Messages[0].Content[0].Text), &input); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatal("task analysis did not capture model input")
	}
	if input.Task == nil || input.Task.ID != taskID || input.Task.Title != fixture ||
		input.Task.Description != "Review authentication evidence" || input.Task.Priority != "critical" {
		t.Fatalf("task instructions were not supplied to the model: %+v", input.Task)
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
	if task.CompletedAt == nil || !strings.Contains(task.Result, "The requested authentication review found one failed login") ||
		!strings.Contains(task.Result, fmt.Sprintf("Task #%d: %s", taskID, fixture)) {
		t.Fatalf("completed task lacks analyst result: %+v", task)
	}
	// The detail form sends its prior status and result with edits. Changing
	// the request must reopen the task and detach that stale run regardless.
	previousResult := task.Result
	task = db.BlueTask{}
	blueAPIRequest(t, h, token, http.MethodPatch, path,
		map[string]any{"title": fixture + " revised", "description": "Review the source IP", "case_id": caseID,
			"status": "completed", "result": previousResult}, http.StatusOK, &task)
	if task.Status != "open" || task.Result != "" || task.AnalysisRunID != nil || task.CompletedAt != nil ||
		task.Title != fixture+" revised" || task.Description != "Review the source IP" {
		t.Fatalf("material task edit retained a stale analyst outcome: %+v", task)
	}
	task = db.BlueTask{}
	blueAPIRequest(t, h, token, http.MethodPatch, path,
		map[string]any{"status": "open", "result": "Follow up with IAM"}, http.StatusOK, &task)
	if task.Status != "open" || task.Result != "Follow up with IAM" || task.CompletedAt != nil {
		t.Fatalf("manual follow-up did not persist: %+v", task)
	}
	blueAPIRequest(t, h, token, http.MethodPatch, path,
		map[string]any{"status": "completed", "result": "Manually reviewed"}, http.StatusOK, &task)
	if task.Status != "completed" || task.CompletedAt == nil {
		t.Fatalf("manual completion did not persist: %+v", task)
	}
	previousResult = task.Result
	task = db.BlueTask{}
	blueAPIRequest(t, h, token, http.MethodPatch, path,
		map[string]any{"clear_case": true, "status": "completed", "result": previousResult}, http.StatusOK, &task)
	if task.CaseID != nil || task.Status != "open" || task.Result != "" || task.CompletedAt != nil {
		t.Fatalf("case edit retained an outcome from the previous case: %+v", task)
	}
	blueAPIRequest(t, h, token, http.MethodPatch, path,
		map[string]any{"case_id": caseID}, http.StatusOK, &task)

	// A completed model call is not a completed work item when the evidence
	// cannot answer the task. Preserve its explanation and allow a later retry.
	unanswered, err := pg.CreateBlueAnalysisRun(context.Background(), caseID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pg.AttachBlueTaskRun(context.Background(), taskID, unanswered.ID); err != nil {
		t.Fatal(err)
	}
	obs, err := pg.CreateBlueObservation(context.Background(), db.BlueObservation{
		CaseID: caseID, EventID: &eventID, EvidenceEventIDs: []int64{eventID},
		Kind: "analysis", Summary: "Missing source IP", Detail: "Task outcome: insufficient_evidence. The source IP was not recorded.",
		Assessment: json.RawMessage(`{"task_outcome":{"status":"insufficient_evidence"}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pg.UpdateBlueAnalysisRun(context.Background(), unanswered.ID, "running", nil, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := pg.UpdateBlueAnalysisRun(context.Background(), unanswered.ID, "completed", &obs.ID, ""); err != nil {
		t.Fatal(err)
	}
	if err := pg.SyncBlueTaskFromRun(context.Background(), unanswered.ID); err != nil {
		t.Fatal(err)
	}
	task = db.BlueTask{}
	blueAPIRequest(t, h, token, http.MethodGet, path, nil, http.StatusOK, &task)
	if task.Status != "blocked" || task.CompletedAt != nil || !strings.Contains(task.Result, "source IP was not recorded") {
		t.Fatalf("unanswerable task was marked completed or lost its explanation: %+v", task)
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

func TestBlueTaskStopPG(t *testing.T) {
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
	if err := pg.QueryRow(`INSERT INTO blue_cases (title) VALUES ($1) RETURNING id`, fmt.Sprintf("blue-stop-%d", time.Now().UnixNano())).Scan(&caseID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pg.Exec(`DELETE FROM blue_cases WHERE id=$1`, caseID) })
	if err := pg.QueryRow(`INSERT INTO blue_events (case_id,source,event_type,message,occurred_at,raw)
		VALUES ($1,'test','auth','failed login',now(),'{}'::jsonb) RETURNING id`, caseID).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	provider := &heldBlueProvider{started: make(chan struct{}), release: make(chan struct{}),
		output: fmt.Sprintf(`{"summary":"A failed login occurred","verdict":"inconclusive","confidence":"low","severity":"unknown","facts":[{"text":"A login failed","evidence":[{"kind":"event","id":%d}]}],"hypotheses":[],"unknowns":[],"detection_ideas":[],"response_recommendations":[],"task_outcome":{"status":"answered","summary":"A failed login was found","evidence":[{"kind":"event","id":%d}]}}`, eventID, eventID)}
	t.Cleanup(func() { close(provider.release) })
	s := &Server{m: &Manager{pg: pg}, ctx: context.Background(), jwtKey: []byte(strings.Repeat("s", 32)),
		llmOn: true, llmProv: provider}
	h := s.Handler()
	token, err := signJWT(s.signingKey(s.readPassHash()))
	if err != nil {
		t.Fatal(err)
	}
	var task db.BlueTask
	blueAPIRequest(t, h, token, http.MethodPost, "/api/blue/tasks",
		map[string]any{"title": "Review failed login", "case_id": caseID}, http.StatusCreated, &task)
	path := fmt.Sprintf("/api/blue/tasks/%d", task.ID)
	var queued struct {
		Task db.BlueTask        `json:"task"`
		Run  db.BlueAnalysisRun `json:"run"`
	}
	blueAPIRequest(t, h, token, http.MethodPost, path+"/run", nil, http.StatusAccepted, &queued)
	select {
	case <-provider.started:
	case <-time.After(5 * time.Second):
		t.Fatal("task analysis did not start")
	}
	task = db.BlueTask{}
	blueAPIRequest(t, h, token, http.MethodPost, path+"/stop", nil, http.StatusOK, &task)
	if task.Status != "blocked" || task.CompletedAt != nil || !strings.Contains(task.Result, "stopped by user") {
		t.Fatalf("stopped task did not retain cancellation outcome: %+v", task)
	}
	blueAPIRequest(t, h, token, http.MethodPost, path+"/stop", nil, http.StatusConflict, nil)
	deadline := time.Now().Add(5 * time.Second)
	for len(blueAnalysisSlots) != 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if len(blueAnalysisSlots) != 0 {
		t.Fatal("stopped analysis kept its concurrency slot")
	}
	run, err := pg.GetBlueAnalysisRun(context.Background(), queued.Run.ID)
	if err != nil || run == nil || run.Status != "failed" || run.ObservationID != nil {
		t.Fatalf("stopped run completed or retained observation: run=%+v err=%v", run, err)
	}
}

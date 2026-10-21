package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/Autumn-27/artex/blue/analysis"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/norma/llm"
	"github.com/Autumn-27/norma/transcript"
)

const (
	blueAnalysisTimeout = 2 * time.Minute
	blueAnalysisDBTime  = 10 * time.Second
)

// A case analysis is a bounded background job. This cap protects shared LLM
// profiles from unbounded manual starts; a new request receives 429 when full.
var blueAnalysisSlots = make(chan struct{}, 2)

func (s *Server) blueAnalyzeCase(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	caseID, ok := blueID(w, r)
	if !ok {
		return
	}
	job, status, message := s.prepareBlueCaseAnalysis(r.Context(), pg, caseID)
	if job == nil {
		writeErr(w, status, message)
		return
	}
	s.startBlueAnalysis(pg, job)
	writeJSON(w, http.StatusAccepted, map[string]any{"run": &job.run})
}

// A prepared job has a persisted queued run and owns one analysis slot. The
// caller can attach a work item before launching its worker, avoiding a race
// where a very fast model finishes before the work item records the run ID.
type bluePreparedAnalysis struct {
	run       db.BlueAnalysisRun
	provider  llm.Provider
	maxTokens int
	strategy  string
	input     analysis.Input
}

// Register cancellation before starting the worker. This closes the gap where
// a stop request could arrive after the queued response but before the model
// goroutine began.
func (s *Server) startBlueAnalysis(pg *db.DB, job *bluePreparedAnalysis) {
	parent := s.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, blueAnalysisTimeout)
	s.blueAnalysisMu.Lock()
	if s.blueAnalysisCancel == nil {
		s.blueAnalysisCancel = make(map[int64]context.CancelFunc)
	}
	s.blueAnalysisCancel[job.run.ID] = cancel
	s.blueAnalysisMu.Unlock()
	go func() {
		defer func() {
			s.blueAnalysisMu.Lock()
			delete(s.blueAnalysisCancel, job.run.ID)
			s.blueAnalysisMu.Unlock()
			cancel()
		}()
		s.runBlueAnalysis(ctx, pg, job.run, job.provider, job.maxTokens, job.strategy, job.input)
	}()
}

func (s *Server) cancelBlueAnalysis(id int64) {
	s.blueAnalysisMu.Lock()
	cancel := s.blueAnalysisCancel[id]
	s.blueAnalysisMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *Server) prepareBlueCaseAnalysis(ctx context.Context, pg *db.DB, caseID int64) (*bluePreparedAnalysis, int, string) {
	internal := func(err error) (*bluePreparedAnalysis, int, string) {
		log.Printf("[blue] prepare analysis: %v", err)
		return nil, http.StatusInternalServerError, "blue-team request failed"
	}
	caseItem, err := pg.GetBlueCase(ctx, caseID)
	if err != nil {
		return internal(err)
	}
	if caseItem == nil {
		return nil, http.StatusNotFound, "case not found"
	}
	actor, err := pg.GetAgentByKey("blue_analyst")
	if err != nil {
		return internal(err)
	}
	if actor == nil || !actor.Enabled {
		return nil, http.StatusConflict, "blue analyst is disabled"
	}

	// The editable prompt and model binding are read at job creation, so editing
	// either setting while a run is active cannot silently change that run.
	strategy, err := pg.CurrentPrompt(actor.ID)
	if err != nil {
		return internal(err)
	}
	prov, cfg, profileID, ready := s.blueAnalystProvider(actor)
	if !ready {
		return nil, http.StatusServiceUnavailable, "configure a model for the blue analyst"
	}
	input, err := blueAnalysisInput(ctx, pg, caseItem)
	if err != nil {
		return internal(err)
	}
	if len(input.Events)+len(input.Alerts) == 0 {
		return nil, http.StatusConflict, "add case events or alerts before analysis"
	}
	select {
	case blueAnalysisSlots <- struct{}{}:
	default:
		return nil, http.StatusTooManyRequests, "blue analysis capacity is busy; retry shortly"
	}
	run, err := pg.CreateBlueAnalysisRun(ctx, caseID, profileID)
	if err != nil {
		<-blueAnalysisSlots
		if errors.Is(err, db.ErrBlueConflict) {
			return nil, http.StatusConflict, "this case already has an active analysis"
		}
		return internal(err)
	}
	return &bluePreparedAnalysis{run: *run, provider: prov, maxTokens: cfg.MaxTokens,
		strategy: strategy, input: input}, 0, ""
}

func (s *Server) blueAnalystProvider(actor *db.Agent) (llm.Provider, intConfig, *int64, bool) {
	if actor.LLMProfileID != nil {
		if prov, cfg, ok := s.agentBindingProvider(actor.Key); ok {
			return prov, intConfig{MaxTokens: cfg.MaxTokens}, actor.LLMProfileID, true
		}
	}
	prov, cfg, ok := s.globalProvider()
	if !ok {
		return nil, intConfig{}, nil, false
	}
	var profileID *int64
	if p, err := s.m.pg.ActiveProfile(); err == nil && p != nil {
		id := p.ID
		profileID = &id
	}
	return prov, intConfig{MaxTokens: cfg.MaxTokens}, profileID, true
}

// This local shape prevents the HTTP handler from passing provider credentials
// into the background job; it only needs the output token budget.
type intConfig struct{ MaxTokens int }

func blueAnalysisInput(ctx context.Context, pg *db.DB, item *db.BlueCase) (analysis.Input, error) {
	id := item.ID
	events, err := pg.ListBlueEvents(ctx, &id, "", analysis.MaxEvents+1, 0)
	if err != nil {
		return analysis.Input{}, err
	}
	alerts, err := pg.ListBlueAlerts(ctx, &id, "", "", analysis.MaxAlerts+1, 0)
	if err != nil {
		return analysis.Input{}, err
	}
	in := analysis.Input{CaseID: id, Title: item.Title, Description: item.Description,
		Status: item.Status, Severity: item.Severity,
		EventsTruncated: len(events) > analysis.MaxEvents, AlertsTruncated: len(alerts) > analysis.MaxAlerts,
		Events: make([]analysis.Event, 0, min(len(events), analysis.MaxEvents)),
		Alerts: make([]analysis.Alert, 0, min(len(alerts), analysis.MaxAlerts))}
	if len(events) > analysis.MaxEvents {
		events = events[:analysis.MaxEvents]
	}
	if len(alerts) > analysis.MaxAlerts {
		alerts = alerts[:analysis.MaxAlerts]
	}
	for _, e := range events {
		in.Events = append(in.Events, analysis.Event{ID: e.ID, Source: e.Source,
			EventType: e.EventType, Severity: e.Severity, Asset: e.Asset,
			Message: e.Message, OccurredAt: e.OccurredAt, Raw: e.Raw})
	}
	for _, a := range alerts {
		in.Alerts = append(in.Alerts, analysis.Alert{ID: a.ID, RuleID: a.RuleID,
			EventID: a.EventID, Title: a.Title, Severity: a.Severity,
			Status: a.Status, Reason: a.Reason})
	}
	return in, nil
}

func (s *Server) runBlueAnalysis(ctx context.Context, pg *db.DB, run db.BlueAnalysisRun, prov llm.Provider, maxTokens int, strategy string, in analysis.Input) {
	defer func() { <-blueAnalysisSlots }()
	defer func() {
		if failure := recover(); failure != nil {
			log.Printf("[blue] analysis run %d panicked: %v", run.ID, failure)
			_, _ = s.blueUpdateRun(pg, run.ID, "failed", nil, "analysis stopped unexpectedly")
		}
	}()
	if _, err := s.blueUpdateRun(pg, run.ID, "running", nil, ""); err != nil {
		log.Printf("[blue] start analysis run %d: %v", run.ID, err)
		return
	}
	ctx = transcript.WithSessionID(ctx, "blue-case-"+strconv.FormatInt(run.CaseID, 10)+"-analysis-"+strconv.FormatInt(run.ID, 10))
	assessment, err := analysis.AnalyzeWithPrompt(ctx, prov, strategy, in, maxTokens)
	if err != nil {
		log.Printf("[blue] analysis run %d failed: %v", run.ID, err)
		_, _ = s.blueUpdateRun(pg, run.ID, "failed", nil, "model response failed or did not pass evidence validation")
		return
	}
	if ctx.Err() != nil {
		// Some providers may return a final response despite cancellation. Never
		// persist it after the user has stopped the run.
		_, _ = s.blueUpdateRun(pg, run.ID, "failed", nil, "analysis stopped before completion")
		return
	}
	eventIDs, alertIDs := assessment.EvidenceIDs()
	assessmentJSON, err := json.Marshal(assessment)
	if err != nil {
		log.Printf("[blue] encode analysis run %d: %v", run.ID, err)
		_, _ = s.blueUpdateRun(pg, run.ID, "failed", nil, "could not encode analysis observation")
		return
	}
	var primaryEvent, primaryAlert *int64
	if len(eventIDs) > 0 {
		primaryEvent = &eventIDs[0]
	}
	if len(alertIDs) > 0 {
		primaryAlert = &alertIDs[0]
	}
	storeCtx, done := context.WithTimeout(context.Background(), blueAnalysisDBTime)
	summary, detail := assessment.Summary, assessment.Markdown()
	if in.Task != nil {
		summary = assessment.TaskOutcome.Summary
		detail = fmt.Sprintf("Task #%d: %s\n\n%s", in.Task.ID, in.Task.Title, detail)
	}
	obs, err := pg.CreateBlueObservation(storeCtx, db.BlueObservation{
		CaseID: run.CaseID, EventID: primaryEvent, AlertID: primaryAlert,
		EvidenceEventIDs: eventIDs, EvidenceAlertIDs: alertIDs,
		Kind: "analysis", Summary: summary, Detail: detail,
		Assessment: assessmentJSON, Author: "blue_analyst",
	})
	done()
	if err != nil {
		log.Printf("[blue] store analysis run %d: %v", run.ID, err)
		_, _ = s.blueUpdateRun(pg, run.ID, "failed", nil, "could not save analysis observation")
		return
	}
	if _, err := s.blueUpdateRun(pg, run.ID, "completed", &obs.ID, ""); err != nil {
		log.Printf("[blue] complete analysis run %d: %v", run.ID, err)
		cleanupCtx, cancel := context.WithTimeout(context.Background(), blueAnalysisDBTime)
		if cleanupErr := pg.DeleteUnattachedBlueAnalysisObservation(cleanupCtx, obs.ID); cleanupErr != nil {
			log.Printf("[blue] clean up incomplete analysis observation %d: %v", obs.ID, cleanupErr)
		}
		cancel()
	}
}

func (s *Server) blueUpdateRun(pg *db.DB, id int64, status string, obsID *int64, errorText string) (*db.BlueAnalysisRun, error) {
	ctx, cancel := context.WithTimeout(context.Background(), blueAnalysisDBTime)
	defer cancel()
	run, err := pg.UpdateBlueAnalysisRun(ctx, id, status, obsID, errorText)
	if err == nil && (status == "completed" || status == "failed") {
		if syncErr := pg.SyncBlueTaskFromRun(ctx, id); syncErr != nil {
			log.Printf("[blue] sync task for analysis run %d: %v", id, syncErr)
		}
	}
	return run, err
}

func (s *Server) blueListCaseAnalyses(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id, ok := blueID(w, r)
	if !ok {
		return
	}
	item, err := pg.GetBlueCase(r.Context(), id)
	if err != nil {
		blueInternal(w, err)
		return
	}
	if item == nil {
		writeErr(w, http.StatusNotFound, "case not found")
		return
	}
	limit, offset, ok := blueLimitOffset(w, r)
	if !ok {
		return
	}
	runs, err := pg.ListBlueAnalysisRuns(r.Context(), id, limit, offset)
	if err != nil {
		blueInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs})
}

func (s *Server) blueGetAnalysis(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id, ok := blueID(w, r)
	if !ok {
		return
	}
	run, err := pg.GetBlueAnalysisRun(r.Context(), id)
	if err != nil {
		blueInternal(w, err)
		return
	}
	if run == nil {
		writeErr(w, http.StatusNotFound, "analysis run not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"run": run})
}

// recoverBlueAnalyses is invoked during startup. A background goroutine cannot
// survive process exit; persisted queued/running rows must not stay stuck.
func (s *Server) recoverBlueAnalyses() {
	if s.m.pg == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), blueAnalysisDBTime)
	defer cancel()
	if err := s.m.pg.FailInterruptedBlueAnalysisRuns(ctx); err != nil {
		log.Printf("[blue] recover interrupted analyses: %v", err)
	}
	if err := s.m.pg.SyncFinishedBlueTaskRuns(ctx); err != nil {
		log.Printf("[blue] recover analysis tasks: %v", err)
	}
}

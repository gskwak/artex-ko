package server

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/Autumn-27/artex/db"
	"github.com/jackc/pgx/v5/pgconn"
)

func validBlueTaskStatus(status string) bool {
	switch status {
	case "open", "in_progress", "completed", "blocked":
		return true
	}
	return false
}

func validBlueTaskPriority(priority string) bool {
	switch priority {
	case "low", "medium", "high", "critical":
		return true
	}
	return false
}

func (s *Server) blueTaskCaseExists(w http.ResponseWriter, r *http.Request, pg *db.DB, id *int64) bool {
	if id == nil {
		return true
	}
	if *id <= 0 {
		writeErr(w, http.StatusBadRequest, "case_id must be positive")
		return false
	}
	item, err := pg.GetBlueCase(r.Context(), *id)
	if err != nil {
		blueInternal(w, err)
		return false
	}
	if item == nil {
		writeErr(w, http.StatusNotFound, "case not found")
		return false
	}
	return true
}

func blueTaskStoreError(w http.ResponseWriter, err error) {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		writeErr(w, http.StatusNotFound, "case not found")
		return
	}
	blueInternal(w, err)
}

func (s *Server) blueListTasks(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	limit, offset, ok := blueLimitOffset(w, r)
	if !ok {
		return
	}
	status := r.URL.Query().Get("status")
	if status != "" && !validBlueTaskStatus(status) {
		writeErr(w, http.StatusBadRequest, "invalid task status")
		return
	}
	caseID, ok := blueQueryID(w, r, "case_id")
	if !ok {
		return
	}
	items, err := pg.ListBlueTasks(r.Context(), status, caseID, limit, offset)
	if err != nil {
		blueInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) blueCreateTask(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	var req struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		CaseID      *int64 `json:"case_id"`
		Priority    string `json:"priority"`
	}
	if !decodeBlueJSON(w, r, &req) {
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" || len(req.Title) > 200 {
		writeErr(w, http.StatusBadRequest, "task title is required and must be at most 200 bytes")
		return
	}
	if len(req.Description) > 20000 {
		writeErr(w, http.StatusBadRequest, "task description is too long")
		return
	}
	if req.Priority == "" {
		req.Priority = "medium"
	}
	if !validBlueTaskPriority(req.Priority) {
		writeErr(w, http.StatusBadRequest, "invalid task priority")
		return
	}
	if !s.blueTaskCaseExists(w, r, pg, req.CaseID) {
		return
	}
	item, err := pg.CreateBlueTask(r.Context(), db.BlueTask{
		Title: req.Title, Description: req.Description, CaseID: req.CaseID, Priority: req.Priority,
	})
	if err != nil {
		blueTaskStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) blueGetTask(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id, ok := blueID(w, r)
	if !ok {
		return
	}
	item, err := pg.GetBlueTask(r.Context(), id)
	if err != nil {
		blueInternal(w, err)
		return
	}
	if item == nil {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) bluePatchTask(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id, ok := blueID(w, r)
	if !ok {
		return
	}
	var patch db.BlueTaskPatch
	if !decodeBlueJSON(w, r, &patch) {
		return
	}
	if patch.Title != nil {
		*patch.Title = strings.TrimSpace(*patch.Title)
		if *patch.Title == "" || len(*patch.Title) > 200 {
			writeErr(w, http.StatusBadRequest, "invalid task title")
			return
		}
	}
	if patch.Description != nil && len(*patch.Description) > 20000 {
		writeErr(w, http.StatusBadRequest, "task description is too long")
		return
	}
	if patch.Result != nil && len(*patch.Result) > 50000 {
		writeErr(w, http.StatusBadRequest, "task result is too long")
		return
	}
	if patch.Status != nil && !validBlueTaskStatus(*patch.Status) {
		writeErr(w, http.StatusBadRequest, "invalid task status")
		return
	}
	if patch.Priority != nil && !validBlueTaskPriority(*patch.Priority) {
		writeErr(w, http.StatusBadRequest, "invalid task priority")
		return
	}
	if patch.CaseID != nil && patch.ClearCase {
		writeErr(w, http.StatusBadRequest, "case_id and clear_case cannot be used together")
		return
	}
	if !s.blueTaskCaseExists(w, r, pg, patch.CaseID) {
		return
	}
	item, err := pg.PatchBlueTask(r.Context(), id, patch)
	if errors.Is(err, db.ErrBlueConflict) {
		writeErr(w, http.StatusConflict, "task has an active analysis")
		return
	}
	if err != nil {
		blueTaskStoreError(w, err)
		return
	}
	if item == nil {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) blueRunTask(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id, ok := blueID(w, r)
	if !ok {
		return
	}
	item, err := pg.GetBlueTask(r.Context(), id)
	if err != nil {
		blueInternal(w, err)
		return
	}
	if item == nil {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	if item.CaseID == nil {
		writeErr(w, http.StatusConflict, "link a case before running this task")
		return
	}
	if item.Status != "open" && item.Status != "blocked" {
		writeErr(w, http.StatusConflict, "only open or blocked tasks can be run")
		return
	}
	job, status, message := s.prepareBlueCaseAnalysis(r.Context(), pg, *item.CaseID)
	if job == nil {
		writeErr(w, status, message)
		return
	}
	updated, err := pg.AttachBlueTaskRun(r.Context(), id, job.run.ID)
	if err != nil || updated == nil {
		if _, finishErr := s.blueUpdateRun(pg, job.run.ID, "failed", nil, "task changed before analysis started"); finishErr != nil {
			log.Printf("[blue] abandon task analysis run %d: %v", job.run.ID, finishErr)
		}
		<-blueAnalysisSlots
		if errors.Is(err, db.ErrBlueConflict) {
			writeErr(w, http.StatusConflict, "task changed before analysis started")
		} else if err != nil {
			blueInternal(w, err)
		} else {
			writeErr(w, http.StatusNotFound, "task not found")
		}
		return
	}
	go s.runBlueAnalysis(pg, job.run, job.provider, job.maxTokens, job.strategy, job.input)
	writeJSON(w, http.StatusAccepted, map[string]any{"task": updated, "run": &job.run})
}

package server

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Autumn-27/artex/blue/detection"
	"github.com/Autumn-27/artex/blue/ingest"
	"github.com/Autumn-27/artex/db"
)

const (
	maxBlueBodyBytes   = 8 << 20
	maxBlueImportItems = 500
	maxBlueListLimit   = 1000
)

// registerBlueRoutes keeps defensive investigations separate from the red-team
// exploration API. The Server's normal JWT middleware protects all these paths.
func (s *Server) registerBlueRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/blue/overview", s.blueOverview)
	mux.HandleFunc("GET /api/blue/cases", s.blueListCases)
	mux.HandleFunc("POST /api/blue/cases", s.blueCreateCase)
	mux.HandleFunc("POST /api/blue/cases/from-finding", s.blueCaseFromFinding)
	mux.HandleFunc("GET /api/blue/cases/{id}", s.blueGetCase)
	mux.HandleFunc("PATCH /api/blue/cases/{id}", s.bluePatchCase)
	mux.HandleFunc("GET /api/blue/events", s.blueListEvents)
	mux.HandleFunc("GET /api/blue/events/{id}", s.blueGetEvent)
	mux.HandleFunc("POST /api/blue/events/import", s.blueImportEvents)
	mux.HandleFunc("POST /api/blue/events/{id}/move-case", s.blueMoveEventCase)
	mux.HandleFunc("GET /api/blue/rules", s.blueListRules)
	mux.HandleFunc("POST /api/blue/rules", s.blueCreateRule)
	mux.HandleFunc("GET /api/blue/rules/{id}", s.blueGetRule)
	mux.HandleFunc("PATCH /api/blue/rules/{id}", s.bluePatchRule)
	mux.HandleFunc("POST /api/blue/rules/{id}/test", s.blueTestRule)
	mux.HandleFunc("POST /api/blue/rules/{id}/run", s.blueRunRule)
	mux.HandleFunc("GET /api/blue/alerts", s.blueListAlerts)
	mux.HandleFunc("GET /api/blue/alerts/{id}", s.blueGetAlert)
	mux.HandleFunc("PATCH /api/blue/alerts/{id}", s.bluePatchAlert)
	mux.HandleFunc("GET /api/blue/cases/{id}/observations", s.blueListObservations)
	mux.HandleFunc("POST /api/blue/cases/{id}/observations", s.blueCreateObservation)
	mux.HandleFunc("POST /api/blue/cases/{id}/analyze", s.blueAnalyzeCase)
	mux.HandleFunc("GET /api/blue/cases/{id}/analyses", s.blueListCaseAnalyses)
	mux.HandleFunc("GET /api/blue/analyses/{id}", s.blueGetAnalysis)
	mux.HandleFunc("GET /api/blue/actions", s.blueListActions)
	mux.HandleFunc("GET /api/blue/cases/{id}/actions", s.blueListCaseActions)
	mux.HandleFunc("POST /api/blue/cases/{id}/actions", s.blueCreateAction)
	mux.HandleFunc("POST /api/blue/actions/{id}/decision", s.blueDecideAction)
	mux.HandleFunc("POST /api/blue/actions/{id}/complete", s.blueCompleteAction)
}

func decodeBlueJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBlueBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeErr(w, http.StatusRequestEntityTooLarge, "blue-team request body is too large")
		} else {
			writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		}
		return false
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeErr(w, http.StatusRequestEntityTooLarge, "blue-team request body is too large")
		} else {
			writeErr(w, http.StatusBadRequest, "request body must contain one JSON value")
		}
		return false
	}
	return true
}

func blueID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, ok := pathInt(r, "id")
	if !ok || id <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid blue-team resource id")
		return 0, false
	}
	return id, true
}

func blueLimitOffset(w http.ResponseWriter, r *http.Request) (int, int, bool) {
	limit, offset := 100, 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > maxBlueListLimit {
			writeErr(w, http.StatusBadRequest, "limit must be between 1 and 1000")
			return 0, 0, false
		}
		limit = n
	}
	if raw := r.URL.Query().Get("offset"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			writeErr(w, http.StatusBadRequest, "offset must be nonnegative")
			return 0, 0, false
		}
		offset = n
	}
	return limit, offset, true
}

func blueQueryID(w http.ResponseWriter, r *http.Request, key string) (*int64, bool) {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return nil, true
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 {
		writeErr(w, http.StatusBadRequest, key+" must be a positive integer")
		return nil, false
	}
	return &n, true
}

func validBlueSeverity(v string) bool {
	switch v {
	case "info", "low", "medium", "high", "critical":
		return true
	}
	return false
}

func validBlueCaseStatus(v string) bool {
	switch v {
	case "open", "investigating", "contained", "resolved", "closed":
		return true
	}
	return false
}

func validBlueAlertStatus(v string) bool {
	switch v {
	case "new", "investigating", "confirmed", "false_positive", "dismissed":
		return true
	}
	return false
}

func validBlueObservationKind(v string) bool {
	switch v {
	case "fact", "hypothesis", "unknown", "recommendation", "analysis":
		return true
	}
	return false
}

func blueNotFound(w http.ResponseWriter, err error, entity string) bool {
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, http.StatusNotFound, entity+" not found")
		return true
	}
	return false
}

func blueInternal(w http.ResponseWriter, err error) {
	log.Printf("[blue] internal error: %v", err)
	writeErr(w, http.StatusInternalServerError, "blue-team request failed")
}

func (s *Server) blueOverview(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	result, err := pg.BlueOverview(r.Context())
	if err != nil {
		blueInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) blueListCases(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	limit, offset, ok := blueLimitOffset(w, r)
	if !ok {
		return
	}
	status := r.URL.Query().Get("status")
	if status != "" && !validBlueCaseStatus(status) {
		writeErr(w, http.StatusBadRequest, "invalid case status")
		return
	}
	items, err := pg.ListBlueCases(r.Context(), status, limit, offset)
	if err != nil {
		blueInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) blueCreateCase(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	var req db.BlueCase
	if !decodeBlueJSON(w, r, &req) {
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" || len(req.Title) > 200 {
		writeErr(w, http.StatusBadRequest, "case title is required and must be at most 200 bytes")
		return
	}
	if len(req.Description) > 20000 || len(req.Summary) > 20000 {
		writeErr(w, http.StatusBadRequest, "case description or summary is too long")
		return
	}
	if req.Status == "" {
		req.Status = "open"
	}
	if req.Severity == "" {
		req.Severity = "info"
	}
	if !validBlueCaseStatus(req.Status) || !validBlueSeverity(req.Severity) {
		writeErr(w, http.StatusBadRequest, "invalid case status or severity")
		return
	}
	if req.RedFindingID != nil {
		finding, err := pg.GetFinding(*req.RedFindingID)
		if err != nil {
			blueInternal(w, err)
			return
		}
		if finding == nil {
			writeErr(w, http.StatusNotFound, "red-team finding not found")
			return
		}
		if finding.Status != "confirmed" {
			writeErr(w, http.StatusConflict, "only confirmed red-team findings can be linked")
			return
		}
	}
	item, err := pg.CreateBlueCase(r.Context(), req)
	if err != nil {
		blueInternal(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) blueCaseFromFinding(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	var req struct {
		FindingID int64 `json:"finding_id"`
	}
	if !decodeBlueJSON(w, r, &req) {
		return
	}
	if req.FindingID <= 0 {
		writeErr(w, http.StatusBadRequest, "finding_id must be positive")
		return
	}
	f, err := pg.GetFinding(req.FindingID)
	if err != nil {
		blueInternal(w, err)
		return
	}
	if f == nil {
		writeErr(w, http.StatusNotFound, "red-team finding not found")
		return
	}
	if f.Status != "confirmed" {
		writeErr(w, http.StatusConflict, "only confirmed red-team findings can be linked")
		return
	}
	title := strings.TrimSpace(f.Name)
	if title == "" {
		title = strings.TrimSpace(f.VulnClass)
	}
	if title == "" {
		title = fmt.Sprintf("Red-team finding #%d", f.ID)
	}
	severity := f.Severity
	if !validBlueSeverity(severity) {
		severity = "medium"
	}
	item, err := pg.CreateBlueCase(r.Context(), db.BlueCase{
		Title: title, Description: f.Summary, Summary: "Validate detection and response coverage for this red-team finding.",
		Status: "open", Severity: severity, RedFindingID: &f.ID,
	})
	if err != nil {
		blueInternal(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) blueGetCase(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id, ok := blueID(w, r)
	if !ok {
		return
	}
	item, err := pg.GetBlueCase(r.Context(), id)
	if blueNotFound(w, err, "case") {
		return
	}
	if err != nil {
		blueInternal(w, err)
		return
	}
	if item == nil {
		writeErr(w, http.StatusNotFound, "case not found")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) bluePatchCase(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id, ok := blueID(w, r)
	if !ok {
		return
	}
	var patch db.BlueCasePatch
	if !decodeBlueJSON(w, r, &patch) {
		return
	}
	if patch.Title != nil && (strings.TrimSpace(*patch.Title) == "" || len(*patch.Title) > 200) {
		writeErr(w, http.StatusBadRequest, "invalid case title")
		return
	}
	if patch.Status != nil && !validBlueCaseStatus(*patch.Status) {
		writeErr(w, http.StatusBadRequest, "invalid case status")
		return
	}
	if patch.Severity != nil && !validBlueSeverity(*patch.Severity) {
		writeErr(w, http.StatusBadRequest, "invalid case severity")
		return
	}
	if (patch.Description != nil && len(*patch.Description) > 20000) ||
		(patch.Summary != nil && len(*patch.Summary) > 20000) {
		writeErr(w, http.StatusBadRequest, "case description or summary is too long")
		return
	}
	item, err := pg.PatchBlueCase(r.Context(), id, patch)
	if blueNotFound(w, err, "case") {
		return
	}
	if err != nil {
		blueInternal(w, err)
		return
	}
	if item == nil {
		writeErr(w, http.StatusNotFound, "case not found")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) blueListEvents(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	limit, offset, ok := blueLimitOffset(w, r)
	if !ok {
		return
	}
	caseID, ok := blueQueryID(w, r, "case_id")
	if !ok {
		return
	}
	source := strings.TrimSpace(r.URL.Query().Get("source"))
	if len(source) > 200 {
		writeErr(w, http.StatusBadRequest, "source filter is too long")
		return
	}
	items, err := pg.ListBlueEvents(r.Context(), caseID, source, limit, offset)
	if err != nil {
		blueInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) blueGetEvent(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id, ok := blueID(w, r)
	if !ok {
		return
	}
	item, err := pg.GetBlueEvent(r.Context(), id)
	if blueNotFound(w, err, "event") {
		return
	}
	if err != nil {
		blueInternal(w, err)
		return
	}
	if item == nil {
		writeErr(w, http.StatusNotFound, "event not found")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) blueMoveEventCase(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id, ok := blueID(w, r)
	if !ok {
		return
	}
	var req struct {
		CaseID int64 `json:"case_id"`
	}
	if !decodeBlueJSON(w, r, &req) {
		return
	}
	if req.CaseID <= 0 {
		writeErr(w, http.StatusBadRequest, "case_id must be a positive integer")
		return
	}
	caseItem, err := pg.GetBlueCase(r.Context(), req.CaseID)
	if err != nil {
		blueInternal(w, err)
		return
	}
	if caseItem == nil {
		writeErr(w, http.StatusNotFound, "case not found")
		return
	}
	item, err := pg.MoveBlueEventCase(r.Context(), id, req.CaseID)
	if blueNotFound(w, err, "event") {
		return
	}
	if errors.Is(err, db.ErrBlueConflict) {
		writeErr(w, http.StatusConflict, "event or alert evidence is cited by an observation")
		return
	}
	if err != nil {
		blueInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) blueImportEvents(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	events, ok := parseBlueImportEvents(w, r)
	if !ok {
		return
	}
	if len(events) == 0 || len(events) > maxBlueImportItems {
		writeErr(w, http.StatusBadRequest, "events must contain between 1 and 500 records")
		return
	}
	for i := range events {
		ev := &events[i]
		ev.Source = strings.TrimSpace(ev.Source)
		ev.ExternalID = strings.TrimSpace(ev.ExternalID)
		ev.EventType = strings.TrimSpace(ev.EventType)
		ev.Message = strings.TrimSpace(ev.Message)
		ev.Asset = strings.TrimSpace(ev.Asset)
		if ev.Source == "" || ev.EventType == "" || ev.Message == "" {
			writeErr(w, http.StatusBadRequest, fmt.Sprintf("events[%d] requires source, event_type and message", i))
			return
		}
		if len(ev.Source) > 200 || len(ev.ExternalID) > 500 || len(ev.EventType) > 200 || len(ev.Asset) > 500 || len(ev.Message) > 10000 {
			writeErr(w, http.StatusBadRequest, fmt.Sprintf("events[%d] contains an oversized field", i))
			return
		}
		if ev.Severity == "" {
			ev.Severity = "info"
		}
		if !validBlueSeverity(ev.Severity) {
			writeErr(w, http.StatusBadRequest, fmt.Sprintf("events[%d] has invalid severity", i))
			return
		}
		if ev.OccurredAt.IsZero() {
			ev.OccurredAt = time.Now().UTC()
		}
		if ev.CaseID != nil {
			caseItem, err := pg.GetBlueCase(r.Context(), *ev.CaseID)
			if err != nil {
				blueInternal(w, err)
				return
			}
			if caseItem == nil {
				writeErr(w, http.StatusBadRequest, fmt.Sprintf("events[%d] has unknown case_id", i))
				return
			}
		}
		if len(ev.Raw) == 0 || bytes.Equal(bytes.TrimSpace(ev.Raw), []byte("null")) {
			ev.Raw = json.RawMessage(`{}`)
		}
		if !json.Valid(ev.Raw) {
			writeErr(w, http.StatusBadRequest, fmt.Sprintf("events[%d] has invalid raw JSON", i))
			return
		}
	}
	items, err := pg.ImportBlueEvents(r.Context(), events)
	if err != nil {
		blueInternal(w, err)
		return
	}
	matched, created, detectionErrors, warnings := s.blueApplyEnabledRules(r, pg, items)
	writeJSON(w, http.StatusCreated, map[string]any{
		"imported": len(items), "events": items,
		"alerts_matched": matched, "alerts_created": created,
		"detection_errors": detectionErrors, "warnings": warnings,
	})
}

func parseBlueImportEvents(w http.ResponseWriter, r *http.Request) ([]db.BlueEvent, bool) {
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaType != "multipart/form-data" {
		var req struct {
			Events []db.BlueEvent `json:"events"`
		}
		if !decodeBlueJSON(w, r, &req) {
			return nil, false
		}
		return req.Events, true
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBlueBodyBytes+(1<<20))
	if err := r.ParseMultipartForm(maxBlueBodyBytes); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeErr(w, http.StatusRequestEntityTooLarge, "event upload is too large")
		} else {
			writeErr(w, http.StatusBadRequest, "invalid event file upload: "+err.Error())
		}
		return nil, false
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "file is required")
		return nil, false
	}
	defer file.Close()
	parsed, err := ingest.Parse(file, r.FormValue("format"), ingest.Options{
		MaxBytes: maxBlueBodyBytes, MaxRecords: maxBlueImportItems,
		DefaultSource: strings.TrimSpace(r.FormValue("source")),
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid event file: "+err.Error())
		return nil, false
	}
	events := make([]db.BlueEvent, 0, len(parsed))
	for _, e := range parsed {
		events = append(events, db.BlueEvent{
			Source: e.Source, ExternalID: e.ExternalID, EventType: e.EventType,
			Severity: e.Severity, Asset: e.Asset, Message: e.Message,
			OccurredAt: e.OccurredAt, Raw: e.Raw,
		})
	}
	return events, true
}

func (s *Server) blueListRules(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	limit, offset, ok := blueLimitOffset(w, r)
	if !ok {
		return
	}
	items, err := pg.ListBlueRules(r.Context(), limit, offset)
	if err != nil {
		blueInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func blueCompileRule(rule db.BlueRule) (*detection.Rule, error) {
	def := bytes.TrimSpace(rule.Definition)
	if len(def) != 0 && !bytes.Equal(def, []byte("{}")) && !bytes.Equal(def, []byte("null")) {
		return detection.CompileJSON(def)
	}
	return detection.CompileSimple(rule.Field, rule.Operator, rule.Value)
}

func (s *Server) blueCreateRule(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	var req struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Field       string          `json:"field"`
		Operator    string          `json:"operator"`
		Value       string          `json:"value"`
		Severity    string          `json:"severity"`
		Enabled     *bool           `json:"enabled"`
		Definition  json.RawMessage `json:"definition"`
		SigmaYAML   string          `json:"sigma_yaml"`
	}
	if !decodeBlueJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > 200 || len(req.Description) > 10000 {
		writeErr(w, http.StatusBadRequest, "invalid rule name or description")
		return
	}
	if req.Severity == "" {
		req.Severity = "medium"
	}
	if !validBlueSeverity(req.Severity) {
		writeErr(w, http.StatusBadRequest, "invalid rule severity")
		return
	}
	if req.Operator == "" {
		req.Operator = "equals"
	}
	if len(req.Field) > 200 || len(req.Operator) > 50 || len(req.Value) > 10000 {
		writeErr(w, http.StatusBadRequest, "rule field, operator or value is too long")
		return
	}
	if req.SigmaYAML != "" {
		if len(req.Definition) != 0 {
			writeErr(w, http.StatusBadRequest, "provide definition or sigma_yaml, not both")
			return
		}
		definition, err := detection.ConvertSigmaYAML([]byte(req.SigmaYAML))
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid Sigma YAML: "+err.Error())
			return
		}
		req.Definition = definition
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	rule := db.BlueRule{
		Name: req.Name, Description: req.Description, Field: req.Field, Operator: req.Operator,
		Value: req.Value, Severity: req.Severity, Enabled: enabled, Definition: req.Definition,
	}
	if _, err := blueCompileRule(rule); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid detection rule: "+err.Error())
		return
	}
	item, err := pg.CreateBlueRule(r.Context(), rule)
	if err != nil {
		blueInternal(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) blueGetRule(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id, ok := blueID(w, r)
	if !ok {
		return
	}
	item, err := pg.GetBlueRule(r.Context(), id)
	if blueNotFound(w, err, "rule") {
		return
	}
	if err != nil {
		blueInternal(w, err)
		return
	}
	if item == nil {
		writeErr(w, http.StatusNotFound, "rule not found")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) bluePatchRule(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id, ok := blueID(w, r)
	if !ok {
		return
	}
	var req struct {
		Name        *string         `json:"name"`
		Description *string         `json:"description"`
		Field       *string         `json:"field"`
		Operator    *string         `json:"operator"`
		Value       *string         `json:"value"`
		Severity    *string         `json:"severity"`
		Enabled     *bool           `json:"enabled"`
		Definition  json.RawMessage `json:"definition"`
		SigmaYAML   *string         `json:"sigma_yaml"`
	}
	if !decodeBlueJSON(w, r, &req) {
		return
	}
	patch := db.BlueRulePatch{
		Name: req.Name, Description: req.Description, Field: req.Field,
		Operator: req.Operator, Value: req.Value, Severity: req.Severity, Enabled: req.Enabled,
	}
	if len(req.Definition) > 0 {
		patch.Definition = &req.Definition
	}
	if req.SigmaYAML != nil {
		if patch.Definition != nil {
			writeErr(w, http.StatusBadRequest, "provide definition or sigma_yaml, not both")
			return
		}
		definition, err := detection.ConvertSigmaYAML([]byte(*req.SigmaYAML))
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid Sigma YAML: "+err.Error())
			return
		}
		patch.Definition = &definition
	}
	old, err := pg.GetBlueRule(r.Context(), id)
	if err != nil {
		blueInternal(w, err)
		return
	}
	if old == nil {
		writeErr(w, http.StatusNotFound, "rule not found")
		return
	}
	candidate := *old
	if patch.Name != nil {
		candidate.Name = strings.TrimSpace(*patch.Name)
		*patch.Name = candidate.Name
	}
	if patch.Description != nil {
		candidate.Description = *patch.Description
	}
	if patch.Field != nil {
		candidate.Field = *patch.Field
	}
	if patch.Operator != nil {
		candidate.Operator = *patch.Operator
	}
	if patch.Value != nil {
		candidate.Value = *patch.Value
	}
	if patch.Severity != nil {
		candidate.Severity = *patch.Severity
	}
	if patch.Definition != nil {
		candidate.Definition = *patch.Definition
	}
	if candidate.Name == "" || len(candidate.Name) > 200 || len(candidate.Description) > 10000 ||
		len(candidate.Field) > 200 || len(candidate.Operator) > 50 || len(candidate.Value) > 10000 ||
		!validBlueSeverity(candidate.Severity) {
		writeErr(w, http.StatusBadRequest, "invalid rule name, description or severity")
		return
	}
	if _, err := blueCompileRule(candidate); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid detection rule: "+err.Error())
		return
	}
	item, err := pg.PatchBlueRule(r.Context(), id, patch)
	if blueNotFound(w, err, "rule") {
		return
	}
	if err != nil {
		blueInternal(w, err)
		return
	}
	if item == nil {
		writeErr(w, http.StatusNotFound, "rule not found")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

type blueRuleMatch struct {
	EventID    int64     `json:"event_id"`
	Source     string    `json:"source"`
	EventType  string    `json:"event_type"`
	Message    string    `json:"message"`
	OccurredAt time.Time `json:"occurred_at"`
}

func blueEventFields(ev db.BlueEvent) map[string]any {
	fields := map[string]any{
		"source": ev.Source, "event_type": ev.EventType, "severity": ev.Severity,
		"asset": ev.Asset, "message": ev.Message, "occurred_at": ev.OccurredAt.Format(time.RFC3339Nano),
	}
	var raw any
	if err := json.Unmarshal(ev.Raw, &raw); err == nil {
		fields["raw"] = raw
	} else {
		fields["raw"] = map[string]any{}
	}
	if obj, ok := raw.(map[string]any); ok {
		if logsource, ok := obj["logsource"].(map[string]any); ok {
			fields["logsource"] = logsource
		}
	}
	if fields["logsource"] == nil {
		fields["logsource"] = map[string]any{"product": ev.Source, "service": ev.EventType}
	}
	return fields
}

func blueAlertFromMatch(rule db.BlueRule, event db.BlueEvent) db.BlueAlert {
	return db.BlueAlert{
		RuleID: rule.ID, EventID: event.ID, CaseID: event.CaseID,
		Title: rule.Name, Severity: rule.Severity, Status: "new",
		Reason: fmt.Sprintf("Detection rule #%d matched event #%d.", rule.ID, event.ID),
	}
}

// Import evaluation runs after the event transaction so that an evaluation
// failure cannot discard original telemetry. The response makes partial
// detection failures explicit to the importing analyst.
func (s *Server) blueApplyEnabledRules(r *http.Request, pg *db.DB, events []db.BlueEvent) (int, int, int, []string) {
	matched, created, failures := 0, 0, 0
	warnings := []string{}
	report := func(summary string, err error) {
		failures++
		if len(warnings) < 20 {
			warnings = append(warnings, summary)
			log.Printf("[blue-detection] %s: %v", summary, err)
		}
	}
	for offset := 0; ; offset += 200 {
		rules, err := pg.ListBlueRules(r.Context(), 200, offset)
		if err != nil {
			report("could not list enabled detection rules", err)
			return matched, created, failures, warnings
		}
		for _, rule := range rules {
			if !rule.Enabled {
				continue
			}
			compiled, err := blueCompileRule(rule)
			if err != nil {
				report(fmt.Sprintf("rule #%d could not be compiled", rule.ID), err)
				continue
			}
			for _, ev := range events {
				if !compiled.Match(blueEventFields(ev)) {
					continue
				}
				matched++
				alert, err := pg.UpsertBlueAlert(r.Context(), blueAlertFromMatch(rule, ev))
				if err != nil {
					report(fmt.Sprintf("rule #%d could not store alert for event #%d", rule.ID, ev.ID), err)
					continue
				}
				if alert.Created {
					created++
				}
			}
		}
		if len(rules) < 200 {
			break
		}
	}
	return matched, created, failures, warnings
}

func blueOptionalLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var req struct {
		Limit int `json:"limit"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil && err != io.EOF {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeErr(w, http.StatusRequestEntityTooLarge, "run options are too large")
		} else {
			writeErr(w, http.StatusBadRequest, "invalid run options: "+err.Error())
		}
		return 0, false
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeErr(w, http.StatusRequestEntityTooLarge, "run options are too large")
		} else {
			writeErr(w, http.StatusBadRequest, "run options must contain one JSON value")
		}
		return 0, false
	}
	if req.Limit == 0 {
		req.Limit = 5000
	}
	if req.Limit < 1 || req.Limit > 50000 {
		writeErr(w, http.StatusBadRequest, "limit must be between 1 and 50000")
		return 0, false
	}
	return req.Limit, true
}

func (s *Server) blueEvaluateRule(w http.ResponseWriter, r *http.Request, persist bool) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id, ok := blueID(w, r)
	if !ok {
		return
	}
	limit, ok := blueOptionalLimit(w, r)
	if !ok {
		return
	}
	rule, err := pg.GetBlueRule(r.Context(), id)
	if err != nil {
		blueInternal(w, err)
		return
	}
	if rule == nil {
		writeErr(w, http.StatusNotFound, "rule not found")
		return
	}
	if persist && !rule.Enabled {
		writeErr(w, http.StatusConflict, "enable the rule before running it")
		return
	}
	compiled, err := blueCompileRule(*rule)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid detection rule: "+err.Error())
		return
	}
	matched, created, scanned := 0, 0, 0
	matches := make([]blueRuleMatch, 0, 100)
	maxID, err := pg.BlueEventHighWatermark(r.Context())
	if err != nil {
		blueInternal(w, err)
		return
	}
	var afterID int64
	for scanned < limit {
		pageSize := 250
		if left := limit - scanned; left < pageSize {
			pageSize = left
		}
		events, err := pg.ListBlueEventsAfterID(r.Context(), afterID, maxID, pageSize)
		if err != nil {
			blueInternal(w, err)
			return
		}
		for _, ev := range events {
			afterID = ev.ID
			scanned++
			if !compiled.Match(blueEventFields(ev)) {
				continue
			}
			matched++
			if len(matches) < 100 {
				matches = append(matches, blueRuleMatch{
					EventID: ev.ID, Source: ev.Source, EventType: ev.EventType,
					Message: ev.Message, OccurredAt: ev.OccurredAt,
				})
			}
			if persist {
				alert, err := pg.UpsertBlueAlert(r.Context(), blueAlertFromMatch(*rule, ev))
				if err != nil {
					blueInternal(w, err)
					return
				}
				if alert.Created {
					created++
				}
			}
		}
		if len(events) < pageSize {
			break
		}
	}
	result := map[string]any{"matched_count": matched, "scanned_count": scanned, "matches": matches}
	if persist {
		result["created_alerts"] = created
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) blueTestRule(w http.ResponseWriter, r *http.Request) {
	s.blueEvaluateRule(w, r, false)
}

func (s *Server) blueRunRule(w http.ResponseWriter, r *http.Request) {
	s.blueEvaluateRule(w, r, true)
}

func (s *Server) blueListAlerts(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	limit, offset, ok := blueLimitOffset(w, r)
	if !ok {
		return
	}
	caseID, ok := blueQueryID(w, r, "case_id")
	if !ok {
		return
	}
	status, severity := r.URL.Query().Get("status"), r.URL.Query().Get("severity")
	if status != "" && !validBlueAlertStatus(status) {
		writeErr(w, http.StatusBadRequest, "invalid alert status")
		return
	}
	if severity != "" && !validBlueSeverity(severity) {
		writeErr(w, http.StatusBadRequest, "invalid alert severity")
		return
	}
	items, err := pg.ListBlueAlerts(r.Context(), caseID, status, severity, limit, offset)
	if err != nil {
		blueInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) blueGetAlert(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id, ok := blueID(w, r)
	if !ok {
		return
	}
	item, err := pg.GetBlueAlert(r.Context(), id)
	if err != nil {
		blueInternal(w, err)
		return
	}
	if item == nil {
		writeErr(w, http.StatusNotFound, "alert not found")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) bluePatchAlert(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id, ok := blueID(w, r)
	if !ok {
		return
	}
	var req struct {
		CaseID json.RawMessage `json:"case_id"`
		Status *string         `json:"status"`
		Reason *string         `json:"reason"`
	}
	if !decodeBlueJSON(w, r, &req) {
		return
	}
	patch := db.BlueAlertPatch{Status: req.Status, Reason: req.Reason}
	if len(req.CaseID) > 0 {
		if bytes.Equal(bytes.TrimSpace(req.CaseID), []byte("null")) {
			patch.ClearCase = true
		} else {
			var id int64
			if err := json.Unmarshal(req.CaseID, &id); err != nil || id <= 0 {
				writeErr(w, http.StatusBadRequest, "case_id must be a positive integer or null")
				return
			}
			patch.CaseID = &id
		}
	}
	if patch.Status != nil && !validBlueAlertStatus(*patch.Status) {
		writeErr(w, http.StatusBadRequest, "invalid alert status")
		return
	}
	if patch.Reason != nil && len(*patch.Reason) > 20000 {
		writeErr(w, http.StatusBadRequest, "alert reason is too long")
		return
	}
	if patch.CaseID != nil {
		item, err := pg.GetBlueCase(r.Context(), *patch.CaseID)
		if err != nil {
			blueInternal(w, err)
			return
		}
		if item == nil {
			writeErr(w, http.StatusBadRequest, "case_id does not exist")
			return
		}
	}
	item, err := pg.PatchBlueAlert(r.Context(), id, patch)
	if blueNotFound(w, err, "alert") {
		return
	}
	if errors.Is(err, db.ErrBlueConflict) {
		writeErr(w, http.StatusConflict, "alert or source event is already linked to another case or cited by an observation")
		return
	}
	if err != nil {
		blueInternal(w, err)
		return
	}
	if item == nil {
		writeErr(w, http.StatusNotFound, "alert not found")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) blueListObservations(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id, ok := blueID(w, r)
	if !ok {
		return
	}
	caseItem, err := pg.GetBlueCase(r.Context(), id)
	if err != nil {
		blueInternal(w, err)
		return
	}
	if caseItem == nil {
		writeErr(w, http.StatusNotFound, "case not found")
		return
	}
	limit, offset, ok := blueLimitOffset(w, r)
	if !ok {
		return
	}
	items, err := pg.ListBlueObservations(r.Context(), id, limit, offset)
	if err != nil {
		blueInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) blueCreateObservation(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id, ok := blueID(w, r)
	if !ok {
		return
	}
	caseItem, err := pg.GetBlueCase(r.Context(), id)
	if err != nil {
		blueInternal(w, err)
		return
	}
	if caseItem == nil {
		writeErr(w, http.StatusNotFound, "case not found")
		return
	}
	var req struct {
		Kind             string  `json:"kind"`
		Summary          string  `json:"summary"`
		Detail           string  `json:"detail"`
		EventID          *int64  `json:"event_id"`
		AlertID          *int64  `json:"alert_id"`
		EvidenceEventIDs []int64 `json:"evidence_event_ids"`
		EvidenceAlertIDs []int64 `json:"evidence_alert_ids"`
	}
	if !decodeBlueJSON(w, r, &req) {
		return
	}
	req.Summary = strings.TrimSpace(req.Summary)
	if !validBlueObservationKind(req.Kind) || req.Summary == "" || len(req.Summary) > 1000 || len(req.Detail) > 20000 {
		writeErr(w, http.StatusBadRequest, "invalid observation kind, summary or detail")
		return
	}
	if len(req.EvidenceEventIDs) > 100 || len(req.EvidenceAlertIDs) > 100 {
		writeErr(w, http.StatusBadRequest, "too many evidence references")
		return
	}
	item, err := pg.CreateBlueObservation(r.Context(), db.BlueObservation{
		CaseID: id, EventID: req.EventID, AlertID: req.AlertID, Kind: req.Kind,
		Summary: req.Summary, Detail: req.Detail, Author: "analyst",
		EvidenceEventIDs: req.EvidenceEventIDs, EvidenceAlertIDs: req.EvidenceAlertIDs,
	})
	if blueNotFound(w, err, "evidence") {
		return
	}
	if errors.Is(err, db.ErrBlueInvalidEvidence) {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		blueInternal(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) blueListActions(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	limit, offset, ok := blueLimitOffset(w, r)
	if !ok {
		return
	}
	caseID, ok := blueQueryID(w, r, "case_id")
	if !ok {
		return
	}
	status := r.URL.Query().Get("status")
	if status != "" && !validBlueActionStatus(status) {
		writeErr(w, http.StatusBadRequest, "invalid action status")
		return
	}
	items, err := pg.ListBlueActions(r.Context(), caseID, status, limit, offset)
	if err != nil {
		blueInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func validBlueActionStatus(v string) bool {
	switch v {
	case "pending", "approved", "rejected", "completed", "failed":
		return true
	}
	return false
}

func (s *Server) blueListCaseActions(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id, ok := blueID(w, r)
	if !ok {
		return
	}
	caseItem, err := pg.GetBlueCase(r.Context(), id)
	if err != nil {
		blueInternal(w, err)
		return
	}
	if caseItem == nil {
		writeErr(w, http.StatusNotFound, "case not found")
		return
	}
	limit, offset, ok := blueLimitOffset(w, r)
	if !ok {
		return
	}
	items, err := pg.ListBlueActions(r.Context(), &id, "", limit, offset)
	if err != nil {
		blueInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) blueCreateAction(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id, ok := blueID(w, r)
	if !ok {
		return
	}
	caseItem, err := pg.GetBlueCase(r.Context(), id)
	if err != nil {
		blueInternal(w, err)
		return
	}
	if caseItem == nil {
		writeErr(w, http.StatusNotFound, "case not found")
		return
	}
	var req struct {
		Kind      string          `json:"kind"`
		Target    string          `json:"target"`
		Rationale string          `json:"rationale"`
		Params    json.RawMessage `json:"params"`
	}
	if !decodeBlueJSON(w, r, &req) {
		return
	}
	req.Kind, req.Target, req.Rationale = strings.TrimSpace(req.Kind), strings.TrimSpace(req.Target), strings.TrimSpace(req.Rationale)
	if req.Kind == "" || req.Target == "" || req.Rationale == "" || len(req.Kind) > 100 || len(req.Target) > 500 || len(req.Rationale) > 10000 {
		writeErr(w, http.StatusBadRequest, "kind, target and rationale are required")
		return
	}
	if len(req.Params) == 0 {
		req.Params = json.RawMessage(`{}`)
	}
	if !json.Valid(req.Params) {
		writeErr(w, http.StatusBadRequest, "invalid action params")
		return
	}
	item, err := pg.CreateBlueAction(r.Context(), db.BlueAction{
		CaseID: id, Kind: req.Kind, Target: req.Target, Rationale: req.Rationale,
		Params: req.Params, Status: "pending", RequestedBy: "admin",
	})
	if err != nil {
		blueInternal(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) blueDecideAction(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id, ok := blueID(w, r)
	if !ok {
		return
	}
	var req struct {
		Approved *bool  `json:"approved"`
		Note     string `json:"note"`
	}
	if !decodeBlueJSON(w, r, &req) {
		return
	}
	if req.Approved == nil || len(req.Note) > 10000 {
		writeErr(w, http.StatusBadRequest, "approved is required and note must be at most 10000 bytes")
		return
	}
	item, err := pg.DecideBlueAction(r.Context(), id, *req.Approved, req.Note, "admin")
	if blueNotFound(w, err, "action") {
		return
	}
	if err != nil {
		if errors.Is(err, db.ErrBlueConflict) {
			writeErr(w, http.StatusConflict, "action has already been decided")
		} else {
			blueInternal(w, err)
		}
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) blueCompleteAction(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id, ok := blueID(w, r)
	if !ok {
		return
	}
	var req struct {
		Success  *bool  `json:"success"`
		Note     string `json:"note"`
		Evidence string `json:"evidence"`
	}
	if !decodeBlueJSON(w, r, &req) {
		return
	}
	if req.Success == nil || strings.TrimSpace(req.Note) == "" || len(req.Note) > 10000 || len(req.Evidence) > 20000 {
		writeErr(w, http.StatusBadRequest, "success and completion note are required")
		return
	}
	item, err := pg.CompleteBlueAction(r.Context(), id, *req.Success, req.Note, req.Evidence, "admin")
	if blueNotFound(w, err, "action") {
		return
	}
	if err != nil {
		if errors.Is(err, db.ErrBlueConflict) {
			writeErr(w, http.StatusConflict, "action must be approved before completion")
		} else {
			blueInternal(w, err)
		}
		return
	}
	writeJSON(w, http.StatusOK, item)
}

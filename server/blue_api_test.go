package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/db"
)

func blueAPIRequest(t *testing.T, h http.Handler, token, method, path string, body any, want int, out any) {
	t.Helper()
	var encoded []byte
	if body != nil {
		var err error
		encoded, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(encoded))
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != want {
		t.Fatalf("%s %s: status %d, want %d; body: %s", method, path, w.Code, want, w.Body.String())
	}
	if out != nil {
		if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
			t.Fatalf("%s %s: decode response: %v; body: %s", method, path, err, w.Body.String())
		}
	}
}

// The real HTTP mux must reject unauthenticated reads and mutations before any
// blue-team handler touches the database. This check runs without PostgreSQL.
func TestBlueAPIRequiresJWT(t *testing.T) {
	s := &Server{m: &Manager{}, jwtKey: []byte(strings.Repeat("b", 32))}
	h := s.Handler()
	for _, tc := range []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodGet, "/api/blue/overview", nil},
		{http.MethodGet, "/api/blue/cases", nil},
		{http.MethodGet, "/api/blue/events", nil},
		{http.MethodGet, "/api/blue/rules", nil},
		{http.MethodGet, "/api/blue/alerts", nil},
		{http.MethodGet, "/api/blue/actions", nil},
		{http.MethodPost, "/api/blue/events/import", map[string]any{"events": []any{}}},
		{http.MethodPost, "/api/blue/cases", map[string]any{"title": "unauthorized"}},
		{http.MethodPost, "/api/blue/actions/1/decision", map[string]any{"approved": true}},
	} {
		blueAPIRequest(t, h, "", tc.method, tc.path, tc.body, http.StatusUnauthorized, nil)
	}
	blueAPIRequest(t, h, "invalid.jwt.token", http.MethodGet, "/api/blue/overview", nil, http.StatusUnauthorized, nil)
}

// ARTEX_BLUE_TEST_PG_DSN must name a disposable PostgreSQL database. We require
// this explicit opt-in instead of falling back to config.json or ARTEX_PG_DSN,
// which may point at a user's running ARTEX instance. For example, set it to a
// dedicated artex_blue_test database in a temporary PostgreSQL container.
func TestBlueAPIInvestigationWorkflowPG(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ARTEX_BLUE_TEST_PG_DSN"))
	if dsn == "" {
		t.Skip("set ARTEX_BLUE_TEST_PG_DSN to a disposable PostgreSQL test database")
	}
	pg, err := db.Open(dsn)
	if err != nil {
		t.Fatalf("open blue-team test database: %v", err)
	}
	t.Cleanup(func() { _ = pg.Close() })

	// Keep the API under its production route registration and JWT middleware.
	s := &Server{m: &Manager{pg: pg}, jwtKey: []byte(strings.Repeat("i", 32))}
	h := s.Handler()
	token, err := signJWT(s.signingKey(s.readPassHash()))
	if err != nil {
		t.Fatal(err)
	}
	blueAPIRequest(t, h, "", http.MethodPost, "/api/blue/cases", map[string]any{"title": "blocked"}, http.StatusUnauthorized, nil)
	// The second JSON decode must also report MaxBytesError as 413. The first
	// object is valid; the oversized trailing whitespace crosses the body cap.
	oversized := httptest.NewRequest(http.MethodPost, "/api/blue/cases",
		strings.NewReader(`{"title":"small"}`+strings.Repeat(" ", maxBlueBodyBytes)))
	oversized.Header.Set("Content-Type", "application/json")
	oversized.Header.Set("Authorization", "Bearer "+token)
	oversizedResult := httptest.NewRecorder()
	h.ServeHTTP(oversizedResult, oversized)
	if oversizedResult.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized JSON: status %d, want 413; body: %s", oversizedResult.Code, oversizedResult.Body.String())
	}

	fixture := "blue-e2e-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	eventType := fixture + ".auth_failure"
	source := fixture + ".sensor"
	var caseID, otherCaseID, ruleID, sigmaRuleID int64
	var eventIDs []int64
	t.Cleanup(func() {
		ctx := context.Background()
		if caseID != 0 {
			if _, err := pg.ExecContext(ctx, `DELETE FROM blue_cases WHERE id=$1`, caseID); err != nil {
				t.Errorf("remove blue case fixture: %v", err)
			}
		}
		if otherCaseID != 0 {
			if _, err := pg.ExecContext(ctx, `DELETE FROM blue_cases WHERE id=$1`, otherCaseID); err != nil {
				t.Errorf("remove second blue case fixture: %v", err)
			}
		}
		if ruleID != 0 {
			if _, err := pg.ExecContext(ctx, `DELETE FROM blue_rules WHERE id=$1`, ruleID); err != nil {
				t.Errorf("remove blue rule fixture: %v", err)
			}
		}
		if sigmaRuleID != 0 {
			if _, err := pg.ExecContext(ctx, `DELETE FROM blue_rules WHERE id=$1`, sigmaRuleID); err != nil {
				t.Errorf("remove Sigma rule fixture: %v", err)
			}
		}
		for _, id := range eventIDs {
			if _, err := pg.ExecContext(ctx, `DELETE FROM blue_events WHERE id=$1`, id); err != nil {
				t.Errorf("remove blue event fixture %d: %v", id, err)
			}
		}
	})

	var rule db.BlueRule
	blueAPIRequest(t, h, token, http.MethodPost, "/api/blue/rules", map[string]any{
		"name": fixture, "field": "event_type", "operator": "equals", "value": eventType,
		"severity": "high", "enabled": true,
	}, http.StatusCreated, &rule)
	if rule.ID <= 0 || !rule.Enabled {
		t.Fatalf("created rule lacks ID or is disabled: %+v", rule)
	}
	ruleID = rule.ID

	importBody := map[string]any{"events": []map[string]any{
		{"source": source, "external_id": "match", "event_type": eventType,
			"severity": "medium", "asset": "host-17", "message": "Repeated login failures",
			"raw": map[string]any{"user": "analyst-test"}},
		{"source": source, "external_id": "nonmatch", "event_type": fixture + ".normal",
			"severity": "info", "asset": "host-17", "message": "Normal login"},
	}}
	var imported struct {
		Imported      int            `json:"imported"`
		Events        []db.BlueEvent `json:"events"`
		AlertsMatched int            `json:"alerts_matched"`
	}
	blueAPIRequest(t, h, token, http.MethodPost, "/api/blue/events/import", importBody, http.StatusCreated, &imported)
	if imported.Imported != 2 || len(imported.Events) != 2 || imported.AlertsMatched != 1 {
		t.Fatalf("import should create one alert from two events: %+v", imported)
	}
	matchID, otherID := imported.Events[0].ID, imported.Events[1].ID
	if matchID <= 0 || otherID <= 0 || matchID == otherID {
		t.Fatalf("import returned invalid event IDs: %+v", imported.Events)
	}
	eventIDs = []int64{matchID, otherID}

	var testRun struct {
		MatchedCount int `json:"matched_count"`
		ScannedCount int `json:"scanned_count"`
	}
	rulePath := fmt.Sprintf("/api/blue/rules/%d", ruleID)
	blueAPIRequest(t, h, token, http.MethodPost, rulePath+"/test", map[string]int{"limit": 100}, http.StatusOK, &testRun)
	if testRun.MatchedCount != 1 || testRun.ScannedCount < 2 {
		t.Fatalf("rule dry run should match only imported failure: %+v", testRun)
	}
	var rerun struct {
		MatchedCount  int `json:"matched_count"`
		CreatedAlerts int `json:"created_alerts"`
	}
	blueAPIRequest(t, h, token, http.MethodPost, rulePath+"/run", map[string]int{"limit": 100}, http.StatusOK, &rerun)
	if rerun.MatchedCount != 1 || rerun.CreatedAlerts != 0 {
		t.Fatalf("rerunning rule must not duplicate the alert: %+v", rerun)
	}
	blueAPIRequest(t, h, token, http.MethodPatch, rulePath,
		map[string]any{"enabled": false}, http.StatusOK, &rule)
	blueAPIRequest(t, h, token, http.MethodPost, rulePath+"/run",
		map[string]int{"limit": 100}, http.StatusConflict, nil)
	blueAPIRequest(t, h, token, http.MethodPost, rulePath+"/test",
		map[string]int{"limit": 100}, http.StatusOK, &testRun)
	if testRun.MatchedCount != 1 {
		t.Fatalf("disabled rule should remain available for a dry run: %+v", testRun)
	}

	// Sigma YAML can be created and replaced through the API. JSON null clears
	// the advanced definition and restores this rule's simple field comparison.
	sigma := "title: Blue API test\ndetection:\n  selection:\n    event_type: " + eventType + "\n  condition: selection\n"
	var sigmaRule db.BlueRule
	blueAPIRequest(t, h, token, http.MethodPost, "/api/blue/rules",
		map[string]any{"name": fixture + "-sigma", "sigma_yaml": sigma, "enabled": false},
		http.StatusCreated, &sigmaRule)
	sigmaRuleID = sigmaRule.ID
	if sigmaRuleID <= 0 || len(sigmaRule.Definition) == 0 {
		t.Fatalf("Sigma rule definition was not stored: %+v", sigmaRule)
	}
	sigmaPath := fmt.Sprintf("/api/blue/rules/%d", sigmaRuleID)
	blueAPIRequest(t, h, token, http.MethodPost, sigmaPath+"/test",
		map[string]int{"limit": 100}, http.StatusOK, &testRun)
	if testRun.MatchedCount != 1 {
		t.Fatalf("created Sigma rule did not match imported event: %+v", testRun)
	}
	sigma = "title: Blue API test update\ndetection:\n  selection:\n    message: Repeated login failures\n  condition: selection\n"
	blueAPIRequest(t, h, token, http.MethodPatch, sigmaPath,
		map[string]any{"sigma_yaml": sigma}, http.StatusOK, &sigmaRule)
	blueAPIRequest(t, h, token, http.MethodPost, sigmaPath+"/test",
		map[string]int{"limit": 100}, http.StatusOK, &testRun)
	if testRun.MatchedCount != 1 {
		t.Fatalf("updated Sigma rule did not match imported event: %+v", testRun)
	}
	blueAPIRequest(t, h, token, http.MethodPatch, sigmaPath,
		map[string]any{"definition": nil, "field": "event_type", "operator": "equals", "value": eventType},
		http.StatusOK, &sigmaRule)
	blueAPIRequest(t, h, token, http.MethodPost, sigmaPath+"/test",
		map[string]int{"limit": 100}, http.StatusOK, &testRun)
	if testRun.MatchedCount != 1 {
		t.Fatalf("clearing advanced definition did not restore simple rule: %+v", testRun)
	}

	var alerts []db.BlueAlert
	blueAPIRequest(t, h, token, http.MethodGet, "/api/blue/alerts?limit=1000", nil, http.StatusOK, &alerts)
	var alert db.BlueAlert
	for _, candidate := range alerts {
		if candidate.RuleID == ruleID && candidate.EventID == matchID {
			alert = candidate
		}
		if candidate.RuleID == ruleID && candidate.EventID == otherID {
			t.Fatalf("nonmatching event produced an alert: %+v", candidate)
		}
	}
	if alert.ID <= 0 || alert.Status != "new" || alert.Severity != "high" {
		t.Fatalf("matching alert missing or malformed: %+v", alert)
	}

	var investigation db.BlueCase
	blueAPIRequest(t, h, token, http.MethodPost, "/api/blue/cases", map[string]any{
		"title": "Investigate " + fixture, "severity": "high", "description": "Identity telemetry",
	}, http.StatusCreated, &investigation)
	if investigation.ID <= 0 || investigation.Status != "open" {
		t.Fatalf("created case malformed: %+v", investigation)
	}
	caseID = investigation.ID
	alertPath := fmt.Sprintf("/api/blue/alerts/%d", alert.ID)
	blueAPIRequest(t, h, token, http.MethodPatch, alertPath,
		map[string]any{"case_id": caseID, "status": "investigating"}, http.StatusOK, &alert)
	if alert.CaseID == nil || *alert.CaseID != caseID || alert.Status != "investigating" {
		t.Fatalf("alert triage did not attach case: %+v", alert)
	}
	var attachedEvent db.BlueEvent
	blueAPIRequest(t, h, token, http.MethodGet, fmt.Sprintf("/api/blue/events/%d", matchID), nil, http.StatusOK, &attachedEvent)
	if attachedEvent.CaseID == nil || *attachedEvent.CaseID != caseID {
		t.Fatalf("source event was not attached with alert: %+v", attachedEvent)
	}
	var secondCase db.BlueCase
	blueAPIRequest(t, h, token, http.MethodPost, "/api/blue/cases",
		map[string]any{"title": "Alternate investigation " + fixture}, http.StatusCreated, &secondCase)
	otherCaseID = secondCase.ID
	blueAPIRequest(t, h, token, http.MethodPatch, alertPath,
		map[string]any{"case_id": otherCaseID}, http.StatusConflict, nil)
	movePath := fmt.Sprintf("/api/blue/events/%d/move-case", matchID)
	blueAPIRequest(t, h, token, http.MethodPost, movePath,
		map[string]any{"case_id": otherCaseID}, http.StatusOK, &attachedEvent)
	blueAPIRequest(t, h, token, http.MethodGet, alertPath, nil, http.StatusOK, &alert)
	if attachedEvent.CaseID == nil || *attachedEvent.CaseID != otherCaseID ||
		alert.CaseID == nil || *alert.CaseID != otherCaseID {
		t.Fatalf("explicit event reassignment did not move its alert: event=%+v alert=%+v", attachedEvent, alert)
	}
	blueAPIRequest(t, h, token, http.MethodPost, movePath,
		map[string]any{"case_id": caseID}, http.StatusOK, &attachedEvent)
	blueAPIRequest(t, h, token, http.MethodGet, alertPath, nil, http.StatusOK, &alert)
	if attachedEvent.CaseID == nil || *attachedEvent.CaseID != caseID ||
		alert.CaseID == nil || *alert.CaseID != caseID {
		t.Fatalf("return move did not restore event and alert case: event=%+v alert=%+v", attachedEvent, alert)
	}

	casePath := fmt.Sprintf("/api/blue/cases/%d", caseID)
	blueAPIRequest(t, h, token, http.MethodPost, casePath+"/observations", map[string]any{
		"kind": "fact", "summary": "Failure pattern confirmed", "event_id": otherID,
	}, http.StatusBadRequest, nil)
	var observation db.BlueObservation
	blueAPIRequest(t, h, token, http.MethodPost, casePath+"/observations", map[string]any{
		"kind": "fact", "summary": "Failure pattern confirmed", "detail": "Rule and source event agree.",
		"event_id": matchID, "alert_id": alert.ID,
		"evidence_event_ids": []int64{matchID}, "evidence_alert_ids": []int64{alert.ID},
	}, http.StatusCreated, &observation)
	if observation.ID <= 0 || observation.CaseID != caseID ||
		len(observation.EvidenceEventIDs) != 1 || observation.EvidenceEventIDs[0] != matchID ||
		len(observation.EvidenceAlertIDs) != 1 || observation.EvidenceAlertIDs[0] != alert.ID {
		t.Fatalf("observation lost or duplicated evidence links: %+v", observation)
	}
	var observations []db.BlueObservation
	blueAPIRequest(t, h, token, http.MethodGet, casePath+"/observations", nil, http.StatusOK, &observations)
	if len(observations) != 1 || observations[0].ID != observation.ID {
		t.Fatalf("case observation timeline missing fact: %+v", observations)
	}
	blueAPIRequest(t, h, token, http.MethodPatch, alertPath,
		map[string]any{"case_id": nil}, http.StatusConflict, nil)
	blueAPIRequest(t, h, token, http.MethodPost, movePath,
		map[string]any{"case_id": otherCaseID}, http.StatusConflict, nil)

	var action db.BlueAction
	blueAPIRequest(t, h, token, http.MethodPost, casePath+"/actions", map[string]any{
		"kind": "isolate_host", "target": "host-17", "rationale": "Contain repeated login failures",
		"params": map[string]any{"ticket": fixture},
	}, http.StatusCreated, &action)
	if action.ID <= 0 || action.Status != "pending" {
		t.Fatalf("proposed action malformed: %+v", action)
	}
	actionPath := fmt.Sprintf("/api/blue/actions/%d", action.ID)
	completion := map[string]any{"success": true, "note": "Operator isolated host", "evidence": "change-42"}
	blueAPIRequest(t, h, token, http.MethodPost, actionPath+"/complete", completion, http.StatusConflict, nil)
	blueAPIRequest(t, h, token, http.MethodPost, actionPath+"/decision",
		map[string]any{"approved": true, "note": "Approved by incident lead"}, http.StatusOK, &action)
	if action.Status != "approved" || action.DecidedAt == nil {
		t.Fatalf("approval was not recorded: %+v", action)
	}
	blueAPIRequest(t, h, token, http.MethodPost, actionPath+"/complete", completion, http.StatusOK, &action)
	if action.Status != "completed" || action.CompletedAt == nil || action.CompletionEvidence != "change-42" {
		t.Fatalf("operator-confirmed completion was not recorded: %+v", action)
	}
	blueAPIRequest(t, h, token, http.MethodPost, actionPath+"/decision",
		map[string]any{"approved": false}, http.StatusConflict, nil)

	blueAPIRequest(t, h, token, http.MethodPatch, casePath,
		map[string]any{"status": "contained", "summary": "Host isolated; monitor authentication"}, http.StatusOK, &investigation)
	if investigation.Status != "contained" {
		t.Fatalf("case was not advanced to contained: %+v", investigation)
	}
	var caseActions []db.BlueAction
	blueAPIRequest(t, h, token, http.MethodGet, casePath+"/actions", nil, http.StatusOK, &caseActions)
	if len(caseActions) != 1 || caseActions[0].ID != action.ID || caseActions[0].Status != "completed" {
		t.Fatalf("case action timeline does not show completion: %+v", caseActions)
	}
}

func TestBlueAPIRedFindingHandoffAndConversationIsolationPG(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ARTEX_BLUE_TEST_PG_DSN"))
	if dsn == "" {
		t.Skip("set ARTEX_BLUE_TEST_PG_DSN to a disposable PostgreSQL test database")
	}
	pg, err := db.Open(dsn)
	if err != nil {
		t.Fatalf("open blue-team test database: %v", err)
	}
	t.Cleanup(func() { _ = pg.Close() })
	s := &Server{m: &Manager{pg: pg}, jwtKey: []byte(strings.Repeat("h", 32))}
	h := s.Handler()
	token, err := signJWT(s.signingKey(s.readPassHash()))
	if err != nil {
		t.Fatal(err)
	}

	fixture := "blue-handoff-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	var findingID, caseID, forgedConversationID int64
	t.Cleanup(func() {
		if forgedConversationID != 0 {
			if err := pg.DeleteConversation(forgedConversationID); err != nil {
				t.Errorf("remove forged conversation fixture: %v", err)
			}
		}
		if caseID != 0 {
			if _, err := pg.Exec(`DELETE FROM blue_cases WHERE id=$1`, caseID); err != nil {
				t.Errorf("remove finding-linked case fixture: %v", err)
			}
		}
		if findingID != 0 {
			if _, err := pg.Exec(`DELETE FROM findings WHERE id=$1`, findingID); err != nil {
				t.Errorf("remove red finding fixture: %v", err)
			}
		}
	})

	findingID, err = pg.AddFinding(0, 0, fixture, "Finding "+fixture,
		db.SeverityHigh, "Confirmed test finding", "local test proof", "test", nil)
	if err != nil {
		t.Fatalf("seed red finding: %v", err)
	}
	handoffBody := map[string]any{"finding_id": findingID}
	blueAPIRequest(t, h, token, http.MethodPost, "/api/blue/cases/from-finding",
		handoffBody, http.StatusConflict, nil)
	if affected, err := pg.SetFindingStatus(findingID, db.FindingConfirmed); err != nil || affected != 1 {
		t.Fatalf("confirm red finding: affected=%d err=%v", affected, err)
	}
	var linked db.BlueCase
	blueAPIRequest(t, h, token, http.MethodPost, "/api/blue/cases/from-finding",
		handoffBody, http.StatusCreated, &linked)
	if linked.ID <= 0 || linked.RedFindingID == nil || *linked.RedFindingID != findingID {
		t.Fatalf("red finding handoff failed: %+v", linked)
	}
	caseID = linked.ID
	casePath := fmt.Sprintf("/api/blue/cases/%d", caseID)
	blueAPIRequest(t, h, token, http.MethodPatch, casePath,
		map[string]any{"summary": "Analyst's existing plan"}, http.StatusOK, &linked)
	var replay db.BlueCase
	blueAPIRequest(t, h, token, http.MethodPost, "/api/blue/cases/from-finding",
		handoffBody, http.StatusCreated, &replay)
	if replay.ID != caseID || replay.Summary != "Analyst's existing plan" {
		t.Fatalf("replayed handoff duplicated or overwrote case: first=%+v replay=%+v", linked, replay)
	}

	// The Blue analyst is only run by the case analysis workflow. A caller must
	// not create a generic red-team conversation with this role, nor send a turn
	// to a legacy/forged conversation that already names it.
	blueAPIRequest(t, h, token, http.MethodPost, "/api/conversations",
		map[string]any{"agent_key": "blue_analyst", "title": fixture}, http.StatusForbidden, nil)
	forged, err := pg.CreateConversation("blue_analyst", fixture, nil)
	if err != nil {
		t.Fatalf("seed forged blue analyst conversation: %v", err)
	}
	forgedConversationID = forged.ID
	blueAPIRequest(t, h, token, http.MethodPost,
		fmt.Sprintf("/api/conversations/%d/messages", forgedConversationID),
		map[string]any{"message": "Bypass case analysis"}, http.StatusForbidden, nil)
	var recorded int
	if err := pg.QueryRow(`SELECT count(*) FROM conversation_activities WHERE conversation_id=$1`,
		forgedConversationID).Scan(&recorded); err != nil {
		t.Fatal(err)
	}
	if recorded != 0 {
		t.Fatalf("forged conversation persisted %d blocked message(s)", recorded)
	}
}

func TestBlueAnalystConfigAcceptsOnlyModelBindingPG(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ARTEX_BLUE_TEST_PG_DSN"))
	if dsn == "" {
		t.Skip("set ARTEX_BLUE_TEST_PG_DSN to a disposable PostgreSQL test database")
	}
	pg, err := db.Open(dsn)
	if err != nil {
		t.Fatalf("open blue-team test database: %v", err)
	}
	t.Cleanup(func() { _ = pg.Close() })
	s := &Server{m: &Manager{pg: pg}, jwtKey: []byte(strings.Repeat("c", 32))}
	h := s.Handler()
	token, err := signJWT(s.signingKey(s.readPassHash()))
	if err != nil {
		t.Fatal(err)
	}
	before, err := pg.GetAgentByKey("blue_analyst")
	if err != nil || before == nil {
		t.Fatalf("load seeded blue analyst: agent=%+v err=%v", before, err)
	}
	t.Cleanup(func() {
		if err := pg.SetAgentLLMProfile("blue_analyst", before.LLMProfileID); err != nil {
			t.Errorf("restore blue analyst model binding: %v", err)
		}
	})
	path := "/api/agents/blue_analyst/config"
	for _, tc := range []struct {
		name string
		body map[string]any
	}{
		{"max turns", map[string]any{"max_turns": 0}},
		{"run time", map[string]any{"run_seconds": 0}},
		{"web search", map[string]any{"web_search": false}},
		{"interactive shell", map[string]any{"interactive_shell": false}},
		{"trigger run mode", map[string]any{"trigger_run_mode": "serial"}},
		{"trigger merge mode", map[string]any{"trigger_merge_mode": "all"}},
		{"trigger parallelism", map[string]any{"trigger_max_parallel": 0}},
		{"model plus shell", map[string]any{"llm_profile_id": nil, "interactive_shell": true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			blueAPIRequest(t, h, token, http.MethodPut, path, tc.body, http.StatusBadRequest, nil)
			after, err := pg.GetAgentByKey("blue_analyst")
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("forbidden agent config changed persisted state: before=%+v after=%+v", before, after)
			}
		})
	}

	blueAPIRequest(t, h, token, http.MethodPut, path,
		map[string]any{"llm_profile_id": nil}, http.StatusOK, nil)
	after, err := pg.GetAgentByKey("blue_analyst")
	if err != nil {
		t.Fatal(err)
	}
	if after.LLMProfileID != nil {
		t.Fatalf("profile-only request did not clear model binding: %+v", after)
	}
	beforeWithoutBinding, afterWithoutBinding := *before, *after
	beforeWithoutBinding.LLMProfileID, afterWithoutBinding.LLMProfileID = nil, nil
	if !reflect.DeepEqual(beforeWithoutBinding, afterWithoutBinding) {
		t.Fatalf("profile-only request changed execution settings: before=%+v after=%+v", before, after)
	}
}

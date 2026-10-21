package db

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestBlueCaseEvidenceAndResponseWorkflow(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	ctx := context.Background()
	caseA, err := d.CreateBlueCase(ctx, BlueCase{Title: "blue workflow A"})
	if err != nil {
		t.Fatal(err)
	}
	caseB, err := d.CreateBlueCase(ctx, BlueCase{Title: "blue workflow B"})
	if err != nil {
		t.Fatal(err)
	}
	var eventID, ruleID, peerRuleID, findingID, linkedCaseID int64
	t.Cleanup(func() {
		_, _ = d.Exec(`DELETE FROM blue_observations WHERE case_id IN ($1,$2)`, caseA.ID, caseB.ID)
		if eventID != 0 {
			_, _ = d.Exec(`DELETE FROM blue_alerts WHERE event_id=$1`, eventID)
			_, _ = d.Exec(`DELETE FROM blue_events WHERE id=$1`, eventID)
		}
		if ruleID != 0 {
			_, _ = d.Exec(`DELETE FROM blue_rules WHERE id=$1`, ruleID)
		}
		if peerRuleID != 0 {
			_, _ = d.Exec(`DELETE FROM blue_rules WHERE id=$1`, peerRuleID)
		}
		_, _ = d.Exec(`DELETE FROM blue_cases WHERE id IN ($1,$2)`, caseA.ID, caseB.ID)
		if linkedCaseID != 0 {
			_, _ = d.Exec(`DELETE FROM blue_cases WHERE id=$1`, linkedCaseID)
		}
		if findingID != 0 {
			_, _ = d.Exec(`DELETE FROM findings WHERE id=$1`, findingID)
		}
	})
	if err := d.QueryRow(`INSERT INTO findings (name,summary,severity) VALUES ('blue handoff test','handoff','high') RETURNING id`).Scan(&findingID); err != nil {
		t.Fatal(err)
	}
	linked, err := d.CreateBlueCase(ctx, BlueCase{Title: "original finding handoff", RedFindingID: &findingID})
	if err != nil {
		t.Fatal(err)
	}
	linkedCaseID = linked.ID
	replayedCase, err := d.CreateBlueCase(ctx, BlueCase{Title: "duplicate handoff", RedFindingID: &findingID})
	if err != nil || replayedCase.ID != linkedCaseID || replayedCase.Title != linked.Title {
		t.Fatalf("idempotent finding handoff: %v, %+v", err, replayedCase)
	}

	events, err := d.ImportBlueEvents(ctx, []BlueEvent{{Source: "blue-test", ExternalID: t.Name() + time.Now().Format(time.RFC3339Nano), EventType: "auth", Message: "failed login", OccurredAt: time.Now().UTC()}})
	if err != nil || len(events) != 1 {
		t.Fatalf("import event: %v, %d results", err, len(events))
	}
	eventID = events[0].ID
	replayInput := events[0]
	replayInput.Message = "mutated replay payload"
	replayed, err := d.ImportBlueEvents(ctx, []BlueEvent{replayInput})
	if err != nil || len(replayed) != 1 || replayed[0].ID != eventID || replayed[0].Message != "failed login" {
		t.Fatalf("idempotent import: %v, %+v", err, replayed)
	}
	maxID, err := d.BlueEventHighWatermark(ctx)
	if err != nil || maxID < eventID {
		t.Fatalf("high watermark: %d, %v", maxID, err)
	}
	page, err := d.ListBlueEventsAfterID(ctx, eventID-1, eventID, 10)
	if err != nil || len(page) != 1 || page[0].ID != eventID {
		t.Fatalf("keyset page: %v, %+v", err, page)
	}

	rule, err := d.CreateBlueRule(ctx, BlueRule{Name: "failed-login", Field: "message", Operator: "contains", Value: "failed", Severity: "high", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	ruleID = rule.ID
	alert, err := d.UpsertBlueAlert(ctx, BlueAlert{RuleID: rule.ID, EventID: eventID, CaseID: &caseA.ID})
	if err != nil || !alert.Created {
		t.Fatalf("create alert: %v, %+v", err, alert)
	}
	linkedEvent, err := d.GetBlueEvent(ctx, eventID)
	if err != nil || linkedEvent.CaseID == nil || *linkedEvent.CaseID != caseA.ID {
		t.Fatalf("alert did not attach its event: %v, %+v", err, linkedEvent)
	}
	peerRule, err := d.CreateBlueRule(ctx, BlueRule{Name: "peer-rule", Field: "message", Operator: "contains", Value: "login", Severity: "medium", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	peerRuleID = peerRule.ID
	peerAlert, err := d.UpsertBlueAlert(ctx, BlueAlert{RuleID: peerRule.ID, EventID: eventID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.PatchBlueAlert(ctx, peerAlert.ID, BlueAlertPatch{ClearCase: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.MoveBlueEventCase(ctx, eventID, caseA.ID); err != nil {
		t.Fatalf("same-case move should reconnect peer alerts: %v", err)
	}
	peerAlert, err = d.GetBlueAlert(ctx, peerAlert.ID)
	if err != nil || peerAlert.CaseID == nil || *peerAlert.CaseID != caseA.ID {
		t.Fatalf("peer alert remains detached: %v, %+v", err, peerAlert)
	}
	if _, err := d.CreateBlueObservation(ctx, BlueObservation{CaseID: caseB.ID, EventID: &eventID, Summary: "wrong case"}); !errors.Is(err, ErrBlueInvalidEvidence) {
		t.Fatalf("cross-case evidence should be rejected, got %v", err)
	}
	assessment := json.RawMessage(`{"verdict":"inconclusive","confidence":"low"}`)
	obs, err := d.CreateBlueObservation(ctx, BlueObservation{CaseID: caseA.ID, EventID: &eventID, AlertID: &alert.ID, EvidenceEventIDs: []int64{eventID}, EvidenceAlertIDs: []int64{alert.ID}, Kind: "analysis", Summary: "Needs review", Assessment: assessment})
	var decodedAssessment struct {
		Verdict string `json:"verdict"`
	}
	if err == nil {
		err = json.Unmarshal(obs.Assessment, &decodedAssessment)
	}
	if err != nil || decodedAssessment.Verdict != "inconclusive" {
		t.Fatalf("create observation: %v, %+v", err, obs)
	}
	if _, err := d.MoveBlueEventCase(ctx, eventID, caseB.ID); !errors.Is(err, ErrBlueConflict) {
		t.Fatalf("moving cited evidence should conflict, got %v", err)
	}
	if _, err := d.PatchBlueAlert(ctx, alert.ID, BlueAlertPatch{ClearCase: true}); !errors.Is(err, ErrBlueConflict) {
		t.Fatalf("detaching cited alert should conflict, got %v", err)
	}
	action, err := d.CreateBlueAction(ctx, BlueAction{CaseID: caseA.ID, Kind: "contain", Target: "host-a", Rationale: "isolate after review"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.DecideBlueAction(ctx, action.ID, true, "approved for manual work", "analyst"); err != nil {
		t.Fatal(err)
	}
	completed, err := d.CompleteBlueAction(ctx, action.ID, true, "operator confirmed", "ticket 123", "operator")
	if err != nil || completed.Status != "completed" {
		t.Fatalf("complete action: %v, %+v", err, completed)
	}
	if _, err := d.CompleteBlueAction(ctx, action.ID, true, "again", "", "operator"); !errors.Is(err, ErrBlueConflict) {
		t.Fatalf("repeat completion should conflict, got %v", err)
	}
	run, err := d.CreateBlueAnalysisRun(ctx, caseA.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreateBlueAnalysisRun(ctx, caseA.ID, nil); !errors.Is(err, ErrBlueConflict) {
		t.Fatalf("overlapping analysis should conflict, got %v", err)
	}
	if _, err := d.UpdateBlueAnalysisRun(ctx, run.ID, "failed", nil, "test stopped"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreateBlueAnalysisRun(ctx, caseA.ID, nil); err != nil {
		t.Fatalf("analysis should restart after failed run: %v", err)
	}
}

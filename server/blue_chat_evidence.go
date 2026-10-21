package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Autumn-27/artex/db"
)

var errBlueChatInvalidEvidence = errors.New("selected evidence must belong to the case and fit the evidence limit")

// A chat attachment is a bounded snapshot of records already linked to one
// Blue case. The model cannot choose additional records or call a retrieval tool.
type blueChatAttachedCase struct {
	prompt   string
	metadata json.RawMessage
}

type blueChatEvidenceEvent struct {
	ID         int64     `json:"id"`
	Source     string    `json:"source"`
	EventType  string    `json:"event_type"`
	Severity   string    `json:"severity"`
	Asset      string    `json:"asset"`
	Message    string    `json:"message"`
	OccurredAt time.Time `json:"occurred_at"`
	RawPreview string    `json:"raw_preview,omitempty"`
}

type blueChatEvidenceAlert struct {
	ID       int64  `json:"id"`
	EventID  int64  `json:"event_id"`
	Title    string `json:"title"`
	Severity string `json:"severity"`
	Status   string `json:"status"`
	Reason   string `json:"reason"`
}

type blueChatEvidence struct {
	Case struct {
		ID          int64  `json:"id"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Status      string `json:"status"`
		Severity    string `json:"severity"`
		Summary     string `json:"summary"`
	} `json:"case"`
	Events           []blueChatEvidenceEvent `json:"events"`
	Alerts           []blueChatEvidenceAlert `json:"alerts"`
	SelectedEventIDs []int64                 `json:"selected_event_ids,omitempty"`
	SelectedAlertIDs []int64                 `json:"selected_alert_ids,omitempty"`
	EventsTruncated  bool                    `json:"events_truncated"`
	AlertsTruncated  bool                    `json:"alerts_truncated"`
}

func validateBlueChatSelectedIDs(eventIDs, alertIDs []int64) error {
	for _, group := range []struct {
		name  string
		ids   []int64
		limit int
	}{{"event_ids", eventIDs, blueChatEvidenceEvents}, {"alert_ids", alertIDs, blueChatEvidenceAlerts}} {
		if len(group.ids) > group.limit {
			return fmt.Errorf("%s exceeds the selection limit: %w", group.name, errBlueChatInvalidEvidence)
		}
		seen := make(map[int64]bool, len(group.ids))
		for _, id := range group.ids {
			if id <= 0 || seen[id] {
				return fmt.Errorf("%s contains an invalid or duplicate ID: %w", group.name, errBlueChatInvalidEvidence)
			}
			seen[id] = true
		}
	}
	return nil
}

func clipBlueChatEvidence(value string, maxRunes int) string {
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes]) + "… [truncated]"
}

func loadBlueChatCase(ctx context.Context, pg *db.DB, id int64, selectedEventIDs, selectedAlertIDs []int64) (*blueChatAttachedCase, error) {
	if err := validateBlueChatSelectedIDs(selectedEventIDs, selectedAlertIDs); err != nil {
		return nil, err
	}
	item, err := pg.GetBlueCase(ctx, id)
	if err != nil || item == nil {
		return nil, err
	}
	selectedEvents := make([]db.BlueEvent, 0, len(selectedEventIDs))
	selectedAlerts := make([]db.BlueAlert, 0, len(selectedAlertIDs))
	for _, eventID := range selectedEventIDs {
		event, err := pg.GetBlueEvent(ctx, eventID)
		if err != nil {
			return nil, err
		}
		if event == nil || event.CaseID == nil || *event.CaseID != id {
			return nil, fmt.Errorf("event %d: %w", eventID, errBlueChatInvalidEvidence)
		}
		selectedEvents = append(selectedEvents, *event)
	}
	for _, alertID := range selectedAlertIDs {
		alert, err := pg.GetBlueAlert(ctx, alertID)
		if err != nil {
			return nil, err
		}
		if alert == nil || alert.CaseID == nil || *alert.CaseID != id {
			return nil, fmt.Errorf("alert %d: %w", alertID, errBlueChatInvalidEvidence)
		}
		selectedAlerts = append(selectedAlerts, *alert)
	}
	events, err := pg.ListBlueEvents(ctx, &id, "", blueChatEvidenceEvents+1, 0)
	if err != nil {
		return nil, err
	}
	alerts, err := pg.ListBlueAlerts(ctx, &id, "", "", blueChatEvidenceAlerts+1, 0)
	if err != nil {
		return nil, err
	}
	evidence := blueChatEvidence{
		Events:           make([]blueChatEvidenceEvent, 0, blueChatEvidenceEvents),
		Alerts:           make([]blueChatEvidenceAlert, 0, blueChatEvidenceAlerts),
		SelectedEventIDs: append([]int64(nil), selectedEventIDs...),
		SelectedAlertIDs: append([]int64(nil), selectedAlertIDs...),
		EventsTruncated:  len(events) > blueChatEvidenceEvents,
		AlertsTruncated:  len(alerts) > blueChatEvidenceAlerts,
	}
	evidence.Case.ID = item.ID
	evidence.Case.Title = clipBlueChatEvidence(item.Title, 250)
	evidence.Case.Description = clipBlueChatEvidence(item.Description, 700)
	evidence.Case.Status = item.Status
	evidence.Case.Severity = item.Severity
	evidence.Case.Summary = clipBlueChatEvidence(item.Summary, 700)
	selectedEventSet := make(map[int64]bool, len(selectedEvents))
	for _, event := range selectedEvents {
		selectedEventSet[event.ID] = true
	}
	for _, event := range events {
		if len(selectedEvents) == blueChatEvidenceEvents {
			break
		}
		if !selectedEventSet[event.ID] {
			selectedEvents = append(selectedEvents, event)
			selectedEventSet[event.ID] = true
		}
	}
	if len(selectedEvents) > blueChatEvidenceEvents {
		selectedEvents = selectedEvents[:blueChatEvidenceEvents]
		evidence.EventsTruncated = true
	}
	if len(events) > len(selectedEvents) {
		evidence.EventsTruncated = true
	}
	for _, event := range selectedEvents {
		evidence.Events = append(evidence.Events, blueChatEvidenceEvent{
			ID: event.ID, Source: clipBlueChatEvidence(event.Source, 120),
			EventType: clipBlueChatEvidence(event.EventType, 120), Severity: event.Severity,
			Asset: clipBlueChatEvidence(event.Asset, 120), Message: clipBlueChatEvidence(event.Message, 450),
			OccurredAt: event.OccurredAt, RawPreview: clipBlueChatEvidence(string(event.Raw), 450),
		})
	}
	selectedAlertSet := make(map[int64]bool, len(selectedAlerts))
	for _, alert := range selectedAlerts {
		selectedAlertSet[alert.ID] = true
	}
	for _, alert := range alerts {
		if len(selectedAlerts) == blueChatEvidenceAlerts {
			break
		}
		if !selectedAlertSet[alert.ID] {
			selectedAlerts = append(selectedAlerts, alert)
			selectedAlertSet[alert.ID] = true
		}
	}
	if len(selectedAlerts) > blueChatEvidenceAlerts {
		selectedAlerts = selectedAlerts[:blueChatEvidenceAlerts]
		evidence.AlertsTruncated = true
	}
	if len(alerts) > len(selectedAlerts) {
		evidence.AlertsTruncated = true
	}
	for _, alert := range selectedAlerts {
		evidence.Alerts = append(evidence.Alerts, blueChatEvidenceAlert{
			ID: alert.ID, EventID: alert.EventID, Title: clipBlueChatEvidence(alert.Title, 250),
			Severity: alert.Severity, Status: alert.Status, Reason: clipBlueChatEvidence(alert.Reason, 450),
		})
	}
	payload, err := json.Marshal(evidence)
	if err != nil {
		return nil, err
	}
	for len(payload) > blueChatEvidenceBytes {
		if len(evidence.Events) > len(selectedEventIDs) &&
			(len(evidence.Events) >= len(evidence.Alerts) || len(evidence.Alerts) == len(selectedAlertIDs)) {
			evidence.Events = evidence.Events[:len(evidence.Events)-1]
			evidence.EventsTruncated = true
		} else if len(evidence.Alerts) > len(selectedAlertIDs) {
			evidence.Alerts = evidence.Alerts[:len(evidence.Alerts)-1]
			evidence.AlertsTruncated = true
		} else {
			return nil, fmt.Errorf("selected records exceed the case evidence budget: %w", errBlueChatInvalidEvidence)
		}
		payload, err = json.Marshal(evidence)
		if err != nil {
			return nil, err
		}
	}
	ref := struct {
		BlueCase struct {
			ID               int64   `json:"id"`
			Title            string  `json:"title"`
			EventIDs         []int64 `json:"event_ids"`
			AlertIDs         []int64 `json:"alert_ids"`
			SelectedEventIDs []int64 `json:"selected_event_ids,omitempty"`
			SelectedAlertIDs []int64 `json:"selected_alert_ids,omitempty"`
			EventsTruncated  bool    `json:"events_truncated"`
			AlertsTruncated  bool    `json:"alerts_truncated"`
		} `json:"blue_case"`
	}{}
	ref.BlueCase.ID = id
	ref.BlueCase.Title = clipBlueChatEvidence(item.Title, 120)
	ref.BlueCase.EventIDs = make([]int64, 0, len(evidence.Events))
	ref.BlueCase.AlertIDs = make([]int64, 0, len(evidence.Alerts))
	ref.BlueCase.SelectedEventIDs = append([]int64(nil), selectedEventIDs...)
	ref.BlueCase.SelectedAlertIDs = append([]int64(nil), selectedAlertIDs...)
	ref.BlueCase.EventsTruncated = evidence.EventsTruncated
	ref.BlueCase.AlertsTruncated = evidence.AlertsTruncated
	for _, event := range evidence.Events {
		ref.BlueCase.EventIDs = append(ref.BlueCase.EventIDs, event.ID)
	}
	for _, alert := range evidence.Alerts {
		ref.BlueCase.AlertIDs = append(ref.BlueCase.AlertIDs, alert.ID)
	}
	metadata, err := json.Marshal(ref)
	if err != nil {
		return nil, err
	}
	return &blueChatAttachedCase{prompt: string(payload), metadata: metadata}, nil
}

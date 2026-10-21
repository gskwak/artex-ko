package server

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Autumn-27/artex/db"
)

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
	Events          []blueChatEvidenceEvent `json:"events"`
	Alerts          []blueChatEvidenceAlert `json:"alerts"`
	EventsTruncated bool                    `json:"events_truncated"`
	AlertsTruncated bool                    `json:"alerts_truncated"`
}

func clipBlueChatEvidence(value string, maxRunes int) string {
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes]) + "… [truncated]"
}

func loadBlueChatCase(ctx context.Context, pg *db.DB, id int64) (*blueChatAttachedCase, error) {
	item, err := pg.GetBlueCase(ctx, id)
	if err != nil || item == nil {
		return nil, err
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
		Events:          make([]blueChatEvidenceEvent, 0, min(len(events), blueChatEvidenceEvents)),
		Alerts:          make([]blueChatEvidenceAlert, 0, min(len(alerts), blueChatEvidenceAlerts)),
		EventsTruncated: len(events) > blueChatEvidenceEvents,
		AlertsTruncated: len(alerts) > blueChatEvidenceAlerts,
	}
	evidence.Case.ID = item.ID
	evidence.Case.Title = clipBlueChatEvidence(item.Title, 250)
	evidence.Case.Description = clipBlueChatEvidence(item.Description, 700)
	evidence.Case.Status = item.Status
	evidence.Case.Severity = item.Severity
	evidence.Case.Summary = clipBlueChatEvidence(item.Summary, 700)
	for _, event := range events[:min(len(events), blueChatEvidenceEvents)] {
		evidence.Events = append(evidence.Events, blueChatEvidenceEvent{
			ID: event.ID, Source: clipBlueChatEvidence(event.Source, 120),
			EventType: clipBlueChatEvidence(event.EventType, 120), Severity: event.Severity,
			Asset: clipBlueChatEvidence(event.Asset, 120), Message: clipBlueChatEvidence(event.Message, 450),
			OccurredAt: event.OccurredAt, RawPreview: clipBlueChatEvidence(string(event.Raw), 450),
		})
	}
	for _, alert := range alerts[:min(len(alerts), blueChatEvidenceAlerts)] {
		evidence.Alerts = append(evidence.Alerts, blueChatEvidenceAlert{
			ID: alert.ID, EventID: alert.EventID, Title: clipBlueChatEvidence(alert.Title, 250),
			Severity: alert.Severity, Status: alert.Status, Reason: clipBlueChatEvidence(alert.Reason, 450),
		})
	}
	payload, err := json.Marshal(evidence)
	if err != nil {
		return nil, err
	}
	for len(payload) > blueChatEvidenceBytes && len(evidence.Events)+len(evidence.Alerts) > 0 {
		if len(evidence.Events) >= len(evidence.Alerts) && len(evidence.Events) > 0 {
			evidence.Events = evidence.Events[:len(evidence.Events)-1]
			evidence.EventsTruncated = true
		} else {
			evidence.Alerts = evidence.Alerts[:len(evidence.Alerts)-1]
			evidence.AlertsTruncated = true
		}
		payload, err = json.Marshal(evidence)
		if err != nil {
			return nil, err
		}
	}
	ref := struct {
		BlueCase struct {
			ID              int64   `json:"id"`
			Title           string  `json:"title"`
			EventIDs        []int64 `json:"event_ids"`
			AlertIDs        []int64 `json:"alert_ids"`
			EventsTruncated bool    `json:"events_truncated"`
			AlertsTruncated bool    `json:"alerts_truncated"`
		} `json:"blue_case"`
	}{}
	ref.BlueCase.ID = id
	ref.BlueCase.Title = clipBlueChatEvidence(item.Title, 120)
	ref.BlueCase.EventIDs = make([]int64, 0, len(evidence.Events))
	ref.BlueCase.AlertIDs = make([]int64, 0, len(evidence.Alerts))
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

package server

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/db"
)

// This integration test must use a disposable database. It verifies that
// search and filters run before LIMIT, rather than only on a loaded UI page.
func TestBlueListSearchBeforePaginationPG(t *testing.T) {
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
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	needle := "older-blue-search-" + suffix
	caseA, err := pg.CreateBlueCase(ctx, db.BlueCase{Title: needle, Severity: "high"})
	if err != nil {
		t.Fatal(err)
	}
	caseB, err := pg.CreateBlueCase(ctx, db.BlueCase{Title: "newer-blue-search-" + suffix, Severity: "low"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pg.ExecContext(ctx, `DELETE FROM blue_cases WHERE id IN ($1,$2)`, caseA.ID, caseB.ID)
	})
	events, err := pg.ImportBlueEvents(ctx, []db.BlueEvent{
		{Source: "sensor-" + suffix, ExternalID: "old-" + suffix, Asset: needle, Message: "older evidence", Severity: "high", OccurredAt: time.Now().Add(-time.Hour).UTC()},
		{Source: "sensor-" + suffix, ExternalID: "new-" + suffix, Asset: "newer-evidence", Message: "newer evidence", Severity: "low", OccurredAt: time.Now().UTC()},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pg.ExecContext(ctx, `DELETE FROM blue_events WHERE id IN ($1,$2)`, events[0].ID, events[1].ID)
	})
	rule, err := pg.CreateBlueRule(ctx, db.BlueRule{Name: "search-rule-" + suffix, Field: "message", Operator: "contains", Value: "evidence", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pg.ExecContext(ctx, `DELETE FROM blue_rules WHERE id=$1`, rule.ID) })
	alertA, err := pg.UpsertBlueAlert(ctx, db.BlueAlert{RuleID: rule.ID, EventID: events[0].ID, Title: needle, Severity: "high"})
	if err != nil {
		t.Fatal(err)
	}
	alertB, err := pg.UpsertBlueAlert(ctx, db.BlueAlert{RuleID: rule.ID, EventID: events[1].ID, Title: "newer-alert-" + suffix, Severity: "low"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pg.ExecContext(ctx, `DELETE FROM blue_alerts WHERE id IN ($1,$2)`, alertA.ID, alertB.ID)
	})
	taskA, err := pg.CreateBlueTask(ctx, db.BlueTask{Title: needle, CaseID: &caseA.ID, Priority: "critical"})
	if err != nil {
		t.Fatal(err)
	}
	taskB, err := pg.CreateBlueTask(ctx, db.BlueTask{Title: "newer-task-" + suffix, CaseID: &caseB.ID, Priority: "low"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pg.ExecContext(ctx, `DELETE FROM blue_tasks WHERE id IN ($1,$2)`, taskA.ID, taskB.ID) })

	s := &Server{m: &Manager{pg: pg}, jwtKey: []byte(strings.Repeat("s", 32))}
	token, err := signJWT(s.signingKey(s.readPassHash()))
	if err != nil {
		t.Fatal(err)
	}
	h := s.Handler()
	escaped := url.QueryEscape(needle)
	tests := []struct {
		name, path string
		wantID     int64
	}{
		{"case search", "/api/blue/cases?limit=1&q=" + escaped, caseA.ID},
		{"case severity", "/api/blue/cases?limit=1&severity=high&q=" + escaped, caseA.ID},
		{"event search", "/api/blue/events?limit=1&q=" + escaped, events[0].ID},
		{"event source and severity", "/api/blue/events?limit=1&source=" + url.QueryEscape(strings.ToUpper("sensor-"+suffix)) + "&severity=high", events[0].ID},
		{"alert search", "/api/blue/alerts?limit=1&q=" + escaped, alertA.ID},
		{"task search", "/api/blue/tasks?limit=1&q=" + escaped, taskA.ID},
		{"task priority", "/api/blue/tasks?limit=1&priority=critical&q=" + escaped, taskA.ID},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var rows []struct {
				ID int64 `json:"id"`
			}
			blueAPIRequest(t, h, token, http.MethodGet, tc.path, nil, http.StatusOK, &rows)
			if len(rows) != 1 || rows[0].ID != tc.wantID {
				t.Fatalf("%s: got %+v, want id %d", tc.path, rows, tc.wantID)
			}
		})
	}
	var sorted []struct {
		ID int64 `json:"id"`
	}
	blueAPIRequest(t, h, token, http.MethodGet,
		"/api/blue/tasks?limit=1&sort=id&order=asc&q="+url.QueryEscape(suffix), nil, http.StatusOK, &sorted)
	if len(sorted) != 1 || sorted[0].ID != taskA.ID {
		t.Fatalf("sorted tasks: got %+v, want oldest id %d", sorted, taskA.ID)
	}
	for _, path := range []string{
		"/api/blue/cases?q=" + strings.Repeat("a", 201),
		"/api/blue/cases?q=%FF",
		"/api/blue/cases?severity=invalid",
		"/api/blue/events?severity=invalid",
		"/api/blue/tasks?sort=invalid",
		"/api/blue/tasks?order=invalid",
	} {
		blueAPIRequest(t, h, token, http.MethodGet, path, nil, http.StatusBadRequest, nil)
	}
}

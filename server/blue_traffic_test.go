package server

import (
	"context"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/db"
)

func TestBlueTrafficImportedEventFilteringPG(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ARTEX_BLUE_TEST_PG_DSN"))
	if dsn == "" {
		t.Skip("set ARTEX_BLUE_TEST_PG_DSN to a disposable PostgreSQL test database")
	}
	pg, err := db.Open(dsn)
	if err != nil {
		t.Fatalf("open blue-team test database: %v", err)
	}
	t.Cleanup(func() { _ = pg.Close() })
	s := &Server{m: &Manager{pg: pg}, jwtKey: []byte(strings.Repeat("t", 32))}
	h := s.Handler()
	token, err := signJWT(s.signingKey(s.readPassHash()))
	if err != nil {
		t.Fatal(err)
	}
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	source := "fixture-e2e-" + suffix
	nestedSource := "nested-e2e-" + suffix
	base := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	var imported struct {
		Events []db.BlueEvent `json:"events"`
	}
	blueAPIRequest(t, h, token, http.MethodPost, "/api/blue/events/import", map[string]any{"events": []map[string]any{
		{"source": source, "event_type": "generic", "message": "outbound connection", "occurred_at": base,
			"raw": map[string]any{"src_ip": "192.0.2.8", "dest_ip": "198.51.100.7", "proto": "TCP"}},
		{"source": source, "event_type": "generic", "message": "Zeek exchange", "occurred_at": base.Add(time.Minute),
			"raw": map[string]any{"id.orig_h": "192.0.2.8", "id.resp_h": "203.0.113.9", "protocol": "UDP"}},
		{"source": source, "event_type": "network.flow", "message": "flow summary", "occurred_at": base.Add(2 * time.Minute),
			"raw": map[string]any{"proto": "ICMP"}},
		{"source": source, "event_type": "auth.failure", "message": "login failure", "occurred_at": base.Add(3 * time.Minute),
			"raw": map[string]any{"src_ip": "192.0.2.8", "proto": "TCP"}},
		{"source": nestedSource, "event_type": "generic", "message": "file-style nested network fields", "occurred_at": base.Add(4 * time.Minute),
			"raw": map[string]any{"raw": map[string]any{"src_ip": "192.0.2.9", "dest_ip": "198.51.100.12", "proto": "TCP", "app_proto": "HTTP"}}},
	}}, http.StatusCreated, &imported)
	if len(imported.Events) != 5 {
		t.Fatalf("imported %d events, want 5", len(imported.Events))
	}
	t.Cleanup(func() {
		for _, event := range imported.Events {
			if _, err := pg.ExecContext(context.Background(), `DELETE FROM blue_events WHERE id=$1`, event.ID); err != nil {
				t.Errorf("remove blue traffic fixture %d: %v", event.ID, err)
			}
		}
	})

	type trafficPage struct {
		Items []db.BlueEvent `json:"items"`
		Total int64          `json:"total"`
	}
	basePath := "/api/blue/traffic?source=" + source
	var first trafficPage
	blueAPIRequest(t, h, token, http.MethodGet, basePath+"&limit=1", nil, http.StatusOK, &first)
	if first.Total != 3 || len(first.Items) != 1 || first.Items[0].ID != imported.Events[2].ID {
		t.Fatalf("first network page = %+v, want newest of 3 network events", first)
	}
	var second trafficPage
	blueAPIRequest(t, h, token, http.MethodGet, basePath+"&limit=1&offset=1", nil, http.StatusOK, &second)
	if second.Total != 3 || len(second.Items) != 1 || second.Items[0].ID != imported.Events[1].ID {
		t.Fatalf("second network page = %+v, want Zeek event", second)
	}
	var tcp trafficPage
	blueAPIRequest(t, h, token, http.MethodGet, basePath+"&protocol=tcp", nil, http.StatusOK, &tcp)
	if tcp.Total != 1 || len(tcp.Items) != 1 || tcp.Items[0].ID != imported.Events[0].ID {
		t.Fatalf("TCP filter = %+v, want only Suricata-style connection", tcp)
	}
	var searched trafficPage
	blueAPIRequest(t, h, token, http.MethodGet, basePath+"&q=198.51.100.7", nil, http.StatusOK, &searched)
	if searched.Total != 1 || len(searched.Items) != 1 || searched.Items[0].ID != imported.Events[0].ID {
		t.Fatalf("address search = %+v, want one matching connection", searched)
	}
	var nested trafficPage
	blueAPIRequest(t, h, token, http.MethodGet, "/api/blue/traffic?source="+nestedSource[:len(nestedSource)-1]+"&protocol=http", nil, http.StatusOK, &nested)
	if nested.Total != 1 || len(nested.Items) != 1 || nested.Items[0].ID != imported.Events[4].ID {
		t.Fatalf("file-style nested fields and application protocol = %+v, want one connection", nested)
	}
	var nestedSearch trafficPage
	blueAPIRequest(t, h, token, http.MethodGet, "/api/blue/traffic?source="+nestedSource+"&q=198.51.100.12", nil, http.StatusOK, &nestedSearch)
	if nestedSearch.Total != 1 || len(nestedSearch.Items) != 1 || nestedSearch.Items[0].ID != imported.Events[4].ID {
		t.Fatalf("nested address search = %+v, want one connection", nestedSearch)
	}
	blueAPIRequest(t, h, token, http.MethodGet, basePath+"&limit=0", nil, http.StatusBadRequest, nil)
	blueAPIRequest(t, h, token, http.MethodGet, basePath+"&q="+strings.Repeat("x", 201), nil, http.StatusBadRequest, nil)
}

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Autumn-27/artex/db"
)

type blueSourceTestState struct {
	ID                  int64      `json:"id"`
	Name                string     `json:"name"`
	URL                 string     `json:"url"`
	Enabled             bool       `json:"enabled"`
	PollIntervalSeconds int        `json:"poll_interval_seconds"`
	Cursor              string     `json:"cursor"`
	LastSyncedAt        *time.Time `json:"last_synced_at"`
	LastError           string     `json:"last_error"`
}

func blueSourceTestHarness(t *testing.T) (*db.DB, *Server, http.Handler, string) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ARTEX_BLUE_TEST_PG_DSN"))
	if dsn == "" {
		t.Skip("set ARTEX_BLUE_TEST_PG_DSN to a disposable PostgreSQL test database")
	}
	pg, err := db.Open(dsn)
	if err != nil {
		t.Fatalf("open blue-team test database: %v", err)
	}
	t.Cleanup(func() { _ = pg.Close() })
	s := &Server{m: &Manager{pg: pg}, jwtKey: []byte(strings.Repeat("s", 32)), blueSourceSyncing: map[int64]bool{}}
	token, err := signJWT(s.signingKey(s.readPassHash()))
	if err != nil {
		t.Fatal(err)
	}
	return pg, s, s.Handler(), token
}

func blueSourceTestJSONL(t *testing.T, source, externalID, path string) string {
	t.Helper()
	record := map[string]any{
		"source": source, "external_id": externalID, "event_type": "http.request",
		"severity": "medium", "asset": "fixture:8080",
		"message": "GET " + path + " returned 200", "occurred_at": time.Now().UTC().Format(time.RFC3339Nano),
		"src_ip": "192.0.2.10", "dest_ip": "198.51.100.20", "proto": "TCP", "method": "GET", "url": path,
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded) + "\n"
}

func blueSourceTestGet(t *testing.T, h http.Handler, token string, id int64) blueSourceTestState {
	t.Helper()
	var sources []blueSourceTestState
	blueAPIRequest(t, h, token, http.MethodGet, "/api/blue/sources", nil, http.StatusOK, &sources)
	for _, source := range sources {
		if source.ID == id {
			return source
		}
	}
	t.Fatalf("source %d absent from list", id)
	return blueSourceTestState{}
}

func TestBlueSourcesRequireJWT(t *testing.T) {
	s := &Server{m: &Manager{}, jwtKey: []byte(strings.Repeat("s", 32))}
	h := s.Handler()
	for _, tc := range []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodGet, "/api/blue/sources", nil},
		{http.MethodPost, "/api/blue/sources", map[string]any{"name": "x", "url": "http://example.test/logs"}},
		{http.MethodPatch, "/api/blue/sources/1", map[string]any{"enabled": false}},
		{http.MethodDelete, "/api/blue/sources/1", nil},
		{http.MethodPost, "/api/blue/sources/1/sync", nil},
	} {
		blueAPIRequest(t, h, "", tc.method, tc.path, tc.body, http.StatusUnauthorized, nil)
	}
}

func TestBlueSourcePullWorkflowPG(t *testing.T) {
	pg, _, h, token := blueSourceTestHarness(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	sourceName := "pull-test-" + suffix
	line1 := blueSourceTestJSONL(t, sourceName, "one", "/debug/config")
	line2 := blueSourceTestJSONL(t, sourceName, "two", "/api/items")
	var mu sync.Mutex
	mode := "normal"
	var redirectTargetHits atomic.Int32
	var emptyCursorQueryHits atomic.Int32
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect-target" {
			redirectTargetHits.Add(1)
			w.Header().Set("Content-Type", "application/x-ndjson")
			w.Header().Set("X-ARTEX-Next-Cursor", "redirected")
			_, _ = w.Write([]byte(line1))
			return
		}
		mu.Lock()
		currentMode := mode
		mu.Unlock()
		switch currentMode {
		case "bad-type":
			w.Header().Set("Content-Type", "text/html")
			w.Header().Set("X-ARTEX-Next-Cursor", "bad")
			_, _ = w.Write([]byte("<html>not telemetry</html>"))
			return
		case "bad-cursor":
			w.Header().Set("Content-Type", "application/x-ndjson")
			w.Header().Set("X-ARTEX-Next-Cursor", strings.Repeat("x", 1025))
			_, _ = w.Write([]byte(line1))
			return
		case "too-large":
			w.Header().Set("Content-Type", "application/x-ndjson")
			w.Header().Set("X-ARTEX-Next-Cursor", "oversized")
			_, _ = w.Write([]byte(strings.Repeat("x", maxBlueBodyBytes+1)))
			return
		case "server-error":
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		case "redirect":
			w.Header().Set("Location", "/redirect-target")
			w.WriteHeader(http.StatusFound)
			return
		case "missing-cursor":
			w.Header().Set("Content-Type", "application/x-ndjson")
			_, _ = w.Write([]byte(line1))
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		switch r.URL.Query().Get("cursor") {
		case "":
			if r.URL.Query().Has("cursor") {
				emptyCursorQueryHits.Add(1)
			}
			w.Header().Set("X-ARTEX-Next-Cursor", "cursor-1")
			_, _ = w.Write([]byte(line1))
		case "cursor-1":
			w.Header().Set("X-ARTEX-Next-Cursor", "cursor-2")
			_, _ = w.Write([]byte(line2))
		case "cursor-2":
			w.Header().Set("X-ARTEX-Next-Cursor", "cursor-2")
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer feed.Close()

	var created blueSourceTestState
	blueAPIRequest(t, h, token, http.MethodPost, "/api/blue/sources", map[string]any{
		"name": sourceName, "url": feed.URL, "enabled": false, "poll_interval_seconds": 30,
	}, http.StatusCreated, &created)
	if created.ID <= 0 || created.Name != sourceName || created.Enabled || created.PollIntervalSeconds != 30 {
		t.Fatalf("created source = %+v", created)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		if _, err := pg.ExecContext(ctx, `DELETE FROM blue_events WHERE source=$1`, sourceName); err != nil {
			t.Errorf("remove pull events: %v", err)
		}
		if _, err := pg.ExecContext(ctx, `DELETE FROM blue_event_sources WHERE id=$1`, created.ID); err != nil {
			t.Errorf("remove pull source: %v", err)
		}
	})
	var listed []blueSourceTestState
	blueAPIRequest(t, h, token, http.MethodGet, "/api/blue/sources", nil, http.StatusOK, &listed)
	if len(listed) == 0 {
		t.Fatal("created source missing from list")
	}
	for i, wantCursor := range []string{"cursor-1", "cursor-2", "cursor-2"} {
		var synced struct {
			Source    blueSourceTestState `json:"source"`
			Processed int                 `json:"processed"`
		}
		blueAPIRequest(t, h, token, http.MethodPost, fmt.Sprintf("/api/blue/sources/%d/sync", created.ID), nil, http.StatusOK, &synced)
		if synced.Source.Cursor != wantCursor || synced.Processed != min(i+1, 2)-min(i, 2) {
			t.Fatalf("sync %d = %+v, want cursor %q", i+1, synced, wantCursor)
		}
	}
	if emptyCursorQueryHits.Load() != 0 {
		t.Fatal("initial source request must omit an empty cursor query")
	}
	var traffic struct {
		Total int64 `json:"total"`
	}
	blueAPIRequest(t, h, token, http.MethodGet, "/api/blue/traffic?source="+sourceName, nil, http.StatusOK, &traffic)
	if traffic.Total != 2 {
		t.Fatalf("traffic total = %d, want 2", traffic.Total)
	}
	var sourceRows int
	if err := pg.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM blue_events WHERE source=$1 AND raw->>'source'=$2`, sourceName, sourceName).Scan(&sourceRows); err != nil {
		t.Fatal(err)
	}
	if sourceRows != 2 {
		t.Fatalf("pull source stored %d events with original source in raw, want 2", sourceRows)
	}

	// A failed pull must not advance the persisted cursor. The next healthy pull
	// can therefore retry without losing evidence.
	for _, bad := range []string{"bad-type", "bad-cursor", "too-large", "server-error", "redirect", "missing-cursor"} {
		mu.Lock()
		mode = bad
		mu.Unlock()
		blueAPIRequest(t, h, token, http.MethodPost, fmt.Sprintf("/api/blue/sources/%d/sync", created.ID), nil, http.StatusBadGateway, nil)
		state := blueSourceTestGet(t, h, token, created.ID)
		if state.Cursor != "cursor-2" || state.LastError == "" {
			t.Fatalf("%s changed cursor or omitted error: %+v", bad, state)
		}
	}
	if redirectTargetHits.Load() != 0 {
		t.Fatalf("followed a telemetry redirect %d times", redirectTargetHits.Load())
	}
	mu.Lock()
	mode = "normal"
	mu.Unlock()
	var recovered struct {
		Source blueSourceTestState `json:"source"`
	}
	blueAPIRequest(t, h, token, http.MethodPost, fmt.Sprintf("/api/blue/sources/%d/sync", created.ID), nil, http.StatusOK, &recovered)
	if recovered.Source.Cursor != "cursor-2" || recovered.Source.LastError != "" {
		t.Fatalf("healthy pull did not clear error: %+v", recovered.Source)
	}

	// Simulate a replay after a crash: the cursor is reset but stable source and
	// external_id values prevent duplicate blue_events rows.
	var reset blueSourceTestState
	blueAPIRequest(t, h, token, http.MethodPatch, fmt.Sprintf("/api/blue/sources/%d", created.ID),
		map[string]any{"reset_cursor": true}, http.StatusOK, &reset)
	if reset.Cursor != "" {
		t.Fatalf("cursor reset = %q, want empty", reset.Cursor)
	}
	blueAPIRequest(t, h, token, http.MethodPost, fmt.Sprintf("/api/blue/sources/%d/sync", created.ID), nil, http.StatusOK, nil)
	var count int
	if err := pg.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM blue_events WHERE source=$1`, sourceName).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("replayed source has %d events, want 2", count)
	}
	blueAPIRequest(t, h, token, http.MethodDelete, fmt.Sprintf("/api/blue/sources/%d", created.ID), nil, http.StatusNoContent, nil)
	var afterDelete []blueSourceTestState
	blueAPIRequest(t, h, token, http.MethodGet, "/api/blue/sources", nil, http.StatusOK, &afterDelete)
	for _, source := range afterDelete {
		if source.ID == created.ID {
			t.Fatalf("deleted source %d is still listed", created.ID)
		}
	}
}

func TestBlueSourceConcurrentSyncPG(t *testing.T) {
	pg, _, h, token := blueSourceTestHarness(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	sourceName := "pull-concurrent-" + suffix
	line := blueSourceTestJSONL(t, sourceName, "one", "/debug/config")
	entered := make(chan struct{})
	release := make(chan struct{})
	var enterOnce sync.Once
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		enterOnce.Do(func() { close(entered) })
		<-release
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("X-ARTEX-Next-Cursor", "next")
		_, _ = w.Write([]byte(line))
	}))
	defer feed.Close()
	var created blueSourceTestState
	blueAPIRequest(t, h, token, http.MethodPost, "/api/blue/sources", map[string]any{
		"name": sourceName, "url": feed.URL, "enabled": false,
	}, http.StatusCreated, &created)
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pg.ExecContext(ctx, `DELETE FROM blue_events WHERE source=$1`, sourceName)
		_, _ = pg.ExecContext(ctx, `DELETE FROM blue_event_sources WHERE id=$1`, created.ID)
	})
	path := fmt.Sprintf("/api/blue/sources/%d/sync", created.ID)
	firstCode := make(chan int, 1)
	go func() {
		r := httptest.NewRequest(http.MethodPost, path, nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		firstCode <- w.Code
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("first pull never reached source")
	}
	blueAPIRequest(t, h, token, http.MethodPost, path, nil, http.StatusConflict, nil)
	close(release)
	select {
	case code := <-firstCode:
		if code != http.StatusOK {
			t.Fatalf("first sync status = %d, want 200", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first sync did not finish")
	}
}

func TestBlueSourceAutoPollDueAndEnabledPG(t *testing.T) {
	pg, s, h, token := blueSourceTestHarness(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	enabledName := "pull-auto-on-" + suffix
	disabledName := "pull-auto-off-" + suffix
	var enabledHits, disabledHits atomic.Int32
	feed := func(name string, hits *atomic.Int32) *httptest.Server {
		line := blueSourceTestJSONL(t, name, "one", "/debug/config")
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			hits.Add(1)
			w.Header().Set("Content-Type", "application/x-ndjson")
			w.Header().Set("X-ARTEX-Next-Cursor", "next")
			_, _ = w.Write([]byte(line))
		}))
	}
	enabledFeed := feed(enabledName, &enabledHits)
	disabledFeed := feed(disabledName, &disabledHits)
	defer enabledFeed.Close()
	defer disabledFeed.Close()
	var enabled, disabled blueSourceTestState
	blueAPIRequest(t, h, token, http.MethodPost, "/api/blue/sources", map[string]any{
		"name": enabledName, "url": enabledFeed.URL, "enabled": true, "poll_interval_seconds": 10,
	}, http.StatusCreated, &enabled)
	blueAPIRequest(t, h, token, http.MethodPost, "/api/blue/sources", map[string]any{
		"name": disabledName, "url": disabledFeed.URL, "enabled": false, "poll_interval_seconds": 10,
	}, http.StatusCreated, &disabled)
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pg.ExecContext(ctx, `DELETE FROM blue_events WHERE source IN($1,$2)`, enabledName, disabledName)
		_, _ = pg.ExecContext(ctx, `DELETE FROM blue_event_sources WHERE id IN($1,$2)`, enabled.ID, disabled.ID)
	})
	if _, err := pg.ExecContext(context.Background(),
		`UPDATE blue_event_sources SET last_synced_at=now()-interval '20 seconds' WHERE id IN($1,$2)`,
		enabled.ID, disabled.ID); err != nil {
		t.Fatal(err)
	}
	s.pollBlueSources(context.Background())
	if enabledHits.Load() != 1 || disabledHits.Load() != 0 {
		t.Fatalf("auto-poll hits enabled=%d disabled=%d, want 1 and 0", enabledHits.Load(), disabledHits.Load())
	}
	if state := blueSourceTestGet(t, h, token, enabled.ID); state.Cursor != "next" || state.LastSyncedAt == nil {
		t.Fatalf("enabled source was not synchronized: %+v", state)
	}
	if state := blueSourceTestGet(t, h, token, disabled.ID); state.Cursor != "" {
		t.Fatalf("disabled source cursor changed: %+v", state)
	}
	s.pollBlueSources(context.Background())
	if enabledHits.Load() != 1 {
		t.Fatalf("not-yet-due source was pulled again: hits=%d", enabledHits.Load())
	}
}

func TestBlueSourceAutoPollSkipsDisabledAfterSnapshotPG(t *testing.T) {
	pg, s, h, token := blueSourceTestHarness(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	firstName, secondName := "pull-pause-first-"+suffix, "pull-pause-second-"+suffix
	firstLine := blueSourceTestJSONL(t, firstName, "one", "/first")
	secondLine := blueSourceTestJSONL(t, secondName, "one", "/second")
	var firstHits, secondHits atomic.Int32
	var secondID int64
	var disableErr error
	firstFeed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		firstHits.Add(1)
		_, disableErr = pg.ExecContext(context.Background(), `UPDATE blue_event_sources SET enabled=false WHERE id=$1`, secondID)
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("X-ARTEX-Next-Cursor", "next")
		_, _ = w.Write([]byte(firstLine))
	}))
	defer firstFeed.Close()
	secondFeed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		secondHits.Add(1)
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("X-ARTEX-Next-Cursor", "next")
		_, _ = w.Write([]byte(secondLine))
	}))
	defer secondFeed.Close()
	var first, second blueSourceTestState
	blueAPIRequest(t, h, token, http.MethodPost, "/api/blue/sources", map[string]any{
		"name": firstName, "url": firstFeed.URL, "enabled": true, "poll_interval_seconds": 10,
	}, http.StatusCreated, &first)
	blueAPIRequest(t, h, token, http.MethodPost, "/api/blue/sources", map[string]any{
		"name": secondName, "url": secondFeed.URL, "enabled": true, "poll_interval_seconds": 10,
	}, http.StatusCreated, &second)
	secondID = second.ID
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pg.ExecContext(ctx, `DELETE FROM blue_events WHERE source IN($1,$2)`, firstName, secondName)
		_, _ = pg.ExecContext(ctx, `DELETE FROM blue_event_sources WHERE id IN($1,$2)`, first.ID, second.ID)
	})
	if _, err := pg.ExecContext(context.Background(),
		`UPDATE blue_event_sources SET last_synced_at=now()-interval '20 seconds' WHERE id IN($1,$2)`,
		first.ID, second.ID); err != nil {
		t.Fatal(err)
	}
	s.pollBlueSources(context.Background())
	if disableErr != nil {
		t.Fatal(disableErr)
	}
	if firstHits.Load() != 1 || secondHits.Load() != 0 {
		t.Fatalf("snapshot pause fetched first=%d second=%d, want 1 and 0", firstHits.Load(), secondHits.Load())
	}
	if state := blueSourceTestGet(t, h, token, second.ID); state.Enabled || state.Cursor != "" {
		t.Fatalf("paused source changed: %+v", state)
	}
}

func TestBlueSourceRenamePreservesDefaultSourceIdempotencePG(t *testing.T) {
	pg, _, h, token := blueSourceTestHarness(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	name := "pull-rename-" + suffix
	var event map[string]any
	if err := json.Unmarshal([]byte(blueSourceTestJSONL(t, name, "one", "/health")), &event); err != nil {
		t.Fatal(err)
	}
	delete(event, "source")
	line, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("X-ARTEX-Next-Cursor", "next")
		_, _ = w.Write(line)
		_, _ = w.Write([]byte("\n"))
	}))
	defer feed.Close()
	var created blueSourceTestState
	blueAPIRequest(t, h, token, http.MethodPost, "/api/blue/sources", map[string]any{
		"name": name, "url": feed.URL, "enabled": false,
	}, http.StatusCreated, &created)
	defaultSource := fmt.Sprintf("pull:%d", created.ID)
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pg.ExecContext(ctx, `DELETE FROM blue_events WHERE source=$1`, defaultSource)
		_, _ = pg.ExecContext(ctx, `DELETE FROM blue_event_sources WHERE id=$1`, created.ID)
	})
	syncPath := fmt.Sprintf("/api/blue/sources/%d/sync", created.ID)
	blueAPIRequest(t, h, token, http.MethodPost, syncPath, nil, http.StatusOK, nil)
	blueAPIRequest(t, h, token, http.MethodPatch, fmt.Sprintf("/api/blue/sources/%d", created.ID), map[string]any{
		"name": name + "-renamed", "reset_cursor": true,
	}, http.StatusOK, nil)
	blueAPIRequest(t, h, token, http.MethodPost, syncPath, nil, http.StatusOK, nil)
	var count int
	if err := pg.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM blue_events WHERE source=$1`, defaultSource).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("renamed source replay retained %d events, want 1", count)
	}
}

func TestBlueSourceURLValidationPG(t *testing.T) {
	_, _, h, token := blueSourceTestHarness(t)
	for i, rawURL := range []string{
		"file:///etc/passwd", "http://user:pass@example.test/logs", "http://example.test/logs#fragment",
		"http://example.test/logs?cursor=override", "http://", strings.Repeat("x", 4096),
	} {
		blueAPIRequest(t, h, token, http.MethodPost, "/api/blue/sources", map[string]any{
			"name": fmt.Sprintf("invalid-source-%d", i), "url": rawURL,
		}, http.StatusBadRequest, nil)
	}
}

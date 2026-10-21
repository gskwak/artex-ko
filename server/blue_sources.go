package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Autumn-27/artex/blue/ingest"
	"github.com/Autumn-27/artex/db"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	blueSourceDefaultInterval = 30
	blueSourceMinInterval     = 10
	blueSourceMaxInterval     = 3600
	blueSourceMaxURLBytes     = 2048
	blueSourceMaxCursorBytes  = 1024
	blueSourceFetchTimeout    = 10 * time.Second
	blueSourcePollTick        = 5 * time.Second
)

// Pull sources are an administrator-configured integration point. URLs can
// reach the server's private network, so they are never derived from telemetry
// and redirects are rejected. All handlers use the normal JWT middleware.
func (s *Server) registerBlueSourceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/blue/sources", s.blueListSources)
	mux.HandleFunc("POST /api/blue/sources", s.blueCreateSource)
	mux.HandleFunc("PATCH /api/blue/sources/{id}", s.bluePatchSource)
	mux.HandleFunc("DELETE /api/blue/sources/{id}", s.blueDeleteSource)
	mux.HandleFunc("POST /api/blue/sources/{id}/sync", s.blueSyncSource)
}

func validBlueSourceURL(raw string) error {
	if raw == "" || len(raw) > blueSourceMaxURLBytes {
		return errors.New("source URL must contain 1 to 2048 bytes")
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" ||
		u.User != nil || u.Fragment != "" || u.Opaque != "" {
		return errors.New("source URL must be an http(s) URL without credentials or fragment")
	}
	if port := u.Port(); port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return errors.New("source URL has an invalid port")
		}
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil || query.Has("cursor") {
		return errors.New("source URL query must be valid and must not contain cursor")
	}
	return nil
}

func validBlueSourceCursor(cursor string) bool {
	if len(cursor) > blueSourceMaxCursorBytes || !utf8.ValidString(cursor) {
		return false
	}
	for _, r := range cursor {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func validateBlueSourceConfig(name, rawURL string, interval int) error {
	if name == "" || len(name) > 200 {
		return errors.New("source name must contain 1 to 200 bytes")
	}
	if err := validBlueSourceURL(rawURL); err != nil {
		return err
	}
	if interval < blueSourceMinInterval || interval > blueSourceMaxInterval {
		return fmt.Errorf("poll_interval_seconds must be between %d and %d", blueSourceMinInterval, blueSourceMaxInterval)
	}
	return nil
}

func blueSourceConflict(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func (s *Server) blueListSources(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	items, err := pg.ListBlueEventSources(r.Context())
	if err != nil {
		blueInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) blueCreateSource(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	var req struct {
		Name                string `json:"name"`
		URL                 string `json:"url"`
		Enabled             *bool  `json:"enabled"`
		PollIntervalSeconds *int   `json:"poll_interval_seconds"`
	}
	if !decodeBlueJSON(w, r, &req) {
		return
	}
	req.Name, req.URL = strings.TrimSpace(req.Name), strings.TrimSpace(req.URL)
	interval := blueSourceDefaultInterval
	if req.PollIntervalSeconds != nil {
		interval = *req.PollIntervalSeconds
	}
	if err := validateBlueSourceConfig(req.Name, req.URL, interval); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	item, err := pg.CreateBlueEventSource(r.Context(), db.BlueEventSource{
		Name: req.Name, URL: req.URL, Enabled: enabled, PollIntervalSeconds: interval,
	})
	if blueSourceConflict(err) {
		writeErr(w, http.StatusConflict, "source name already exists")
		return
	}
	if err != nil {
		blueInternal(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

// One process never fetches the same source twice concurrently. The database's
// URL-and-cursor compare-and-swap also prevents cursor regression across app
// instances or a configuration edit during a fetch.
func (s *Server) beginBlueSourceOperation(id int64) bool {
	s.blueSourceSyncMu.Lock()
	defer s.blueSourceSyncMu.Unlock()
	if s.blueSourceSyncing == nil {
		s.blueSourceSyncing = make(map[int64]bool)
	}
	if s.blueSourceSyncing[id] {
		return false
	}
	s.blueSourceSyncing[id] = true
	return true
}

func (s *Server) endBlueSourceOperation(id int64) {
	s.blueSourceSyncMu.Lock()
	delete(s.blueSourceSyncing, id)
	s.blueSourceSyncMu.Unlock()
}

func (s *Server) bluePatchSource(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id, ok := blueID(w, r)
	if !ok {
		return
	}
	var req struct {
		Name                *string `json:"name"`
		URL                 *string `json:"url"`
		Enabled             *bool   `json:"enabled"`
		PollIntervalSeconds *int    `json:"poll_interval_seconds"`
		ResetCursor         bool    `json:"reset_cursor"`
	}
	if !decodeBlueJSON(w, r, &req) {
		return
	}
	if !s.beginBlueSourceOperation(id) {
		writeErr(w, http.StatusConflict, "source synchronization is in progress")
		return
	}
	defer s.endBlueSourceOperation(id)
	old, err := pg.GetBlueEventSource(r.Context(), id)
	if err != nil {
		blueInternal(w, err)
		return
	}
	if old == nil {
		writeErr(w, http.StatusNotFound, "source not found")
		return
	}
	name, rawURL, interval := old.Name, old.URL, old.PollIntervalSeconds
	if req.Name != nil {
		name = strings.TrimSpace(*req.Name)
		*req.Name = name
	}
	if req.URL != nil {
		rawURL = strings.TrimSpace(*req.URL)
		*req.URL = rawURL
	}
	if req.PollIntervalSeconds != nil {
		interval = *req.PollIntervalSeconds
	}
	if err := validateBlueSourceConfig(name, rawURL, interval); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	item, err := pg.PatchBlueEventSource(r.Context(), id, db.BlueEventSourcePatch{
		Name: req.Name, URL: req.URL, Enabled: req.Enabled,
		PollIntervalSeconds: req.PollIntervalSeconds, ResetCursor: req.ResetCursor,
	})
	if blueSourceConflict(err) {
		writeErr(w, http.StatusConflict, "source name already exists")
		return
	}
	if err != nil {
		blueInternal(w, err)
		return
	}
	if item == nil {
		writeErr(w, http.StatusNotFound, "source not found")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) blueDeleteSource(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id, ok := blueID(w, r)
	if !ok {
		return
	}
	if !s.beginBlueSourceOperation(id) {
		writeErr(w, http.StatusConflict, "source synchronization is in progress")
		return
	}
	defer s.endBlueSourceOperation(id)
	deleted, err := pg.DeleteBlueEventSource(r.Context(), id)
	if err != nil {
		blueInternal(w, err)
		return
	}
	if !deleted {
		writeErr(w, http.StatusNotFound, "source not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type blueSourceSyncResult struct {
	Source          *db.BlueEventSource `json:"source"`
	Processed       int                 `json:"processed"`
	AlertsMatched   int                 `json:"alerts_matched"`
	AlertsCreated   int                 `json:"alerts_created"`
	DetectionErrors int                 `json:"detection_errors"`
	Warnings        []string            `json:"warnings"`
}

func (s *Server) blueSyncSource(w http.ResponseWriter, r *http.Request) {
	if s.pg(w) == nil {
		return
	}
	id, ok := blueID(w, r)
	if !ok {
		return
	}
	result, status, err := s.syncBlueEventSource(r.Context(), id, false)
	if err != nil {
		writeErr(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) syncBlueEventSource(ctx context.Context, id int64, automatic bool) (blueSourceSyncResult, int, error) {
	var result blueSourceSyncResult
	if !s.beginBlueSourceOperation(id) {
		return result, http.StatusConflict, errors.New("source synchronization is in progress")
	}
	defer s.endBlueSourceOperation(id)
	pg := s.m.pg
	if pg == nil {
		return result, http.StatusServiceUnavailable, errors.New("source database is unavailable")
	}
	source, err := pg.GetBlueEventSource(ctx, id)
	if err != nil {
		return result, http.StatusInternalServerError, errors.New("could not load source")
	}
	if source == nil {
		return result, http.StatusNotFound, errors.New("source not found")
	}
	// The poll list is only a scheduling snapshot. A pause applied after that
	// snapshot must prevent the automatic fetch, while manual sync remains usable.
	// The per-source operation guard also prevents a local PATCH from racing this
	// check and the subsequent request.
	if automatic && !source.Enabled {
		return result, http.StatusConflict, errors.New("source is paused")
	}
	events, nextCursor, err := fetchBlueSourceEvents(ctx, *source)
	if err != nil {
		_, recordErr := pg.RecordBlueEventSourceSync(ctx, id, source.Cursor, source.URL, source.UpdatedAt, source.Cursor, err.Error())
		if recordErr != nil {
			log.Printf("[blue-source] record error for source #%d: %v", id, recordErr)
		}
		return result, http.StatusBadGateway, err
	}
	var stored []db.BlueEvent
	if len(events) > 0 {
		stored, err = pg.ImportBlueEvents(ctx, events)
		if err != nil {
			log.Printf("[blue-source] import source #%d: %v", id, err)
			_, _ = pg.RecordBlueEventSourceSync(ctx, id, source.Cursor, source.URL, source.UpdatedAt, source.Cursor, "source event import failed")
			return result, http.StatusInternalServerError, errors.New("source event import failed")
		}
	}
	request := (&http.Request{}).WithContext(ctx)
	matched, created, detectionErrors, warnings := s.blueApplyEnabledRules(request, pg, stored)
	advanced, err := pg.RecordBlueEventSourceSync(ctx, id, source.Cursor, source.URL, source.UpdatedAt, nextCursor, "")
	if err != nil {
		log.Printf("[blue-source] update source #%d cursor: %v", id, err)
		return result, http.StatusInternalServerError, errors.New("could not save source cursor")
	}
	if !advanced {
		return result, http.StatusConflict, errors.New("source changed during synchronization; events were retained")
	}
	updated, err := pg.GetBlueEventSource(ctx, id)
	if err != nil || updated == nil {
		return result, http.StatusInternalServerError, errors.New("could not reload source")
	}
	result = blueSourceSyncResult{Source: updated, Processed: len(stored), AlertsMatched: matched,
		AlertsCreated: created, DetectionErrors: detectionErrors, Warnings: warnings}
	return result, http.StatusOK, nil
}

func fetchBlueSourceEvents(ctx context.Context, source db.BlueEventSource) ([]db.BlueEvent, string, error) {
	if err := validBlueSourceURL(source.URL); err != nil {
		return nil, "", err
	}
	if !validBlueSourceCursor(source.Cursor) {
		return nil, "", errors.New("stored source cursor is invalid")
	}
	u, _ := url.Parse(source.URL)
	query := u.Query()
	if source.Cursor != "" {
		query.Set("cursor", source.Cursor)
	}
	u.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", errors.New("could not create source request")
	}
	request.Header.Set("Accept", "application/x-ndjson")
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	defer transport.CloseIdleConnections()
	client := &http.Client{Timeout: blueSourceFetchTimeout, Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("source redirects are not allowed") }}
	response, err := client.Do(request)
	if err != nil {
		return nil, "", errors.New("source feed request failed or timed out")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("source feed returned HTTP %d", response.StatusCode)
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || (mediaType != "application/x-ndjson" && mediaType != "application/ndjson" &&
		mediaType != "application/jsonl" && mediaType != "application/json" && mediaType != "text/plain") {
		return nil, "", errors.New("source feed must return JSONL content type")
	}
	cursors := response.Header.Values("X-ARTEX-Next-Cursor")
	if len(cursors) != 1 || !validBlueSourceCursor(cursors[0]) {
		return nil, "", errors.New("source feed requires one valid X-ARTEX-Next-Cursor header")
	}
	if response.ContentLength > maxBlueBodyBytes {
		return nil, "", errors.New("source feed exceeds 8 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxBlueBodyBytes+1))
	if err != nil {
		return nil, "", errors.New("could not read source feed")
	}
	if len(data) > maxBlueBodyBytes {
		return nil, "", errors.New("source feed exceeds 8 MiB")
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, cursors[0], nil
	}
	stableSource := "pull:" + strconv.FormatInt(source.ID, 10)
	parsed, err := ingest.Parse(bytes.NewReader(data), "jsonl", ingest.Options{
		MaxBytes: maxBlueBodyBytes, MaxRecords: maxBlueImportItems,
		DefaultSource: stableSource,
	})
	if err != nil {
		return nil, "", fmt.Errorf("invalid source feed: %w", err)
	}
	urlHash := sha256.Sum256([]byte(source.URL))
	prefix := "pull:" + strconv.FormatInt(source.ID, 10) + ":" + hex.EncodeToString(urlHash[:6]) + ":"
	events := make([]db.BlueEvent, 0, len(parsed))
	for i, item := range parsed {
		if item.ExternalID == "" {
			return nil, "", fmt.Errorf("source feed event %d requires external_id for safe replay", i+1)
		}
		if len(item.Source) > 200 || len(item.EventType) > 200 || len(item.Asset) > 500 ||
			len(item.Message) > 10000 || !validBlueSeverity(item.Severity) {
			return nil, "", fmt.Errorf("source feed event %d has an invalid field", i+1)
		}
		externalID := prefix + item.ExternalID
		if len(externalID) > 500 {
			hash := sha256.Sum256([]byte(item.ExternalID))
			externalID = prefix + hex.EncodeToString(hash[:])
		}
		events = append(events, db.BlueEvent{
			Source: item.Source, ExternalID: externalID, EventType: item.EventType,
			Severity: item.Severity, Asset: item.Asset, Message: item.Message,
			OccurredAt: item.OccurredAt, Raw: item.Raw,
		})
	}
	return events, cursors[0], nil
}

func (s *Server) runBlueSourcePoller(ctx context.Context) {
	ticker := time.NewTicker(blueSourcePollTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.pollBlueSources(ctx)
		}
	}
}

func (s *Server) pollBlueSources(ctx context.Context) {
	if s.m.pg == nil {
		return
	}
	sources, err := s.m.pg.ListBlueEventSources(ctx)
	if err != nil {
		log.Printf("[blue-source] list sources: %v", err)
		return
	}
	now := time.Now().UTC()
	for _, source := range sources {
		if !source.Enabled {
			continue
		}
		lastAttempt := source.UpdatedAt
		if source.LastError == "" && source.LastSyncedAt != nil {
			lastAttempt = *source.LastSyncedAt
		}
		if now.Before(lastAttempt.Add(time.Duration(source.PollIntervalSeconds) * time.Second)) {
			continue
		}
		_, status, err := s.syncBlueEventSource(ctx, source.ID, true)
		if err != nil && status != http.StatusConflict && ctx.Err() == nil {
			log.Printf("[blue-source] source #%d sync: %v", source.ID, err)
		}
	}
}

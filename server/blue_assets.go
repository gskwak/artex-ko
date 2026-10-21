package server

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/Autumn-27/artex/db"
	"github.com/jackc/pgx/v5/pgconn"
)

// Blue assets are a defensive inventory. They never confer access to, or
// inherit the scope of, a red-team asset.
func (s *Server) registerBlueAssetRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/blue/assets", s.blueListAssets)
	mux.HandleFunc("POST /api/blue/assets", s.blueCreateAsset)
	mux.HandleFunc("GET /api/blue/assets/{id}", s.blueGetAsset)
	mux.HandleFunc("PATCH /api/blue/assets/{id}", s.bluePatchAsset)
	mux.HandleFunc("GET /api/blue/assets/{id}/activity", s.blueAssetActivity)
	mux.HandleFunc("GET /api/blue/cases/{id}/assets", s.blueListCaseAssets)
	mux.HandleFunc("POST /api/blue/cases/{id}/assets", s.blueLinkCaseAsset)
	mux.HandleFunc("DELETE /api/blue/cases/{id}/assets/{assetID}", s.blueUnlinkCaseAsset)
	mux.HandleFunc("PATCH /api/blue/events/{id}/asset", s.blueSetEventAsset)
}

func validBlueAssetKind(kind string) bool {
	switch kind {
	case "host", "ip", "domain", "service", "application", "other":
		return true
	}
	return false
}

func validBlueAssetStatus(status string) bool {
	return status == "active" || status == "archived"
}

func blueAssetUnique(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func blueAssetUnavailable(w http.ResponseWriter, err error) bool {
	if errors.Is(err, db.ErrBlueAssetUnavailable) {
		writeErr(w, http.StatusConflict, "blue-team asset is missing or archived")
		return true
	}
	if errors.Is(err, db.ErrBlueAssetNotInCase) {
		writeErr(w, http.StatusConflict, "blue-team asset is not linked to this case")
		return true
	}
	return false
}

func (s *Server) blueListAssets(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	limit, offset, ok := blueLimitOffset(w, r)
	if !ok {
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if len(query) > 200 || (status != "" && !validBlueAssetStatus(status)) {
		writeErr(w, http.StatusBadRequest, "invalid asset search or status")
		return
	}
	items, err := pg.ListBlueAssets(r.Context(), query, status, limit, offset)
	if err != nil {
		blueInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) blueCreateAsset(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	var req struct {
		Kind       string `json:"kind"`
		Identifier string `json:"identifier"`
		Label      string `json:"label"`
		Notes      string `json:"notes"`
	}
	if !decodeBlueJSON(w, r, &req) {
		return
	}
	req.Kind = strings.TrimSpace(req.Kind)
	req.Identifier = strings.TrimSpace(req.Identifier)
	req.Label = strings.TrimSpace(req.Label)
	if !validBlueAssetKind(req.Kind) || req.Identifier == "" || len(req.Identifier) > 500 ||
		req.Label == "" || len(req.Label) > 200 || len(req.Notes) > 10000 {
		writeErr(w, http.StatusBadRequest, "invalid asset kind, identifier, label or notes")
		return
	}
	item, err := pg.CreateBlueAsset(r.Context(), db.BlueAsset{
		Kind: req.Kind, Identifier: req.Identifier, Label: req.Label,
		Status: "active", Notes: req.Notes,
	})
	if blueAssetUnique(err) {
		writeErr(w, http.StatusConflict, "blue-team asset already exists")
		return
	}
	if err != nil {
		blueInternal(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) blueGetAsset(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id, ok := blueID(w, r)
	if !ok {
		return
	}
	item, err := pg.GetBlueAsset(r.Context(), id)
	if err != nil {
		blueInternal(w, err)
		return
	}
	if item == nil {
		writeErr(w, http.StatusNotFound, "blue-team asset not found")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) bluePatchAsset(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id, ok := blueID(w, r)
	if !ok {
		return
	}
	var patch db.BlueAssetPatch
	if !decodeBlueJSON(w, r, &patch) {
		return
	}
	if patch.Kind != nil {
		*patch.Kind = strings.TrimSpace(*patch.Kind)
		if !validBlueAssetKind(*patch.Kind) {
			writeErr(w, http.StatusBadRequest, "invalid asset kind")
			return
		}
	}
	if patch.Identifier != nil {
		*patch.Identifier = strings.TrimSpace(*patch.Identifier)
		if *patch.Identifier == "" || len(*patch.Identifier) > 500 {
			writeErr(w, http.StatusBadRequest, "invalid asset identifier")
			return
		}
	}
	if patch.Label != nil {
		*patch.Label = strings.TrimSpace(*patch.Label)
		if *patch.Label == "" || len(*patch.Label) > 200 {
			writeErr(w, http.StatusBadRequest, "invalid asset label")
			return
		}
	}
	if patch.Status != nil && !validBlueAssetStatus(*patch.Status) {
		writeErr(w, http.StatusBadRequest, "invalid asset status")
		return
	}
	if patch.Notes != nil && len(*patch.Notes) > 10000 {
		writeErr(w, http.StatusBadRequest, "asset notes are too long")
		return
	}
	item, err := pg.PatchBlueAsset(r.Context(), id, patch)
	if blueAssetUnique(err) {
		writeErr(w, http.StatusConflict, "blue-team asset already exists")
		return
	}
	if err != nil {
		blueInternal(w, err)
		return
	}
	if item == nil {
		writeErr(w, http.StatusNotFound, "blue-team asset not found")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) blueAssetActivity(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id, ok := blueID(w, r)
	if !ok {
		return
	}
	asset, err := pg.GetBlueAsset(r.Context(), id)
	if err != nil {
		blueInternal(w, err)
		return
	}
	if asset == nil {
		writeErr(w, http.StatusNotFound, "blue-team asset not found")
		return
	}
	activity, err := pg.GetBlueAssetActivity(r.Context(), id, 100)
	if err != nil {
		blueInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, activity)
}

func blueExistingCase(w http.ResponseWriter, r *http.Request, pg *db.DB) (int64, bool) {
	id, ok := blueID(w, r)
	if !ok {
		return 0, false
	}
	item, err := pg.GetBlueCase(r.Context(), id)
	if err != nil {
		blueInternal(w, err)
		return 0, false
	}
	if item == nil {
		writeErr(w, http.StatusNotFound, "case not found")
		return 0, false
	}
	return id, true
}

func (s *Server) blueListCaseAssets(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	caseID, ok := blueExistingCase(w, r, pg)
	if !ok {
		return
	}
	items, err := pg.ListBlueCaseAssets(r.Context(), caseID)
	if err != nil {
		blueInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) blueLinkCaseAsset(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	caseID, ok := blueExistingCase(w, r, pg)
	if !ok {
		return
	}
	var req struct {
		AssetID int64 `json:"asset_id"`
	}
	if !decodeBlueJSON(w, r, &req) {
		return
	}
	if req.AssetID <= 0 {
		writeErr(w, http.StatusBadRequest, "asset_id must be positive")
		return
	}
	if err := pg.LinkBlueCaseAsset(r.Context(), caseID, req.AssetID); err != nil {
		if blueAssetUnavailable(w, err) {
			return
		}
		if errors.Is(err, sql.ErrNoRows) {
			writeErr(w, http.StatusNotFound, "case not found")
			return
		}
		blueInternal(w, err)
		return
	}
	asset, err := pg.GetBlueAsset(r.Context(), req.AssetID)
	if err != nil {
		blueInternal(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, asset)
}

func (s *Server) blueUnlinkCaseAsset(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	caseID, ok := blueExistingCase(w, r, pg)
	if !ok {
		return
	}
	assetID, err := strconv.ParseInt(r.PathValue("assetID"), 10, 64)
	if err != nil || assetID <= 0 {
		writeErr(w, http.StatusBadRequest, "asset id must be positive")
		return
	}
	_, err = pg.UnlinkBlueCaseAsset(r.Context(), caseID, assetID)
	if err != nil {
		blueInternal(w, err)
		return
	}
	// Removing an explicit link is idempotent. An event-derived association may
	// still appear in the case asset list until that event is unlinked.
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) blueSetEventAsset(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	eventID, ok := blueID(w, r)
	if !ok {
		return
	}
	var req struct {
		AssetID json.RawMessage `json:"asset_id"`
	}
	if !decodeBlueJSON(w, r, &req) {
		return
	}
	if len(req.AssetID) == 0 {
		writeErr(w, http.StatusBadRequest, "asset_id is required; use null to unlink")
		return
	}
	var assetID *int64
	if !bytes.Equal(bytes.TrimSpace(req.AssetID), []byte("null")) {
		var id int64
		if err := json.Unmarshal(req.AssetID, &id); err != nil || id <= 0 {
			writeErr(w, http.StatusBadRequest, "asset_id must be a positive integer or null")
			return
		}
		assetID = &id
	}
	item, err := pg.SetBlueEventAsset(r.Context(), eventID, assetID)
	if blueAssetUnavailable(w, err) {
		return
	}
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "event not found")
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

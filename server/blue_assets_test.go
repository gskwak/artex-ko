package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/db"
)

func TestBlueAssetsRequireJWT(t *testing.T) {
	s := &Server{m: &Manager{}, jwtKey: []byte(strings.Repeat("a", 32))}
	h := s.Handler()
	for _, tc := range []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodGet, "/api/blue/assets", nil},
		{http.MethodPost, "/api/blue/assets", map[string]any{"kind": "host", "identifier": "fixture"}},
		{http.MethodGet, "/api/blue/assets/1", nil},
		{http.MethodPatch, "/api/blue/assets/1", map[string]any{"status": "archived"}},
		{http.MethodGet, "/api/blue/assets/1/activity", nil},
		{http.MethodGet, "/api/blue/cases/1/assets", nil},
		{http.MethodPost, "/api/blue/cases/1/assets", map[string]any{"asset_id": 1}},
		{http.MethodDelete, "/api/blue/cases/1/assets/1", nil},
		{http.MethodPatch, "/api/blue/events/1/asset", map[string]any{"asset_id": 1}},
	} {
		blueAPIRequest(t, h, "", tc.method, tc.path, tc.body, http.StatusUnauthorized, nil)
	}
}

func TestBlueAssetWorkflowPG(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ARTEX_BLUE_TEST_PG_DSN"))
	if dsn == "" {
		t.Skip("set ARTEX_BLUE_TEST_PG_DSN to a disposable PostgreSQL test database")
	}
	pg, err := db.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pg.Close() })
	s := &Server{m: &Manager{pg: pg}, jwtKey: []byte(strings.Repeat("a", 32))}
	h := s.Handler()
	token, err := signJWT(s.signingKey(s.readPassHash()))
	if err != nil {
		t.Fatal(err)
	}
	fixture := "blue-asset-api-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	var assetID, caseID, otherCaseID, eventID int64
	t.Cleanup(func() {
		ctx := context.Background()
		if eventID != 0 {
			_, _ = pg.ExecContext(ctx, `DELETE FROM blue_events WHERE id=$1`, eventID)
		}
		if caseID != 0 {
			_, _ = pg.ExecContext(ctx, `DELETE FROM blue_cases WHERE id=$1`, caseID)
		}
		if otherCaseID != 0 {
			_, _ = pg.ExecContext(ctx, `DELETE FROM blue_cases WHERE id=$1`, otherCaseID)
		}
		if assetID != 0 {
			_, _ = pg.ExecContext(ctx, `DELETE FROM blue_assets WHERE id=$1`, assetID)
		}
	})

	var asset db.BlueAsset
	blueAPIRequest(t, h, token, http.MethodPost, "/api/blue/assets", map[string]any{
		"kind": "host", "identifier": fixture, "label": "Lab host", "notes": "isolated fixture",
	}, http.StatusCreated, &asset)
	assetID = asset.ID
	if assetID <= 0 || asset.Status != "active" {
		t.Fatalf("created asset: %+v", asset)
	}
	blueAPIRequest(t, h, token, http.MethodPost, "/api/blue/assets", map[string]any{
		"kind": "host", "identifier": strings.ToUpper(fixture), "label": "Duplicate fixture",
	}, http.StatusConflict, nil)
	blueAPIRequest(t, h, token, http.MethodPost, "/api/blue/assets", map[string]any{
		"kind": "untrusted-kind", "identifier": fixture + "-2",
	}, http.StatusBadRequest, nil)
	blueAPIRequest(t, h, token, http.MethodPost, "/api/blue/assets", map[string]any{
		"kind": "host", "identifier": fixture + "-2", "label": "  ",
	}, http.StatusBadRequest, nil)
	blueAPIRequest(t, h, token, http.MethodPatch, fmt.Sprintf("/api/blue/assets/%d", assetID),
		map[string]any{"label": "\t "}, http.StatusBadRequest, nil)
	var assets []db.BlueAsset
	blueAPIRequest(t, h, token, http.MethodGet, "/api/blue/assets?q="+fixture, nil, http.StatusOK, &assets)
	if len(assets) != 1 || assets[0].ID != assetID {
		t.Fatalf("asset search = %+v", assets)
	}
	var caseItem db.BlueCase
	blueAPIRequest(t, h, token, http.MethodPost, "/api/blue/cases", map[string]any{
		"title": fixture + " case",
	}, http.StatusCreated, &caseItem)
	caseID = caseItem.ID
	var other db.BlueCase
	blueAPIRequest(t, h, token, http.MethodPost, "/api/blue/cases", map[string]any{
		"title": fixture + " other case",
	}, http.StatusCreated, &other)
	otherCaseID = other.ID
	casePath := fmt.Sprintf("/api/blue/cases/%d/assets", caseID)
	blueAPIRequest(t, h, token, http.MethodPost, casePath, map[string]any{"asset_id": assetID}, http.StatusCreated, &asset)
	var caseAssets []struct {
		db.BlueAsset
		ExplicitLink bool `json:"explicit_link"`
	}
	blueAPIRequest(t, h, token, http.MethodGet, casePath, nil, http.StatusOK, &caseAssets)
	if len(caseAssets) != 1 || caseAssets[0].ID != assetID || !caseAssets[0].ExplicitLink {
		t.Fatalf("case assets = %+v", caseAssets)
	}

	// An action may target only an asset attached to its own case.
	action := map[string]any{"kind": "isolate", "target": fixture, "rationale": "fixture test", "asset_id": assetID}
	blueAPIRequest(t, h, token, http.MethodPost, fmt.Sprintf("/api/blue/cases/%d/actions", otherCaseID), action, http.StatusConflict, nil)
	var actionItem db.BlueAction
	blueAPIRequest(t, h, token, http.MethodPost, fmt.Sprintf("/api/blue/cases/%d/actions", caseID), action, http.StatusCreated, &actionItem)
	if actionItem.AssetID == nil || *actionItem.AssetID != assetID {
		t.Fatalf("action asset = %+v", actionItem.AssetID)
	}

	var imported struct {
		Events []db.BlueEvent `json:"events"`
	}
	blueAPIRequest(t, h, token, http.MethodPost, "/api/blue/events/import", map[string]any{
		"events": []map[string]any{{
			"source": fixture, "external_id": "event-one", "event_type": "host.telemetry",
			"message": "fixture host event", "asset": "original-host-snapshot",
			"asset_id": assetID, "case_id": caseID,
		}},
	}, http.StatusCreated, &imported)
	if len(imported.Events) != 1 || imported.Events[0].AssetID == nil ||
		*imported.Events[0].AssetID != assetID || imported.Events[0].Asset != "original-host-snapshot" {
		t.Fatalf("imported events = %+v", imported.Events)
	}
	eventID = imported.Events[0].ID
	var activity db.BlueAssetActivity
	blueAPIRequest(t, h, token, http.MethodGet, fmt.Sprintf("/api/blue/assets/%d/activity", assetID), nil, http.StatusOK, &activity)
	if activity.CaseCount < 1 || activity.EventCount < 1 || activity.ActionCount < 1 {
		t.Fatalf("asset activity = %+v", activity)
	}

	// Removing the explicit link leaves the current event association intact.
	blueAPIRequest(t, h, token, http.MethodDelete, fmt.Sprintf("%s/%d", casePath, assetID), nil, http.StatusNoContent, nil)
	blueAPIRequest(t, h, token, http.MethodGet, casePath, nil, http.StatusOK, &caseAssets)
	if len(caseAssets) != 1 || caseAssets[0].ID != assetID || caseAssets[0].ExplicitLink {
		t.Fatalf("event-derived case asset flag incorrect: %+v", caseAssets)
	}
	var event db.BlueEvent
	blueAPIRequest(t, h, token, http.MethodPatch, fmt.Sprintf("/api/blue/events/%d/asset", eventID),
		map[string]any{"asset_id": nil}, http.StatusOK, &event)
	if event.AssetID != nil || event.Asset != "original-host-snapshot" {
		t.Fatalf("event unlink damaged snapshot: %+v", event)
	}
	blueAPIRequest(t, h, token, http.MethodGet, casePath, nil, http.StatusOK, &caseAssets)
	if len(caseAssets) != 0 {
		t.Fatalf("case asset remained after explicit and event links removed: %+v", caseAssets)
	}

	blueAPIRequest(t, h, token, http.MethodPatch, fmt.Sprintf("/api/blue/assets/%d", assetID),
		map[string]any{"status": "archived"}, http.StatusOK, &asset)
	if asset.Status != "archived" {
		t.Fatalf("archived asset status = %q", asset.Status)
	}
	blueAPIRequest(t, h, token, http.MethodPost, casePath, map[string]any{"asset_id": assetID}, http.StatusConflict, nil)
	blueAPIRequest(t, h, token, http.MethodPatch, fmt.Sprintf("/api/blue/events/%d/asset", eventID),
		map[string]any{"asset_id": assetID}, http.StatusConflict, nil)
	// A duplicate import returns the immutable original even if the asset has
	// since been archived; a genuinely new link to that asset is rejected.
	blueAPIRequest(t, h, token, http.MethodPost, "/api/blue/events/import", map[string]any{
		"events": []map[string]any{{
			"source": fixture, "external_id": "event-one", "event_type": "host.telemetry",
			"message": "changed replay", "asset_id": assetID,
		}},
	}, http.StatusCreated, &imported)
	if len(imported.Events) != 1 || imported.Events[0].ID != eventID || imported.Events[0].Asset != "original-host-snapshot" {
		t.Fatalf("archived replay changed original event: %+v", imported.Events)
	}
	blueAPIRequest(t, h, token, http.MethodPost, "/api/blue/events/import", map[string]any{
		"events": []map[string]any{{
			"source": fixture, "external_id": "new-after-archive", "event_type": "host.telemetry",
			"message": "new event", "asset_id": assetID,
		}},
	}, http.StatusConflict, nil)
	blueAPIRequest(t, h, token, http.MethodGet, fmt.Sprintf("/api/blue/assets/%d/activity", assetID), nil, http.StatusOK, &activity)
	if activity.ActionCount < 1 {
		t.Fatalf("archive removed action history: %+v", activity)
	}
}

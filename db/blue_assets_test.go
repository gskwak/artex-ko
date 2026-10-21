package db

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func openBlueAssetTestDB(t *testing.T) *DB {
	t.Helper()
	d, err := Open(testDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func TestBlueAssetIdentityAndArchive(t *testing.T) {
	d := openBlueAssetTestDB(t)
	ctx := context.Background()
	identifier := "host-" + uuid.NewString() + ".example.test"
	asset, err := d.CreateBlueAsset(ctx, BlueAsset{
		Kind: "host", Identifier: identifier, Label: "Initial host", Notes: "Defensive inventory",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = d.Exec(`DELETE FROM blue_assets WHERE id=$1`, asset.ID) })
	if asset.ID <= 0 || asset.Status != "active" || asset.CreatedAt.IsZero() || asset.UpdatedAt.IsZero() {
		t.Fatalf("unexpected asset defaults: %+v", asset)
	}
	if _, err := d.CreateBlueAsset(ctx, BlueAsset{Kind: "host", Identifier: strings.ToUpper(identifier), Label: "Duplicate"}); err == nil {
		t.Fatal("duplicate defensive identity was accepted")
	}
	got, err := d.GetBlueAsset(ctx, asset.ID)
	if err != nil || got == nil || got.Identifier != identifier {
		t.Fatalf("get asset: %+v, %v", got, err)
	}
	listed, err := d.ListBlueAssets(ctx, identifier, "active", 20, 0)
	if err != nil || len(listed) != 1 || listed[0].ID != asset.ID {
		t.Fatalf("search asset: %+v, %v", listed, err)
	}
	label, status := "Renamed host", "archived"
	archived, err := d.PatchBlueAsset(ctx, asset.ID, BlueAssetPatch{Label: &label, Status: &status})
	if err != nil || archived == nil || archived.Label != label || archived.Status != status || archived.ID != asset.ID {
		t.Fatalf("archive asset: %+v, %v", archived, err)
	}
	listed, err = d.ListBlueAssets(ctx, identifier, "active", 20, 0)
	if err != nil || len(listed) != 0 {
		t.Fatalf("archived asset still in active list: %+v, %v", listed, err)
	}
	listed, err = d.ListBlueAssets(ctx, identifier, "archived", 20, 0)
	if err != nil || len(listed) != 1 || listed[0].ID != asset.ID {
		t.Fatalf("archived asset missing from archive: %+v, %v", listed, err)
	}
	caseItem, err := d.CreateBlueCase(ctx, BlueCase{Title: "archive link " + uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = d.Exec(`DELETE FROM blue_cases WHERE id=$1`, caseItem.ID) })
	if err := d.LinkBlueCaseAsset(ctx, caseItem.ID, asset.ID); !errors.Is(err, ErrBlueAssetUnavailable) {
		t.Fatalf("archived asset must reject new case links, got %v", err)
	}
}

func TestBlueAssetCaseEventAndActionLinks(t *testing.T) {
	d := openBlueAssetTestDB(t)
	ctx := context.Background()
	key := uuid.NewString()
	first, err := d.CreateBlueAsset(ctx, BlueAsset{Kind: "host", Identifier: "first-" + key, Label: "First host"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := d.CreateBlueAsset(ctx, BlueAsset{Kind: "domain", Identifier: "second-" + key + ".example.test", Label: "Second host"})
	if err != nil {
		t.Fatal(err)
	}
	caseA, err := d.CreateBlueCase(ctx, BlueCase{Title: "asset case A " + key})
	if err != nil {
		t.Fatal(err)
	}
	caseB, err := d.CreateBlueCase(ctx, BlueCase{Title: "asset case B " + key})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = d.Exec(`DELETE FROM blue_actions WHERE case_id IN ($1,$2)`, caseA.ID, caseB.ID)
		_, _ = d.Exec(`DELETE FROM blue_events WHERE source=$1`, "blue-asset-test-"+key)
		_, _ = d.Exec(`DELETE FROM blue_case_assets WHERE case_id IN ($1,$2)`, caseA.ID, caseB.ID)
		_, _ = d.Exec(`DELETE FROM blue_cases WHERE id IN ($1,$2)`, caseA.ID, caseB.ID)
		_, _ = d.Exec(`DELETE FROM blue_assets WHERE id IN ($1,$2)`, first.ID, second.ID)
	})

	if err := d.LinkBlueCaseAsset(ctx, caseA.ID, second.ID); err != nil {
		t.Fatal(err)
	}
	if err := d.LinkBlueCaseAsset(ctx, caseA.ID, second.ID); err != nil {
		t.Fatalf("case asset linking should be idempotent: %v", err)
	}
	events, err := d.ImportBlueEvents(ctx, []BlueEvent{{
		CaseID: &caseA.ID, AssetID: &first.ID, Source: "blue-asset-test-" + key,
		ExternalID: "one", EventType: "network", Asset: "raw-host-name", Message: "observed traffic", OccurredAt: time.Now().UTC(),
	}})
	if err != nil || len(events) != 1 || events[0].AssetID == nil || *events[0].AssetID != first.ID {
		t.Fatalf("import linked event: %+v, %v", events, err)
	}
	if events[0].Asset != "raw-host-name" {
		t.Fatalf("source asset text was changed: %+v", events[0])
	}
	replayed, err := d.ImportBlueEvents(ctx, []BlueEvent{{
		CaseID: &caseB.ID, AssetID: &second.ID, Source: "blue-asset-test-" + key,
		ExternalID: "one", EventType: "network", Asset: "replayed-host", Message: "changed", OccurredAt: time.Now().UTC(),
	}})
	if err != nil || len(replayed) != 1 || replayed[0].ID != events[0].ID ||
		replayed[0].AssetID == nil || *replayed[0].AssetID != first.ID || replayed[0].Asset != "raw-host-name" {
		t.Fatalf("replay must preserve original attribution and raw text: %+v, %v", replayed, err)
	}
	caseAssets, err := d.ListBlueCaseAssets(ctx, caseA.ID)
	if err != nil || len(caseAssets) != 2 || !containsBlueAsset(caseAssets, first.ID) || !containsBlueAsset(caseAssets, second.ID) {
		t.Fatalf("case should include explicit and event assets: %+v, %v", caseAssets, err)
	}
	for _, item := range caseAssets {
		if item.ID == first.ID && item.ExplicitLink || item.ID == second.ID && !item.ExplicitLink {
			t.Fatalf("explicit and inferred links were confused: %+v", caseAssets)
		}
	}
	if _, err := d.MoveBlueEventCase(ctx, events[0].ID, caseB.ID); err != nil {
		t.Fatal(err)
	}
	caseAssets, err = d.ListBlueCaseAssets(ctx, caseA.ID)
	if err != nil || len(caseAssets) != 1 || caseAssets[0].ID != second.ID {
		t.Fatalf("moved event left stale case asset: %+v, %v", caseAssets, err)
	}
	caseAssets, err = d.ListBlueCaseAssets(ctx, caseB.ID)
	if err != nil || len(caseAssets) != 1 || caseAssets[0].ID != first.ID {
		t.Fatalf("destination case missing event asset: %+v, %v", caseAssets, err)
	}
	linked, err := d.BlueCaseHasAsset(ctx, caseA.ID, first.ID)
	if err != nil || linked {
		t.Fatalf("moved event must not grant source case association: %v, %v", linked, err)
	}
	linked, err = d.BlueCaseHasAsset(ctx, caseB.ID, first.ID)
	if err != nil || !linked {
		t.Fatalf("destination case should have event asset: %v, %v", linked, err)
	}
	action, err := d.CreateBlueAction(ctx, BlueAction{
		CaseID: caseA.ID, AssetID: &second.ID, Kind: "contain", Target: "raw-target-string", Rationale: "manual containment",
	})
	if err != nil || action == nil || action.AssetID == nil || *action.AssetID != second.ID || action.Target != "raw-target-string" {
		t.Fatalf("linked response action: %+v, %v", action, err)
	}
	if _, err := d.CreateBlueAction(ctx, BlueAction{
		CaseID: caseA.ID, AssetID: &first.ID, Kind: "contain", Target: "other-host", Rationale: "wrong case",
	}); !errors.Is(err, ErrBlueAssetNotInCase) {
		t.Fatalf("action must reject asset outside its case, got %v", err)
	}
	activity, err := d.GetBlueAssetActivity(ctx, second.ID, 20)
	if err != nil || activity == nil || activity.CaseCount != 1 || activity.ActionCount != 1 || activity.EventCount != 0 {
		t.Fatalf("asset activity: %+v, %v", activity, err)
	}
	if len(activity.Cases) != 1 || activity.Cases[0].ID != caseA.ID || len(activity.Actions) != 1 || activity.Actions[0].ID != action.ID {
		t.Fatalf("asset activity details: %+v", activity)
	}
	updated, err := d.SetBlueEventAsset(ctx, events[0].ID, &second.ID)
	if err != nil || updated == nil || updated.AssetID == nil || *updated.AssetID != second.ID || updated.Asset != "raw-host-name" {
		t.Fatalf("event attribution correction changed raw evidence: %+v, %v", updated, err)
	}
	updated, err = d.SetBlueEventAsset(ctx, events[0].ID, nil)
	if err != nil || updated == nil || updated.AssetID != nil || updated.Asset != "raw-host-name" {
		t.Fatalf("event unlink changed raw evidence: %+v, %v", updated, err)
	}
	removed, err := d.UnlinkBlueCaseAsset(ctx, caseA.ID, second.ID)
	if err != nil || !removed {
		t.Fatalf("unlink explicit case asset: %v, %v", removed, err)
	}
	removed, err = d.UnlinkBlueCaseAsset(ctx, caseA.ID, second.ID)
	if err != nil || removed {
		t.Fatalf("repeat unlink must be idempotent: %v, %v", removed, err)
	}
	linked, err = d.BlueCaseHasAsset(ctx, caseA.ID, second.ID)
	if err != nil || linked {
		t.Fatalf("historical action must not authorize a new action: %v, %v", linked, err)
	}
	if _, err := d.CreateBlueAction(ctx, BlueAction{
		CaseID: caseA.ID, AssetID: &second.ID, Kind: "contain", Target: "second follow-up", Rationale: "no current case link",
	}); !errors.Is(err, ErrBlueAssetNotInCase) {
		t.Fatalf("new action must reject historical-only asset, got %v", err)
	}
	caseAssets, err = d.ListBlueCaseAssets(ctx, caseA.ID)
	if err != nil || len(caseAssets) != 0 {
		t.Fatalf("historical action must not become a current case link: %+v, %v", caseAssets, err)
	}
	activity, err = d.GetBlueAssetActivity(ctx, second.ID, 20)
	if err != nil || activity == nil || activity.ActionCount != 1 || activity.CaseCount != 1 {
		t.Fatalf("historical action and case should remain in asset activity: %+v, %v", activity, err)
	}
}

func TestBlueAssetMigrationKeepsLegacyRows(t *testing.T) {
	d := openBlueAssetTestDB(t)
	ctx := context.Background()
	key := uuid.NewString()
	caseItem, err := d.CreateBlueCase(ctx, BlueCase{Title: "legacy asset case " + key})
	if err != nil {
		t.Fatal(err)
	}
	events, err := d.ImportBlueEvents(ctx, []BlueEvent{{
		CaseID: &caseItem.ID, Source: "blue-asset-legacy-" + key, ExternalID: "one",
		EventType: "auth", Asset: "legacy-string-only", Message: "old event", OccurredAt: time.Now().UTC(),
	}})
	if err != nil || len(events) != 1 || events[0].AssetID != nil {
		t.Fatalf("legacy event import: %+v, %v", events, err)
	}
	action, err := d.CreateBlueAction(ctx, BlueAction{CaseID: caseItem.ID, Kind: "review", Target: "legacy-target", Rationale: "legacy action"})
	if err != nil || action == nil || action.AssetID != nil {
		t.Fatalf("legacy action create: %+v, %v", action, err)
	}
	t.Cleanup(func() {
		_, _ = d.Exec(`DELETE FROM blue_actions WHERE id=$1`, action.ID)
		_, _ = d.Exec(`DELETE FROM blue_events WHERE id=$1`, events[0].ID)
		_, _ = d.Exec(`DELETE FROM blue_cases WHERE id=$1`, caseItem.ID)
	})
	reopened, err := Open(testDSN(t))
	if err != nil {
		t.Fatalf("schema must be repeatable: %v", err)
	}
	defer reopened.Close()
	storedEvent, err := reopened.GetBlueEvent(ctx, events[0].ID)
	if err != nil || storedEvent == nil || storedEvent.AssetID != nil || storedEvent.Asset != "legacy-string-only" {
		t.Fatalf("schema replay changed legacy event: %+v, %v", storedEvent, err)
	}
	storedAction, err := reopened.GetBlueAction(ctx, action.ID)
	if err != nil || storedAction == nil || storedAction.AssetID != nil || storedAction.Target != "legacy-target" {
		t.Fatalf("schema replay changed legacy action: %+v, %v", storedAction, err)
	}
}

func containsBlueAsset(items []BlueCaseAsset, id int64) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}

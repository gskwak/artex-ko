package db

import "testing"

// TestTranslateLegacyBuiltinRuleText checks that startup translation updates
// the original seed text once while leaving user edits and unrelated rules alone.
func TestTranslateLegacyBuiltinRuleText(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable (%v) — skipping", err)
	}
	defer d.Close()

	var interceptIDs, assetIDs []int64
	defer func() {
		for _, id := range interceptIDs {
			if _, err := d.Exec(`DELETE FROM intercept_rules WHERE id=$1`, id); err != nil {
				t.Errorf("clean up intercept rule %d: %v", id, err)
			}
		}
		for _, id := range assetIDs {
			if _, err := d.Exec(`DELETE FROM asset_intercept_rules WHERE id=$1`, id); err != nil {
				t.Errorf("clean up asset intercept rule %d: %v", id, err)
			}
		}
	}()

	intercept := legacyBuiltinInterceptRuleText[0]
	insertIntercept := func(name, message, pattern string) int64 {
		t.Helper()
		var id int64
		err := d.QueryRow(`
INSERT INTO intercept_rules(name, match_target, match_type, pattern, action, message)
VALUES($1, 'tool_input', 'regex', $2, 'deny', $3) RETURNING id`, name, pattern, message).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		interceptIDs = append(interceptIDs, id)
		return id
	}
	originalID := insertIntercept(intercept.oldName, intercept.oldMessage, intercept.pattern)
	customID := insertIntercept("User-edited name", "User-edited message", intercept.pattern)
	otherPatternID := insertIntercept(intercept.oldName, intercept.oldMessage, intercept.pattern+"(?:custom)")

	asset := legacyBuiltinAssetRuleNotes[0]
	insertAsset := func(note string, builtin bool) int64 {
		t.Helper()
		var id int64
		err := d.QueryRow(`
INSERT INTO asset_intercept_rules(kind, pattern, note, builtin)
VALUES('fuzzy_domain', $1, $2, $3) RETURNING id`, asset.pattern, note, builtin).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		assetIDs = append(assetIDs, id)
		return id
	}
	assetOriginalID := insertAsset(asset.oldNote, true)
	assetCustomID := insertAsset("User-edited note", true)
	assetNonBuiltinID := insertAsset(asset.oldNote, false)

	for run := 1; run <= 2; run++ {
		if err := d.translateLegacyBuiltinRuleText(); err != nil {
			t.Fatalf("translation run %d: %v", run, err)
		}
		for _, tc := range []struct {
			id                int64
			wantName, wantMsg string
		}{
			{originalID, intercept.newName, intercept.newMessage},
			{customID, "User-edited name", "User-edited message"},
			{otherPatternID, intercept.oldName, intercept.oldMessage},
		} {
			var name, message string
			if err := d.QueryRow(`SELECT name, message FROM intercept_rules WHERE id=$1`, tc.id).Scan(&name, &message); err != nil {
				t.Fatal(err)
			}
			if name != tc.wantName || message != tc.wantMsg {
				t.Errorf("run %d intercept rule %d: got name=%q message=%q, want name=%q message=%q",
					run, tc.id, name, message, tc.wantName, tc.wantMsg)
			}
		}
		for _, tc := range []struct {
			id       int64
			wantNote string
		}{
			{assetOriginalID, asset.newNote},
			{assetCustomID, "User-edited note"},
			{assetNonBuiltinID, asset.oldNote},
		} {
			var note string
			if err := d.QueryRow(`SELECT note FROM asset_intercept_rules WHERE id=$1`, tc.id).Scan(&note); err != nil {
				t.Fatal(err)
			}
			if note != tc.wantNote {
				t.Errorf("run %d asset rule %d: got note=%q, want %q", run, tc.id, note, tc.wantNote)
			}
		}
	}
}

package db

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"testing"
	"time"
)

// Compressed fixtures contain exact previously shipped defaults used only to
// verify migrations. Keeping them as binary fixtures prevents the obsolete
// localized prose from reappearing in the application's source and UI.
func legacyTextFixture(t *testing.T, name string) string {
	t.Helper()
	file, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestLegacyHostToolDescriptionVariant(t *testing.T) {
	old := legacyTextFixture(t, "legacy_traffic_search_description.gz")
	hashes := legacyHostToolTextHashes["traffic_search"]
	if len(hashes.alternateDescriptions) != 1 ||
		!matchesLegacyText(old, hashes.alternateDescriptions[0]) {
		t.Fatal("legacy traffic_search description hash does not match the previous default")
	}
}

func TestMigrateLegacyReporterPrompt(t *testing.T) {
	old := legacyTextFixture(t, "legacy_reporter_prompt.gz")
	if !matchesLegacyText(old, legacyStockPromptHashes["reporter"]) {
		t.Fatal("legacy reporter prompt hash does not match the committed default")
	}
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	key := fmt.Sprintf("reporter_migration_%d", time.Now().UnixNano())
	agent, err := d.CreateAgent(key, "Reporter migration test", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.DeleteAgent(key) })
	if err := d.SeedPromptIfEmpty(agent.ID, old); err != nil {
		t.Fatal(err)
	}
	if err := d.migrateLegacyStockPrompt(agent.ID, legacyStockPromptHashes["reporter"], "English reporter default"); err != nil {
		t.Fatal(err)
	}
	versions, err := d.ListPromptVersions(agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 2 || versions[0].Template != "English reporter default" || versions[1].Template != old {
		t.Fatalf("unexpected reporter prompt history: %+v", versions)
	}
}

func TestMigrateLegacyToolSchemaTextPreservesEdits(t *testing.T) {
	const oldDSL = "DSL \u67e5\u8be2\u8868\u8fbe\u5f0f\uff08\u8bed\u6cd5/\u5b57\u6bb5\u89c1\u5de5\u5177\u63cf\u8ff0\uff09\u3002\u672a\u4f20 id/ids \u65f6\u5fc5\u987b\u975e\u7a7a\u3002"
	if !matchesLegacyText(oldDSL, legacyToolTextHashes["list_assets"].fields["/properties/dsl/description"]) {
		t.Fatal("legacy tool default hash does not match the previous release")
	}
	stored := map[string]any{"properties": map[string]any{
		"dsl":   map[string]any{"description": oldDSL},
		"limit": map[string]any{"description": "User-edited limit help"},
	}}
	fresh := map[string]any{"properties": map[string]any{
		"dsl":   map[string]any{"description": "New DSL help"},
		"limit": map[string]any{"description": "New limit help"},
	}}
	if !migrateLegacySchemaText(stored, fresh, "", legacyToolTextHashes["list_assets"].fields) {
		t.Fatal("expected the untouched DSL help to migrate")
	}
	props := stored["properties"].(map[string]any)
	if got := props["dsl"].(map[string]any)["description"]; got != "New DSL help" {
		t.Fatalf("DSL help = %q", got)
	}
	if got := props["limit"].(map[string]any)["description"]; got != "User-edited limit help" {
		t.Fatalf("user-edited help was replaced: %q", got)
	}
	if migrateLegacySchemaText(stored, fresh, "", legacyToolTextHashes["list_assets"].fields) {
		t.Fatal("second migration changed schema again")
	}
}

func TestSeedToolMigratesOnlyUntouchedText(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	key := fmt.Sprintf("tool_migration_%d", time.Now().UnixNano())
	t.Cleanup(func() { _, _ = d.Exec(`DELETE FROM tools WHERE key=$1`, key) })
	legacyToolTextHashes[key] = legacyToolTextHash{
		description: fmt.Sprintf("%x", sha256.Sum256([]byte("Old stock description"))),
		fields: map[string]string{
			"/properties/a/description": fmt.Sprintf("%x", sha256.Sum256([]byte("Old stock help"))),
			"/properties/b/description": fmt.Sprintf("%x", sha256.Sum256([]byte("Old stock help B"))),
		},
	}
	t.Cleanup(func() { delete(legacyToolTextHashes, key) })
	oldSchema := json.RawMessage(`{"type":"object","properties":{"a":{"description":"Old stock help"},"b":{"description":"User-edited help"}}}`)
	newSchema := json.RawMessage(`{"type":"object","properties":{"a":{"description":"New English help"},"b":{"description":"New default help"}}}`)
	if _, err := d.Exec(`INSERT INTO tools(key,system,description,schema,agents,enabled)
VALUES ($1,true,$2,$3,'["worker"]',false)`, key, "Old stock description", oldSchema); err != nil {
		t.Fatal(err)
	}
	if err := d.SeedTool(key, "New English description", newSchema, json.RawMessage(`["planner"]`)); err != nil {
		t.Fatal(err)
	}
	tool, err := d.GetTool(key)
	if err != nil {
		t.Fatal(err)
	}
	if tool.Description != "New English description" || tool.Enabled || len(tool.Agents) != 1 || tool.Agents[0] != "worker" {
		t.Fatalf("tool migration changed more than stock text: %+v", tool)
	}
	var schema map[string]any
	if err := json.Unmarshal(tool.Schema, &schema); err != nil {
		t.Fatal(err)
	}
	props := schema["properties"].(map[string]any)
	if props["a"].(map[string]any)["description"] != "New English help" ||
		props["b"].(map[string]any)["description"] != "User-edited help" {
		t.Fatalf("schema migration did not preserve the edited field: %s", tool.Schema)
	}
	if err := d.SeedTool(key, "New English description", newSchema, json.RawMessage(`["planner"]`)); err != nil {
		t.Fatal(err)
	}
	tool, err = d.GetTool(key)
	if err != nil || tool.Description != "New English description" {
		t.Fatalf("second seed changed tool: %+v, %v", tool, err)
	}
	// A prior traffic_search default had another exact Chinese description.
	// It must migrate through the alternate hash without disturbing the schema.
	hashes := legacyToolTextHashes[key]
	hashes.alternateDescriptions = legacyHostToolTextHashes["traffic_search"].alternateDescriptions
	legacyToolTextHashes[key] = hashes
	if _, err := d.Exec(`UPDATE tools SET description=$2 WHERE key=$1`,
		key, legacyTextFixture(t, "legacy_traffic_search_description.gz")); err != nil {
		t.Fatal(err)
	}
	if err := d.SeedTool(key, "New English description", newSchema, json.RawMessage(`["planner"]`)); err != nil {
		t.Fatal(err)
	}
	tool, err = d.GetTool(key)
	if err != nil || tool.Description != "New English description" {
		t.Fatalf("alternate stock description was not migrated: %+v, %v", tool, err)
	}
}

func TestMigrateLegacyCustomStarterPrompt(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	const oldStarter = "\u4f60\u662f\u4e00\u4e2a\u4e50\u4e8e\u52a9\u4eba\u7684 AI \u52a9\u624b\u3002\u8bf7\u7528\u7b80\u6d01\u3001\u51c6\u786e\u7684\u4e2d\u6587\u56de\u7b54\u7528\u6237\u7684\u95ee\u9898\uff1b\u5728\u9700\u8981\u65f6\u4f7f\u7528\u53ef\u7528\u7684\u5de5\u5177\u6765\u5b8c\u6210\u4efb\u52a1\u3002\u53ea\u505a\u7528\u6237\u8981\u6c42\u7684\u4e8b\uff0c\u4e0d\u81c6\u9020\u4fe1\u606f\u3002"
	if !matchesLegacyText(oldStarter, legacyStockPromptHashes["_assistant"]) {
		t.Fatal("legacy starter hash does not match the previous release")
	}
	key := fmt.Sprintf("starter_migration_%d", time.Now().UnixNano())
	agent, err := d.CreateAgent(key, "Starter migration test", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.DeleteAgent(key) })
	if err := d.SeedPromptIfEmpty(agent.ID, oldStarter); err != nil {
		t.Fatal(err)
	}
	defaults := map[string]string{"_assistant": "New English starter"}
	if err := d.MigrateLegacyStockPrompts(defaults); err != nil {
		t.Fatal(err)
	}
	versions, err := d.ListPromptVersions(agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 2 || versions[0].Template != defaults["_assistant"] || versions[1].Template != oldStarter {
		t.Fatalf("unexpected prompt versions after migration: %+v", versions)
	}
	if err := d.MigrateLegacyStockPrompts(defaults); err != nil {
		t.Fatal(err)
	}
	versions, err = d.ListPromptVersions(agent.ID)
	if err != nil || len(versions) != 2 {
		t.Fatalf("migration must be idempotent: versions=%d err=%v", len(versions), err)
	}
	if _, err := d.SavePrompt(agent.ID, "User-edited prompt", "", "test"); err != nil {
		t.Fatal(err)
	}
	if err := d.MigrateLegacyStockPrompts(defaults); err != nil {
		t.Fatal(err)
	}
	current, err := d.CurrentPrompt(agent.ID)
	if err != nil || current != "User-edited prompt" {
		t.Fatalf("user edit was replaced: prompt=%q err=%v", current, err)
	}
}

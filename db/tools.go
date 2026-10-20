package db

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"strconv"
)

// Tool is one row of the built-in tool catalog. key + handler live in code; this
// row carries only the page-editable surface: description, parameter schema
// (structure read-only, per-param description/default editable), agent binding,
// and the on/off switch. See schema.sql §H and agent/toolcatalog.go.
type Tool struct {
	Key         string          `json:"key"`
	System      bool            `json:"system"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"schema"`
	Agents      []string        `json:"agents"`
	Enabled     bool            `json:"enabled"`
	Kind        string          `json:"kind"`     // builtin | command | script | http
	Exec        json.RawMessage `json:"exec"`     // Execution specification for a custom tool (kind != builtin).
	Deferred    bool            `json:"deferred"` // Schema loaded on demand via SearchExtraTools/ExecuteExtraTool.
	Calls       int             `json:"calls"`    // runtime ledger aggregate; not stored in tools
}

const toolCols = `key, system, description, schema, agents, enabled, kind, exec, deferred`

// SeedTool inserts a built-in tool's code-defined defaults ONCE. ON CONFLICT DO
// NOTHING: an existing row (possibly edited in the UI) is never overwritten on
// startup — that's what keeps page edits from being wiped every restart. Use
// UpsertToolForce for an explicit "reset to code default".
func (d *DB) SeedTool(key, desc string, schema, agents json.RawMessage) error {
	if len(schema) == 0 {
		schema = json.RawMessage("{}")
	}
	if len(agents) == 0 {
		agents = json.RawMessage("[]")
	}
	_, err := d.Exec(`
INSERT INTO tools(key, system, description, schema, agents, enabled)
VALUES ($1, true, $2, $3, $4, true)
ON CONFLICT (key) DO NOTHING`, key, desc, schema, agents)
	if err != nil {
		return err
	}
	return d.migrateLegacyToolText(key, desc, schema)
}

// migrateLegacyToolText replaces only fields that still equal the former Chinese
// built-in defaults. Tool descriptions and schema help are editable in the UI,
// so each field is checked independently; bindings, enablement, and other schema
// properties are left as stored. Hashes identify the old stock text without
// carrying obsolete localized prose in the source.
func (d *DB) migrateLegacyToolText(key, desc string, schema json.RawMessage) error {
	old, known := legacyToolTextHashes[key]
	if !known {
		old, known = legacyHostToolTextHashes[key]
	}
	if !known {
		return nil
	}
	var system bool
	var currentDesc string
	var currentSchema json.RawMessage
	if err := d.QueryRow(`SELECT system, description, schema FROM tools WHERE key=$1`, key).
		Scan(&system, &currentDesc, &currentSchema); err != nil {
		return err
	}
	if !system {
		return nil
	}

	nextDesc := currentDesc
	stockDescription := old.description != "" && matchesLegacyText(currentDesc, old.description)
	for _, digest := range old.alternateDescriptions {
		stockDescription = stockDescription || matchesLegacyText(currentDesc, digest)
	}
	if stockDescription {
		nextDesc = desc
	}
	var stored, fresh any
	if err := json.Unmarshal(currentSchema, &stored); err != nil {
		return err
	}
	if err := json.Unmarshal(schema, &fresh); err != nil {
		return err
	}
	schemaChanged := migrateLegacySchemaText(stored, fresh, "", old.fields)
	if nextDesc == currentDesc && !schemaChanged {
		return nil
	}
	nextSchema := currentSchema
	if schemaChanged {
		var err error
		nextSchema, err = json.Marshal(stored)
		if err != nil {
			return err
		}
	}
	// The old-value predicate also protects edits made between the read and update.
	_, err := d.Exec(`UPDATE tools SET description=$2, schema=$3, updated_at=now()
WHERE key=$1 AND system AND description=$4 AND schema=$5`,
		key, nextDesc, nextSchema, currentDesc, currentSchema)
	return err
}

func matchesLegacyText(value, digest string) bool {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:]) == digest
}

func migrateLegacySchemaText(stored, fresh any, path string, hashes map[string]string) bool {
	switch old := stored.(type) {
	case map[string]any:
		defaults, ok := fresh.(map[string]any)
		if !ok {
			return false
		}
		changed := false
		for key, value := range old {
			newValue, exists := defaults[key]
			if !exists {
				continue
			}
			fieldPath := path + "/" + key
			if text, ok := value.(string); ok {
				if newText, ok := newValue.(string); ok && newText != text && matchesLegacyText(text, hashes[fieldPath]) {
					old[key] = newText
					changed = true
				}
				continue
			}
			if migrateLegacySchemaText(value, newValue, fieldPath, hashes) {
				changed = true
			}
		}
		return changed
	case []any:
		defaults, ok := fresh.([]any)
		if !ok {
			return false
		}
		changed := false
		for i := 0; i < len(old) && i < len(defaults); i++ {
			fieldPath := path + "/" + strconv.Itoa(i)
			if text, ok := old[i].(string); ok {
				if newText, ok := defaults[i].(string); ok && newText != text && matchesLegacyText(text, hashes[fieldPath]) {
					old[i] = newText
					changed = true
				}
				continue
			}
			if migrateLegacySchemaText(old[i], defaults[i], fieldPath, hashes) {
				changed = true
			}
		}
		return changed
	default:
		return false
	}
}

// AddAgentToToolBinding adds an agent key to the given tools' `agents` arrays if
// not already present (idempotent). Used to give the built-in Auto agent its
// default toolset on existing DBs.
func (d *DB) AddAgentToToolBinding(agentKey string, keys []string) error {
	for _, k := range keys {
		if _, err := d.Exec(`UPDATE tools SET agents = agents || to_jsonb($1::text) WHERE key=$2 AND NOT (agents ? $1)`, agentKey, k); err != nil {
			return err
		}
	}
	return nil
}

// RemoveAgentFromToolBindings strips an agent key from every tool's `agents`
// JSONB array — called when a custom agent is deleted so no tool keeps a dangling
// binding. Uses jsonb `-` (remove array element) guarded by `?` (membership).
func (d *DB) RemoveAgentFromToolBindings(agentKey string) error {
	_, err := d.Exec(`UPDATE tools SET agents = agents - $1 WHERE agents ? $1`, agentKey)
	return err
}

// RemoveAgentFromTool strips one agent key from a SINGLE tool's `agents` array —
// used by one-time migrations that change a tool's default binding on existing DBs
// (SeedTool is first-insert-only, so a changed default never reaches a seeded row).
func (d *DB) RemoveAgentFromTool(agentKey, toolKey string) error {
	_, err := d.Exec(`UPDATE tools SET agents = agents - $1 WHERE key=$2 AND agents ? $1`, agentKey, toolKey)
	return err
}

// UpsertToolForce overwrites a tool row with the given code-default values (used by
// the per-tool "reset" action). It resets description/schema/agents and re-enables
// the tool, but keeps system=true.
func (d *DB) UpsertToolForce(key, desc string, schema, agents json.RawMessage) error {
	if len(schema) == 0 {
		schema = json.RawMessage("{}")
	}
	if len(agents) == 0 {
		agents = json.RawMessage("[]")
	}
	_, err := d.Exec(`
INSERT INTO tools(key, system, description, schema, agents, enabled)
VALUES ($1, true, $2, $3, $4, true)
ON CONFLICT (key) DO UPDATE
  SET description = EXCLUDED.description,
      schema      = EXCLUDED.schema,
      agents      = EXCLUDED.agents,
      enabled     = true`, key, desc, schema, agents)
	return err
}

// RefreshToolDefaults updates a system tool's model-facing description + schema to the
// code defaults, PRESERVING the user's agent binding + enabled flag. Used by one-time
// migrations to propagate a code schema change (SeedTool is first-insert-only, so a new
// parameter added in code otherwise never reaches an already-seeded row). No-op for
// custom tools or unknown keys.
func (d *DB) RefreshToolDefaults(key, desc string, schema json.RawMessage) error {
	if len(schema) == 0 {
		schema = json.RawMessage("{}")
	}
	_, err := d.Exec(`UPDATE tools SET description=$2, schema=$3, updated_at=now() WHERE key=$1 AND system`, key, desc, schema)
	return err
}

func scanTool(rows interface{ Scan(...any) error }) (*Tool, error) {
	var t Tool
	var agents []byte
	if err := rows.Scan(&t.Key, &t.System, &t.Description, &t.Schema, &agents, &t.Enabled, &t.Kind, &t.Exec, &t.Deferred); err != nil {
		return nil, err
	}
	if len(agents) > 0 {
		_ = json.Unmarshal(agents, &t.Agents)
	}
	if t.Agents == nil {
		t.Agents = []string{}
	}
	return &t, nil
}

// ListTools returns the whole tool catalog, ordered by key.
func (d *DB) ListTools() ([]*Tool, error) {
	rows, err := d.Query(`SELECT ` + toolCols + ` FROM tools ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Tool
	for rows.Next() {
		t, err := scanTool(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// GetTool fetches one tool row (nil if absent).
func (d *DB) GetTool(key string) (*Tool, error) {
	row := d.QueryRow(`SELECT `+toolCols+` FROM tools WHERE key=$1`, key)
	t, err := scanTool(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return t, err
}

// CreateCustomTool inserts a user-defined tool (system=false) with an execution
// spec. Fails if the key already exists.
func (d *DB) CreateCustomTool(t *Tool) error {
	schema := t.Schema
	if len(schema) == 0 {
		schema = json.RawMessage("{}")
	}
	exec := t.Exec
	if len(exec) == 0 {
		exec = json.RawMessage("{}")
	}
	agents, _ := json.Marshal(t.Agents)
	if len(agents) == 0 {
		agents = json.RawMessage("[]")
	}
	_, err := d.Exec(`
INSERT INTO tools(key, system, description, schema, agents, enabled, kind, exec, deferred)
VALUES ($1, false, $2, $3, $4, $5, $6, $7, $8)`,
		t.Key, t.Description, schema, agents, t.Enabled, t.Kind, exec, t.Deferred)
	return err
}

// UpdateCustomTool updates a custom tool's editable fields (kind/exec/deferred +
// desc/schema/agents/enabled). Only touches system=false rows.
func (d *DB) UpdateCustomTool(t *Tool) error {
	schema := t.Schema
	if len(schema) == 0 {
		schema = json.RawMessage("{}")
	}
	exec := t.Exec
	if len(exec) == 0 {
		exec = json.RawMessage("{}")
	}
	agents, _ := json.Marshal(t.Agents)
	if len(agents) == 0 {
		agents = json.RawMessage("[]")
	}
	_, err := d.Exec(`
UPDATE tools SET description=$2, schema=$3, agents=$4, enabled=$5, kind=$6, exec=$7, deferred=$8
WHERE key=$1 AND system=false`,
		t.Key, t.Description, schema, agents, t.Enabled, t.Kind, exec, t.Deferred)
	return err
}

// DeleteCustomTool removes a custom tool (system=false only; built-ins protected).
func (d *DB) DeleteCustomTool(key string) error {
	_, err := d.Exec(`DELETE FROM tools WHERE key=$1 AND system=false`, key)
	return err
}

// ListCustomTools returns only the user-defined (system=false) tools.
func (d *DB) ListCustomTools() ([]*Tool, error) {
	rows, err := d.Query(`SELECT ` + toolCols + ` FROM tools WHERE system=false ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Tool
	for rows.Next() {
		t, err := scanTool(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// UpdateTool saves the page-editable fields. key is never changed (it is welded to
// the Go handler). system tools: the caller must keep the schema structure — only
// per-param description/default and the agent binding are meant to move.
func (d *DB) UpdateTool(key, desc string, schema, agents json.RawMessage, enabled bool) error {
	if len(schema) == 0 {
		schema = json.RawMessage("{}")
	}
	if len(agents) == 0 {
		agents = json.RawMessage("[]")
	}
	_, err := d.Exec(`
UPDATE tools SET description=$2, schema=$3, agents=$4, enabled=$5 WHERE key=$1`,
		key, desc, schema, agents, enabled)
	return err
}

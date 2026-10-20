package agent

import (
	"context"
	"encoding/json"

	actool "github.com/Autumn-27/norma/tool"
)

// This file turns built-in tools into an enumerable catalog with DB overrides:
//   - BuiltinToolSeeds() expands the built-in tool sets for execution agents into
//     seed records (key, description, parameter schema, and default agent bindings)
//     for idempotent insertion into the tools table at server startup.
//   - The ToolResolve hook filters assembled tools by agent, overrides their
//     description/schema, and injects parameter defaults using the DB tools rows.
// Tool keys and handlers remain in code. The DB can change only the model-facing
// descriptions and default arguments, never the Call behavior.

// ToolSeed is a seedable snapshot of a built-in tool. Key is CoreTool.Name() and
// is bound to its handler (read-only in the UI). Desc and Schema come from the
// tool definition in code; Agents lists the agent keys bound by default.
type ToolSeed struct {
	Key    string         // = CoreTool.Name(); immutable primary key
	Desc   string         // Top-level description, overridable in the UI
	Schema map[string]any // Parameter JSON Schema; structure read-only, description/default editable in the UI
	Agents []string       // Agent keys bound by default (worker/planner/mainagent)
}

// builtinToolsByAgent constructs each execution agent's domain tool set with a
// read-only ToolSet shell (nil stores). Tool constructors only place closures in
// Spec and do not dereference stores during construction, so nil is safe. These
// tools are used only for Name(), Description(), and InputSchema(), never Call().
//
// SDK general-purpose tools from actool.DefaultTools() (Read/Write/Edit/
// MultiEdit/LS/Glob/Grep/Bash) are intentionally excluded. Every agent always
// has them, so there is no binding choice, and most of their instructions are in
// Prompt(). Overriding only Description() would be misleading. With no seed or
// DB row, ToolResolve passes them through unchanged. Only Artex domain tools
// enter the managed catalog.
func builtinToolsByAgent() map[string][]actool.CoreTool {
	ts := NewToolSet(nil, "")
	return map[string][]actool.CoreTool{
		"mainagent": ts.MainAgentTools(),
		"planner":   ts.PlannerTools(),
		"worker":    ts.WorkerTools(),
		// The goals agent binds set_goals and set_constraints by default to
		// persist decomposed goals and extracted operating constraints. It shares
		// these managed tools with mainagent; the UI can edit descriptions/schemas
		// and select agent bindings.
		"goals": {ts.setGoals(), ts.setConstraints()},
		// auto binds finding-reporting and asset-management tools by default.
		// Other domain tools can be enabled in the UI. New DBs use this seed;
		// existing DBs are migrated by seedAutoDefaultBindings.
		"auto": {ts.addFinding(), ts.insertAssets(), ts.addCompanyScope(), ts.listAssets(), ts.listCompanies()},
		// pentest binds asset lookup/insertion, finding reporting/lookup, and
		// company lookup by default. New DBs use this seed; existing DBs are
		// migrated by seedPentestDefaultBindings.
		"pentest": {ts.listAssets(), ts.insertAssets(), ts.addFinding(), ts.listFindings(), ts.listCompanies()},
	}
}

// defaultUnbound lists system tools that appear in the catalog and can be
// manually bound in the UI, but have no agent bindings by default. ToolResolve
// drops an unbound tool for every agent until it is explicitly enabled. Such a
// tool remains in an agent's base tool set (for example, goal_met in
// PlannerTools) so seed creation can read its description/schema and the tool
// is available at runtime if a user later binds it.
//
// goal_met declares the entire task complete without proving each goal. This
// has a high impact if called incorrectly and duplicates the automatic
// completion triggered when prove_goal marks the final goal, so it has no
// default binding and must be enabled manually when needed.
var defaultUnbound = map[string]bool{"goal_met": true}

// BuiltinToolSeeds deduplicates the agents' built-in tool sets into seed
// records. Tools shared by agents (such as list_assets) become one record with
// the union of agent bindings. Tools in defaultUnbound get an empty binding.
func BuiltinToolSeeds() []ToolSeed {
	byAgent := builtinToolsByAgent()
	order := []string{"mainagent", "goals", "planner", "worker", "auto", "pentest"}

	type acc struct {
		tool   actool.CoreTool
		agents []string
	}
	m := map[string]*acc{}
	var keys []string
	for _, ak := range order {
		for _, t := range byAgent[ak] {
			a, ok := m[t.Name()]
			if !ok {
				a = &acc{tool: t}
				m[t.Name()] = a
				keys = append(keys, t.Name())
			}
			a.agents = append(a.agents, ak)
		}
	}

	out := make([]ToolSeed, 0, len(keys))
	for _, k := range keys {
		a := m[k]
		agents := a.agents
		if defaultUnbound[k] {
			agents = []string{} // Cataloged and manually bindable, but unbound by default (stored as [], not null)
		}
		out = append(out, ToolSeed{
			Key:    k,
			Desc:   a.tool.Description(),
			Schema: a.tool.InputSchema(),
			Agents: agents,
		})
	}
	return out
}

// ToolResolve, if set, post-processes an agent's fully-assembled tool list against
// the DB tools table: it drops tools not bound to this agent (or globally disabled)
// and wraps the rest so the model sees the DB-overridden description/schema and
// default arguments get injected. Tools with no matching DB row (MCP/skill/host
// tools like traffic) pass through untouched. nil = tools unchanged. Wired in
// server/assembly.go.
var ToolResolve func(ctx context.Context, agentKey string, tools []actool.CoreTool) []actool.CoreTool

// DecorateTool wraps t so Description()/InputSchema() report the DB overrides and
// Call() injects scalar parameter defaults (from schema's "default" props) whenever
// the model omitted them. Name/Prompt/permission/scheduler flags delegate to t, so
// the tool's identity and handler are unchanged. Empty desc/schema fall back to t's.
func DecorateTool(t actool.CoreTool, desc string, schema map[string]any) actool.CoreTool {
	if desc == "" {
		desc = t.Description()
	}
	if len(schema) == 0 {
		schema = t.InputSchema()
	}
	return &overriddenTool{CoreTool: t, desc: desc, schema: schema}
}

// overriddenTool is a CoreTool decorator: it embeds the original (so all behavioral
// methods — Prompt/IsReadOnly/IsConcurrencySafe/CheckPermissions/Name — delegate)
// and overrides only the model-facing description/schema plus default injection.
type overriddenTool struct {
	actool.CoreTool
	desc   string
	schema map[string]any
}

func (o *overriddenTool) Description() string         { return o.desc }
func (o *overriddenTool) InputSchema() map[string]any { return o.schema }

func (o *overriddenTool) Call(ctx context.Context, in json.RawMessage, tc *actool.ToolContext) (actool.Result, error) {
	return o.CoreTool.Call(ctx, injectDefaults(in, o.schema), tc)
}

// injectDefaults fills scalar parameter defaults declared in the (possibly edited)
// schema into the input JSON whenever the model omitted the field or left it empty/
// null. Structure (names/types/required) is untouched; only defaults are merged in.
func injectDefaults(in json.RawMessage, schema map[string]any) json.RawMessage {
	defs := scalarDefaults(schema)
	if len(defs) == 0 {
		return in
	}
	m := map[string]json.RawMessage{}
	if len(in) > 0 {
		if err := json.Unmarshal(in, &m); err != nil {
			return in // non-object input: don't touch it
		}
	}
	changed := false
	for k, dv := range defs {
		if cur, ok := m[k]; !ok || isEmptyJSON(cur) {
			m[k] = dv
			changed = true
		}
	}
	if !changed {
		return in
	}
	b, err := json.Marshal(m)
	if err != nil {
		return in
	}
	return b
}

// scalarDefaults extracts properties[k]["default"] for scalar params (string/
// integer/number/boolean). Array/object defaults are skipped: merging them is
// ambiguous and not worth the surprise.
func scalarDefaults(schema map[string]any) map[string]json.RawMessage {
	props, _ := schema["properties"].(map[string]any)
	if len(props) == 0 {
		return nil
	}
	out := map[string]json.RawMessage{}
	for name, raw := range props {
		p, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		dv, ok := p["default"]
		if !ok || dv == nil {
			continue
		}
		switch p["type"] {
		case "string", "integer", "number", "boolean":
			if b, err := json.Marshal(dv); err == nil {
				out[name] = b
			}
		}
	}
	return out
}

func isEmptyJSON(raw json.RawMessage) bool {
	s := string(raw)
	return s == "null" || s == `""`
}

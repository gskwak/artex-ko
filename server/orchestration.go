package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
)

// jsonResult marshals v to a JSON tool result.
func jsonResult(v any) (actool.Result, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return actool.Errorf(err.Error()), nil
	}
	return actool.Text(string(b)), nil
}

// This file implements the P2 cross-task orchestration tools. These host tools
// live in the server layer because they need the Manager, Engine, and task
// creation flow. Read tools call the existing per-task tool against the target
// task's store through a temporary ToolSet. Control tools call Manager or Engine
// directly. They are seeded into the tools table and bound per agent.

// hostTools is the runtime host-tool provider fed to ToolAugment: traffic tools
// (gated by capture) + cross-task orchestration tools + user-defined custom tools.
// The second return is the names of custom tools flagged `deferred` (schema
// withheld, routed via SearchExtraTools/ExecuteExtraTool). Per-agent binding still
// decides who actually sees any of them.
//
//nolint:unused // used as the hostTools provider in wireAgentAugment
func (s *Server) hostTools() ([]actool.CoreTool, map[string][]string) {
	tools := append(s.m.HostTools(), s.orchestrationTools()...)
	tools = append(tools, s.findingRetestTools()...)
	tools = append(tools, s.platformTools()...) // Platform tools for Auto to manage skills, tools, and MCP.
	custom, err := s.customTools()
	if err != nil {
		log.Printf("[custom-tool] load failed: %v", err)
		return tools, nil
	}
	tools = append(tools, custom...)
	// deferred custom tools → name -> its bound agent keys. ToolAugment turns a
	// name into a deferred entry only for agents it's actually bound to (so we don't
	// advertise a tool the per-agent binding will drop from the callable set).
	deferred := map[string][]string{}
	rows, _ := s.m.pg.ListCustomTools()
	for _, t := range rows {
		if t.Deferred && t.Enabled {
			deferred[t.Key] = t.Agents
		}
	}
	return tools, deferred
}

// orchestrationTools returns the cross-task tool set. Bound per-agent via the
// tools table (default: no binding — opt-in for orchestration agents).
func (s *Server) orchestrationTools() []actool.CoreTool {
	return []actool.CoreTool{
		s.toolListTasks(),
		s.toolListLLMProfiles(),
		s.toolSpawnTask(),
		s.toolPauseTask(),
		s.toolGetTaskGraph(),
		s.toolListTaskFindings(),
		s.toolAddHint(),
		s.toolGetWorkerTrace(),
		s.toolListWorkerTraces(),
		s.toolSearchWorkerTraces(),
		s.toolGetTaskNodeDetail(),
		s.toolUpdateFindingReport(),
		s.toolGetFindingTraffic(),
		s.toolBindFindingTraffic(),
	}
}

// --- schema helpers ---

func strParam(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

// parseProfileID reads an LLM profile id from a tool arg that may arrive as a JSON
// number (5) or a numeric string ("5"); returns 0 when absent/unparseable.
func parseProfileID(raw json.RawMessage) int64 {
	if len(raw) == 0 {
		return 0
	}
	var n int64
	if json.Unmarshal(raw, &n) == nil {
		return n
	}
	var str string
	if json.Unmarshal(raw, &str) == nil {
		v, _ := strconv.ParseInt(strings.TrimSpace(str), 10, 64)
		return v
	}
	return 0
}

func objSchema(props map[string]any, required ...string) map[string]any {
	m := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		req := make([]any, len(required))
		for i, r := range required {
			req[i] = r
		}
		m["required"] = req
	}
	return m
}

func roTool(name, desc string, schema map[string]any, run func(context.Context, json.RawMessage) (actool.Result, error)) actool.CoreTool {
	return actool.Build(actool.Spec{
		Name: name, Description: desc, Schema: schema,
		ReadOnly:   func(json.RawMessage) bool { return true },
		Concurrent: func(json.RawMessage) bool { return true },
		Permissions: func(context.Context, json.RawMessage, permission.Context) permission.Decision {
			return permission.Allowed()
		},
		Run: func(ctx context.Context, in json.RawMessage, _ *actool.ToolContext) (actool.Result, error) {
			return run(ctx, in)
		},
	})
}

func wrTool(name, desc string, schema map[string]any, run func(context.Context, json.RawMessage) (actool.Result, error)) actool.CoreTool {
	return actool.Build(actool.Spec{
		Name: name, Description: desc, Schema: schema,
		Permissions: func(context.Context, json.RawMessage, permission.Context) permission.Decision {
			return permission.Allowed()
		},
		Run: func(ctx context.Context, in json.RawMessage, _ *actool.ToolContext) (actool.Result, error) {
			return run(ctx, in)
		},
	})
}

// delegateToTask resolves the `task_id` in the input, builds a ToolSet bound to
// that task's store, strips task_id, and calls the chosen per-task tool — so the
// cross-task read reuses the exact in-task logic against another task.
func (s *Server) delegateToTask(ctx context.Context, in json.RawMessage, pick func(*agent.ToolSet) actool.CoreTool) (actool.Result, error) {
	var head struct {
		TaskID string `json:"task_id"`
	}
	_ = json.Unmarshal(in, &head)
	if strings.TrimSpace(head.TaskID) == "" {
		return actool.Errorf("task_id is required"), nil
	}
	t, ok := s.m.Task(head.TaskID)
	if !ok {
		return actool.Errorf("task not found: " + head.TaskID), nil
	}
	var m map[string]json.RawMessage
	_ = json.Unmarshal(in, &m)
	delete(m, "task_id")
	inner, _ := json.Marshal(m)
	tsx := agent.NewToolSet(t.Store, "orchestrator")
	if s.m.Assets() != nil {
		tsx.SetAssetStore(s.m.Assets(), s.m.Assets().Companies())
	}
	tsx.SetNotify(t.Notify)         // General wake-up for writes without a dedicated callback; no-op for reads.
	tsx.SetNotifyHint(t.NotifyHint) // add_hint records a human-added-hints trigger and wakes the planner.
	return pick(tsx).Call(ctx, inner, nil)
}

// --- tools ---

func (s *Server) toolListTasks() actool.CoreTool {
	return roTool("list_tasks",
		"List all tasks with their ID, description, goal, status, run duration, parent task, and LLM profile. Use this to monitor tasks, identify stalled work, and see each task's LLM. For running tasks, run_seconds is the time since creation; for terminal tasks, it is the time from creation to last activity. llm_profile is the profile used by the task's planner and worker; (active profile) follows the globally active profile.",
		objSchema(map[string]any{}),
		func(context.Context, json.RawMessage) (actool.Result, error) {
			lastAct, _ := s.m.PG().LastActivityAll()
			// id -> name to resolve each task's pinned LLM profile.
			profName := map[int64]string{}
			if profs, err := s.m.pg.ListProfiles(); err == nil {
				for _, p := range profs {
					profName[p.ID] = p.Name
				}
			}
			out := make([]map[string]any, 0)
			for _, t := range s.m.List() {
				status := s.deriveTaskStatus(t)
				end := lastAct[t.ExpID]
				if live := s.engine.LastActivity(t.ID); live > end {
					end = live
				}
				dur := int64(0)
				if status == "running" {
					dur = time.Now().Unix() - t.CreatedAt
				} else if end > t.CreatedAt {
					dur = end - t.CreatedAt
				}
				row := map[string]any{"id": t.ID, "description": t.Description, "goal": t.Goal, "status": status, "run_seconds": dur}
				if t.ParentRef != "" {
					row["parent_ref"] = t.ParentRef
				}
				llmState := t.llmStateSnapshot()
				if llmState.ProfileID == nil {
					row["llm_profile"] = "(active profile)"
				} else if n, ok := profName[*llmState.ProfileID]; ok {
					row["llm_profile"] = n
				} else {
					row["llm_profile"] = fmt.Sprintf("#%d (deleted)", *llmState.ProfileID)
				}
				out = append(out, row)
			}
			return jsonResult(out)
		})
}

// toolListLLMProfiles lists the available LLM profiles (name/model/active) so an
// orchestration agent can pick one for spawn_task's llm_profile. Never leaks keys.
func (s *Server) toolListLLMProfiles() actool.CoreTool {
	return roTool("list_llm_profiles",
		"List available LLM profiles with their ID, name, model, format, and active status. Pass an ID as spawn_task's llm_profile_id to give a child task its own LLM, such as a less expensive model for reconnaissance and a stronger model for exploitation. API keys are omitted.",
		objSchema(map[string]any{}),
		func(context.Context, json.RawMessage) (actool.Result, error) {
			profs, err := s.m.pg.ListProfiles()
			if err != nil {
				return actool.Errorf(err.Error()), nil
			}
			out := make([]map[string]any, 0, len(profs))
			for _, p := range profs {
				out = append(out, map[string]any{
					"id": p.ID, "name": p.Name, "model": p.Model, "format": p.Format, "is_active": p.IsDefault,
				})
			}
			return jsonResult(map[string]any{"profiles": out})
		})
}

func (s *Server) toolSpawnTask() actool.CoreTool {
	return wrTool("spawn_task",
		"Create a child task and start its exploration engine. Returns task_id. Use this to delegate a problem or goal as an independent task. Optionally set parent_ref to the parent task ID for a parent-child link.",
		objSchema(map[string]any{
			"description":            strParam("Task description (short title)"),
			"goal":                   strParam("Task goal (what to achieve)"),
			"parent_ref":             strParam("Optional parent task ID for a parent-child link"),
			"source_task_ids":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": fmt.Sprintf("Optional IDs of source tasks to inherit read-only context from (up to %d). The child can use their known assets and findings as a starting point. Unlike parent_ref, this inherits content rather than only recording a parent-child link.", db.MaxTaskSourceCount)},
			"llm_profile_id":         map[string]any{"type": "integer", "description": "Optional LLM profile ID for this child task's planner and worker (see list_llm_profiles). If omitted, inherit the parent's profile, then fall back to the globally active profile."},
			"timeout_seconds":        map[string]any{"type": "integer", "description": "Optional task timeout in seconds. When reached, the task winds down gracefully and enters the timeout state. Omit or set to 0 for no time limit."},
			"plan_heartbeat_seconds": map[string]any{"type": "integer", "description": "Optional planner heartbeat interval in seconds. If this much time passes after the last planning round or task start without a trigger, start a planning round to recover from stalls and check active workers. Omit or set to 0 for the 600-second (10-minute) default."},
			"seed_first_intent":      map[string]any{"type": "boolean", "description": "Optional for simple tasks: immediately issue a seed intent containing the description and goal, so the worker can begin without waiting for the first planning round. Defaults to false, which plans before execution."},
		}, "description", "goal"),
		func(_ context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				Description          string          `json:"description"`
				Goal                 string          `json:"goal"`
				ParentRef            string          `json:"parent_ref"`
				SourceTaskIDs        []string        `json:"source_task_ids"`
				LLMProfileID         json.RawMessage `json:"llm_profile_id"`
				TimeoutSeconds       int             `json:"timeout_seconds"`
				PlanHeartbeatSeconds int             `json:"plan_heartbeat_seconds"`
				SeedFirstIntent      bool            `json:"seed_first_intent"`
			}
			_ = json.Unmarshal(in, &a)
			if strings.TrimSpace(a.Description) == "" {
				a.Description = "Untitled task"
			}
			if strings.TrimSpace(a.Goal) == "" {
				return actool.Errorf("goal is required"), nil
			}
			if a.TimeoutSeconds < 0 {
				a.TimeoutSeconds = 0
			}
			// Validate read-only source tasks as the HTTP creation path does: count, ID, uniqueness, and existence.
			if len(a.SourceTaskIDs) > db.MaxTaskSourceCount {
				return actool.Errorf(fmt.Sprintf("select at most %d source tasks", db.MaxTaskSourceCount)), nil
			}
			sourceIDs := make([]int64, 0, len(a.SourceTaskIDs))
			seenSources := map[int64]bool{}
			for _, raw := range a.SourceTaskIDs {
				id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
				if err != nil || id <= 0 || seenSources[id] {
					return actool.Errorf("source task ID is invalid or duplicated"), nil
				}
				if _, ok := s.m.Task(strconv.FormatInt(id, 10)); !ok {
					return actool.Errorf(fmt.Sprintf("source task #%d not found", id)), nil
				}
				seenSources[id] = true
				sourceIDs = append(sourceIDs, id)
			}
			// LLM profile resolution: explicit id > inherit parent's pin > active(nil).
			var pin *int64
			if id := parseProfileID(a.LLMProfileID); id > 0 {
				if _, ok := s.loadProfileConfig(id); !ok {
					return actool.Errorf(fmt.Sprintf("LLM profile #%d not found or has no API key", id)), nil
				}
				pin = &id
			} else if a.ParentRef != "" {
				if pt, ok := s.m.Task(a.ParentRef); ok {
					pin = pt.LLMProfileID
				}
			}
			var llmIDs []int64
			if pin != nil {
				llmIDs = []int64{*pin}
			}
			t, err := s.m.CreateTaskWithOptions(a.Description, a.Goal, db.TaskCreateOptions{
				SourceTaskIDs:        sourceIDs,
				LLMProfileIDs:        llmIDs,
				TimeoutSeconds:       a.TimeoutSeconds,
				PlanHeartbeatSeconds: a.PlanHeartbeatSeconds,
			})
			if err != nil {
				return actool.Errorf(err.Error()), nil
			}
			if a.ParentRef != "" {
				t.ParentRef = a.ParentRef
				if id, e := strconv.ParseInt(t.ID, 10, 64); e == nil {
					_ = s.m.PG().SetParentRef(id, a.ParentRef)
				}
			}
			// Share launchTask with HTTP task creation: seed, run visible goal
			// decomposition in the background, then start the engine. A seed intent
			// is optional; by default the planner runs before the worker.
			s.launchTask(t, a.Description+" "+a.Goal, a.SeedFirstIntent)
			return actool.Text(fmt.Sprintf("task created: %s", t.ID)), nil
		})
}

func (s *Server) toolPauseTask() actool.CoreTool {
	return wrTool("pause_task", "Pause a task and stop its planner and worker loops.",
		objSchema(map[string]any{"task_id": strParam("ID of the task to pause")}, "task_id"),
		func(_ context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				TaskID string `json:"task_id"`
			}
			_ = json.Unmarshal(in, &a)
			t, ok := s.m.Task(a.TaskID)
			if !ok {
				return actool.Errorf("task not found: " + a.TaskID), nil
			}
			if _, err := s.applyTaskControlWithCause(t, "pause", agent.AbortPausedByOrchestrator); err != nil {
				return actool.Errorf(err.Error()), nil
			}
			return actool.Text("task paused: " + a.TaskID), nil
		})
}

func (s *Server) toolGetTaskGraph() actool.CoreTool {
	return roTool("get_task_graph", "Read a task's exploration graph overview, including asset counts, frontier, findings, and coverage, as in graph_overview. Specify the task with task_id.",
		objSchema(map[string]any{"task_id": strParam("Task ID")}, "task_id"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			return s.delegateToTask(ctx, in, (*agent.ToolSet).GraphOverviewTool)
		})
}

func (s *Server) toolListTaskFindings() actool.CoreTool {
	return roTool("list_task_findings", "List confirmed findings for a task, including flags and PoCs. Each finding includes id, task_id, intent_id, vulnclass, severity, summary, and status. Specify the task with task_id.",
		objSchema(map[string]any{"task_id": strParam("Task ID")}, "task_id"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			return s.delegateToTask(ctx, in, (*agent.ToolSet).ListFindingsTool)
		})
}

func (s *Server) toolAddHint() actool.CoreTool {
	return wrTool("add_task_hint", "Add strategic hints to a task; its planner reads them in the next intent-generation round.\n"+
		"Prefer batching: submit multiple hints in the hints array (the returned ids array has the same length and order, with id=0 for failed entries). For one hint, omit hints and provide the top-level text field.",
		objSchema(map[string]any{
			"task_id":      strParam("Task ID"),
			"hints":        map[string]any{"type": "array", "description": "Preferred for multiple hints. Each item has the same fields as the top level: text, asset_ids, and traffic_refs.", "items": objSchema(map[string]any{"text": strParam("Hint text"), "asset_ids": map[string]any{"type": "array", "items": map[string]any{"type": "integer"}}, "traffic_refs": agent.HintTrafficSchema()})},
			"text":         strParam("Single hint text"),
			"traffic_refs": agent.HintTrafficSchema(),
			"asset_ids":    map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "description": "Optional IDs of assets in this task to anchor the hint to (zero, one, or several)."},
		}, "task_id"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			return s.delegateToTask(ctx, in, (*agent.ToolSet).AddHintTool)
		})
}

func (s *Server) toolGetWorkerTrace() actool.CoreTool {
	return roTool("get_task_worker_trace",
		"Read a work intent's execution trace in a task. Call get_task_worker_trace(task_id, intent_id) for step summaries, then pass step_ids=[...] to retrieve full content for up to five steps; extra IDs are ignored.",
		objSchema(map[string]any{
			"task_id":   strParam("Task ID"),
			"intent_id": map[string]any{"type": "integer", "description": "Intent ID for the work within this task"},
			"step_ids":  map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "description": "Optional step IDs whose full content to retrieve (up to five per call). Additional IDs are listed in omitted_step_ids."},
		}, "task_id", "intent_id"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			return s.delegateToTask(ctx, in, (*agent.ToolSet).GetWorkerTraceTool)
		})
}

func (s *Server) toolListWorkerTraces() actool.CoreTool {
	return roTool("list_task_worker_traces", "List work intents run in a task and the number of steps in each. Use this to choose traces to inspect with get_task_worker_trace.",
		objSchema(map[string]any{"task_id": strParam("Task ID")}, "task_id"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			return s.delegateToTask(ctx, in, (*agent.ToolSet).ListWorkerTracesTool)
		})
}

func (s *Server) toolSearchWorkerTraces() actool.CoreTool {
	return roTool("search_task_worker_traces", "Search all work execution traces in a task by keyword. Returns matching step summaries and intent_id values.",
		objSchema(map[string]any{"task_id": strParam("Task ID"), "q": strParam("Search keyword")}, "task_id", "q"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			return s.delegateToTask(ctx, in, (*agent.ToolSet).SearchWorkerTracesTool)
		})
}

func (s *Server) toolGetTaskNodeDetail() actool.CoreTool {
	return roTool("get_task_node_detail",
		"Read a task's exploration graph node in full, whether a finding, fact, intent, or goal, including its summary, details, evidence, and PoC. The id is the exploration node ID returned by report_finding or listed by list_task_findings. Use it to gather full evidence before writing a finding report.",
		objSchema(map[string]any{
			"task_id": strParam("Task ID"),
			"id":      map[string]any{"type": "integer", "description": "Exploration graph node ID (not an asset ID)"},
		}, "task_id", "id"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			return s.delegateToTask(ctx, in, (*agent.ToolSet).NodeDetailTool)
		})
}

// toolUpdateFindingReport writes/overwrites a finding's detailed Markdown report.
// finding_id is the id report_finding returned ("finding recorded: <id>", the
// finding node id). The write (SetFindingReportByNodeID) is keyed by node_id and
// task-agnostic, so this host tool needs no task_id / exploration store.
func (s *Server) toolUpdateFindingReport() actool.CoreTool {
	return wrTool("update_finding_report",
		"Write or replace a detailed Markdown report for a recorded finding. Pass the ID returned by report_finding as finding_id (the number in \"finding recorded: <id>\"). Include an overview, impact, reproduction steps, evidence or PoC, and remediation guidance.",
		objSchema(map[string]any{
			"finding_id":       map[string]any{"type": "integer", "description": "Target finding ID returned by report_finding"},
			"report":           strParam("Full detailed report in Markdown"),
			"evidence_version": map[string]any{"type": "integer", "description": "Evidence version returned by get_finding_traffic; prevents a report from overwriting newer evidence changes"},
		}, "finding_id", "report"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				EvidenceVersion *int64          `json:"evidence_version"`
				FindingID       json.RawMessage `json:"finding_id"`
				Report          string          `json:"report"`
			}
			_ = json.Unmarshal(in, &a)
			nodeID := parseProfileID(a.FindingID) // Reuse numeric or numeric-string ID parsing.
			if nodeID <= 0 {
				return actool.Errorf("invalid finding_id"), nil
			}
			n, err := s.m.pg.SetFindingReportVersionByNodeID(ctx, nodeID, a.Report, a.EvidenceVersion)
			if err != nil {
				return actool.Errorf(err.Error()), nil
			}
			if n == 0 {
				return actool.Errorf(fmt.Sprintf("no finding found for finding_id=%d (record it with report_finding first)", nodeID)), nil
			}
			return actool.Text(fmt.Sprintf("finding %d report updated (%d chars)", nodeID, len(a.Report))), nil
		})
}

// deriveTaskStatus mirrors listTasks' status derivation for the list_tasks tool.
func (s *Server) deriveTaskStatus(t *Task) string {
	lifecycle := t.lifecycleSnapshot()
	switch {
	case isTerminalStatus(lifecycle.Status):
		return lifecycle.Status
	case lifecycle.Paused || s.engine.IsPaused(t.ID):
		return "paused"
	case s.engine.ReadyFor(t) && s.engine.Started(t.ID):
		return "running"
	}
	return "created"
}

// orchestrationToolSeeds seeds the cross-task tools into the tools table so they
// are bindable per-agent (default: bound to nobody — opt-in for orchestration
// agents). First-insert only, like the traffic seeds.
func (s *Server) seedOrchestrationTools() {
	// Bind task and platform tools to the built-in Auto agent by default.
	// SeedTool only inserts new rows; seedAutoDefaultBindings backfills old rows.
	autoAgents, _ := json.Marshal([]string{"auto"})
	for _, t := range s.orchestrationTools() {
		schema, _ := json.Marshal(t.InputSchema())
		bindings := autoAgents
		if t.Name() == "bind_finding_traffic" {
			bindings = json.RawMessage(`["reporter"]`)
		}
		_ = s.m.PG().SeedTool(t.Name(), t.Description(), schema, bindings)
	}
	for _, t := range s.platformTools() {
		schema, _ := json.Marshal(t.InputSchema())
		_ = s.m.PG().SeedTool(t.Name(), t.Description(), schema, autoAgents)
	}
	s.refreshBuiltinToolSchemas()
	s.seedAutoDefaultBindings()
	s.seedPlannerDefaultBindings()
	s.seedPlannerListAssetsBinding()
	s.seedCompanyScopeRebind()
	s.seedWorkerReadToolsUnbind() // Remove the default worker binding from three planning read tools (one time).
	s.seedWorkerReadbackRebind()  // Restore worker trace/detail bindings removed by an older migration (one time).
	s.seedAutoReportFindingBinding()
	s.unbindGoalMetDefault()
	s.reseedGoalsPrompt()             // Append the newer goals prompt to old databases (one time).
	s.reseedMainAgentPrompt()         // Append the newer mainagent prompt (one time).
	s.reseedPlannerPrompt()           // Append the newer planner prompt (one time).
	s.reseedWorkerPrompt()            // Append the newer worker prompt (one time).
	s.seedReporterAgent()             // Seed the report-writing agent, bindings, and finding trigger (one time).
	defaults := agent.BuiltinPromptSeeds()
	defaults["reporter"] = agent.ReporterDefaultPrompt
	defaults["_assistant"] = agent.DefaultAssistantPrompt
	if err := s.m.pg.MigrateLegacyStockPrompts(defaults); err != nil {
		log.Printf("[prompts] failed to migrate legacy stock prompts: %v", err)
	}
	s.upgradeReporterTriggerMessage() // Upgrade unchanged reporter trigger defaults in old databases (one time).
	s.seedFindingTrafficTools()       // Add evidence parameters and read tools without replacing user configuration.
	s.seedFindingWorkflowTools()
	// Pentest bindings need no migration: BuiltinToolSeeds already binds
	// list_assets, insert_assets, report_finding, list_findings, and
	// list_companies for fresh databases.
}

// refreshBuiltinToolSchemas propagates code schema/description changes on the
// orchestration + platform tools into already-seeded rows ONCE per version flag —
// SeedTool is first-insert-only, so a new parameter (for example, spawn_task's llm_profile) never
// reaches an old DB otherwise. Preserves each tool's agent binding + enabled flag.
// Keep the v7 flag for its structural migration. Text-only translations are
// handled by SeedTool's exact-match migration so user-edited help stays intact.
func (s *Server) refreshBuiltinToolSchemas() {
	const flag = "tool_schema_refresh_v7_list_facts_paging"
	if v, _, _ := s.m.pg.GetSetting(flag); v == "true" {
		return
	}
	tools := append(s.orchestrationTools(), s.platformTools()...)
	for _, t := range tools {
		schema, _ := json.Marshal(t.InputSchema())
		if err := s.m.pg.RefreshToolDefaults(t.Name(), t.Description(), schema); err != nil {
			log.Printf("[tools] refresh %s schema failed: %v", t.Name(), err)
		}
	}
	// Also refresh these built-in agent tools to their code defaults:
	//   - goal_met: the old description implied it merely ended a planning round,
	//     which could make a planner mark an entire task done immediately.
	//   - insert_assets: add the related parameter for task coverage.
	//   - list_facts: add limit, before, and q for pagination and search.
	refreshBuiltin := map[string]bool{"goal_met": true, "insert_assets": true, "list_facts": true}
	for _, sd := range agent.BuiltinToolSeeds() {
		if !refreshBuiltin[sd.Key] {
			continue
		}
		schema, _ := json.Marshal(sd.Schema)
		if err := s.m.pg.RefreshToolDefaults(sd.Key, sd.Desc, schema); err != nil {
			log.Printf("[tools] refresh %s desc failed: %v", sd.Key, err)
		}
	}
	_ = s.m.pg.SetSetting(flag, "true")
	log.Print(logT("[tools] orchestration/platform 도구 스키마를 코드 기본값으로 새로고침(1회성)"))
}

// unbindGoalMetDefault removes goal_met's default "planner" binding ONCE (guarded by
// a settings flag), so existing DBs match the new default of NO agent. goal_met bypasses
// per-goal prove_goal to declare the whole task done — powerful/risky and redundant with
// the prove_goal→auto-complete path — so it ships unbound; users can re-bind it per agent
// in the UI. A user's own binding to another agent is untouched (we only strip planner).
func (s *Server) unbindGoalMetDefault() {
	const flag = "goal_met_unbind_default_v1"
	if v, _, _ := s.m.pg.GetSetting(flag); v == "true" {
		return
	}
	if err := s.m.pg.RemoveAgentFromTool("planner", "goal_met"); err != nil {
		log.Printf("[tools] failed to unbind goal_met from planner: %v", err)
		return
	}
	_ = s.m.pg.SetSetting(flag, "true")
}

// reseedGoalsPrompt appends the current default goals prompt to existing
// databases. The default added a set_constraints step before goal decomposition;
// SeedPromptIfEmpty cannot update an existing prompt. ResetPromptToDefault adds a
// new version and activates it, leaving earlier versions recoverable. A setting
// limits the migration to one run. Fresh databases already have the new default.
func (s *Server) reseedGoalsPrompt() {
	const flag = "goals_prompt_constraint_step_v1"
	if v, _, _ := s.m.pg.GetSetting(flag); v == "true" {
		return
	}
	defer func() { _ = s.m.pg.SetSetting(flag, "true") }() // Attempt once, even if the update fails.
	a, err := s.m.pg.GetAgentByKey("goals")
	if err != nil || a == nil {
		return // seedPrompts will install the current default in a fresh database.
	}
	tmpl := agent.BuiltinPromptSeeds()["goals"]
	if tmpl == "" {
		return
	}
	// Avoid appending a duplicate version when the current prompt is already the default.
	if cur, err := s.m.pg.CurrentPrompt(a.ID); err == nil && cur == tmpl {
		return
	}
	if _, err := s.m.pg.ResetPromptToDefault(a.ID, tmpl); err != nil {
		log.Printf("[prompts] failed to refresh goals prompt to the new default: %v", err)
		return
	}
	log.Printf("[prompts] appended new default goals prompt version with constraint-extraction step (one time)")
}

// reseedMainAgentPrompt appends the current default mainagent prompt to old
// databases. The default now asks whether directly added intents should become
// formal goals after all goals have been met. SeedPromptIfEmpty only inserts new
// prompts, so ResetPromptToDefault adds a recoverable version. A setting makes
// this a one-time migration; fresh databases already have the current default.
func (s *Server) reseedMainAgentPrompt() {
	const flag = "mainagent_prompt_goalless_intent_v1"
	if v, _, _ := s.m.pg.GetSetting(flag); v == "true" {
		return
	}
	defer func() { _ = s.m.pg.SetSetting(flag, "true") }() // Attempt once, even if the update fails.
	a, err := s.m.pg.GetAgentByKey("mainagent")
	if err != nil || a == nil {
		return // seedPrompts will install the current default in a fresh database.
	}
	tmpl := agent.BuiltinPromptSeeds()["mainagent"]
	if tmpl == "" {
		return
	}
	// Avoid appending a duplicate version when the current prompt is already the default.
	if cur, err := s.m.pg.CurrentPrompt(a.ID); err == nil && cur == tmpl {
		return
	}
	if _, err := s.m.pg.ResetPromptToDefault(a.ID, tmpl); err != nil {
		log.Printf("[prompts] failed to refresh mainagent prompt to the new default: %v", err)
		return
	}
	log.Printf("[prompts] appended new default mainagent prompt version with goal-registration follow-up (one time)")
}

// reseedPlannerPrompt appends the current default planner prompt to old
// databases. It has concise guidance, prioritizes depth, requires new intents
// when goals remain unmet and no intent is running, and bounds review of
// negative conclusions. Bump the setting flag on substantive future changes.
// ResetPromptToDefault preserves earlier versions; fresh databases already
// have the current default.
func (s *Server) reseedPlannerPrompt() {
	const flag = "planner_prompt_compact_realistic_v2"
	if v, _, _ := s.m.pg.GetSetting(flag); v == "true" {
		return
	}
	defer func() { _ = s.m.pg.SetSetting(flag, "true") }() // Attempt once, even if the update fails.
	a, err := s.m.pg.GetAgentByKey("planner")
	if err != nil || a == nil {
		return // seedPrompts will install the current default in a fresh database.
	}
	tmpl := agent.BuiltinPromptSeeds()["planner"]
	if tmpl == "" {
		return
	}
	// Avoid appending a duplicate version when the current prompt is already the default.
	if cur, err := s.m.pg.CurrentPrompt(a.ID); err == nil && cur == tmpl {
		return
	}
	if _, err := s.m.pg.ResetPromptToDefault(a.ID, tmpl); err != nil {
		log.Printf("[prompts] failed to refresh planner prompt to the new default: %v", err)
		return
	}
	log.Printf("[prompts] appended new default planner prompt version with compact guidance, depth priority, and bounded negative review (one time)")
}

// reseedWorkerPrompt appends the current default worker prompt to old
// databases. It removes misleading guidance for negative facts and narrows
// multi-fact entries to independent observations that cannot be merged.
// SeedPromptIfEmpty cannot update existing prompts, so this adds a recoverable
// version once. Fresh databases already have the current default.
func (s *Server) reseedWorkerPrompt() {
	const flag = "worker_prompt_compact_v4"
	if v, _, _ := s.m.pg.GetSetting(flag); v == "true" {
		return
	}
	defer func() { _ = s.m.pg.SetSetting(flag, "true") }() // Attempt once, even if the update fails.
	a, err := s.m.pg.GetAgentByKey("worker")
	if err != nil || a == nil {
		return // seedPrompts will install the current default in a fresh database.
	}
	tmpl := agent.BuiltinPromptSeeds()["worker"]
	if tmpl == "" {
		return
	}
	// Avoid appending a duplicate version when the current prompt is already the default.
	if cur, err := s.m.pg.CurrentPrompt(a.ID); err == nil && cur == tmpl {
		return
	}
	if _, err := s.m.pg.ResetPromptToDefault(a.ID, tmpl); err != nil {
		log.Printf("[prompts] failed to refresh worker prompt to the new default: %v", err)
		return
	}
	log.Printf("[prompts] appended new default worker prompt version with focused context lookup (one time)")
}

// reporterToolCallMessage must always instruct the reporter to read
// get_finding_traffic before writing the report. This read-only tool works
// regardless of capture or automatic binding. Without its evidence_version,
// SetFindingReportVersionByNodeID writes the legacy -1 value, which leaves
// the report permanently marked as needing an evidence update.
const reporterToolCallMessage = "A finding was just recorded by report_finding. Read finding_id (the separate finding record ID) and finding_node_id (the exploration node ID) from the returned JSON. " +
	"First call get_finding_traffic(finding_id) to read the current evidence list and its version; an empty list is normal, so still write the report. " +
	"If the runtime instructions enable automatic binding, verify and link this finding's traffic before reading it. Use finding_node_id for node details. " +
	"Finally call update_finding_report(finding_id=finding_node_id, report, evidence_version=the version you read) to save the report. " +
	"You must pass evidence_version, or the report will permanently be marked as needing an update. Do not confuse the two IDs."

// Hashes of two historical stock trigger messages. Exact matching updates only
// untouched defaults and keeps user-edited trigger messages intact.
const reporterToolCallMessageV1Hash = "c10b22c2daf51a0e4998fe0e84a2e22baf70927a6f7368874e1e1e0b0eacb02b"
const reporterToolCallMessageV2Hash = "2b766affdba89c92772698367e41fc6d570ee25f29964576c0aec96da4d176e6"

func isHistoricalReporterToolCallMessage(message string) bool {
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(message)))
	return hash == reporterToolCallMessageV1Hash || hash == reporterToolCallMessageV2Hash
}

// upgradeReporterTriggerMessage updates reporter triggers in old databases only
// when their text still matches a historical default. seedReporterAgent writes
// a trigger only when creating the agent, so existing agents need this migration.
// User-edited messages remain untouched.
func (s *Server) upgradeReporterTriggerMessage() {
	const flag = "reporter_trigger_english_default_v2"
	if v, _, _ := s.m.pg.GetSetting(flag); v == "true" {
		return
	}
	triggers, err := s.m.pg.ListTriggersFor("reporter")
	if err != nil {
		log.Printf("[reporter] failed to read triggers: %v", err)
		return
	}
	for _, t := range triggers {
		if !t.OnToolCall || !isHistoricalReporterToolCallMessage(t.ToolCallMessage) {
			continue // Leave other triggers and user-edited messages untouched.
		}
		t.ToolCallMessage = reporterToolCallMessage
		if err := s.m.pg.UpdateTrigger(t); err != nil {
			log.Printf("[reporter] failed to upgrade trigger message: %v", err)
			return
		}
		log.Printf("[reporter] upgraded trigger message to read and return evidence_version")
	}
	if err := s.m.pg.SetSetting(flag, "true"); err != nil {
		log.Printf("[reporter] failed to record trigger migration: %v", err)
	}
}

// reporterAgentName·reporterAgentDescription 은 시드되는 reporter 에이전트의 표시 전용
// 라벨이다(한국어화). 에이전트의 두뇌(agent.ReporterDefaultPrompt)와 트리거 주입
// 메시지(reporterToolCallMessage)는 모델 입력이며 영어로 작성된다. 이 이름·설명은
// `agentDTO` 로 system/agents UI 에만 렌더되고(server_mgmt.go) 어떤 프롬프트·에이전트 선택에도
// 들어가지 않는 순수 UI 라벨이라 한국어화한다(BRIEF 사용자 노출 산출물 한국어화 범위). 트리거는
// report_finding 도구 호출 기반(OnToolCall·결정적)이라 reporter 는 key "reporter" 로 참조될 뿐
// 표시 이름으로 선택되지 않는다. 회귀 방어: reporter_seed_localized_test.go. [[F35]]
const reporterAgentName = "보고서 작성"
const reporterAgentDescription = "취약점 상세 보고서 작성: 취약점을 발견하면 자동으로 트리거되어, 증거와 실행 과정을 조회한 뒤 Markdown 보고서를 작성해 해당 취약점에 기록합니다."

// seedReporterAgent creates the editable "report writer" custom agent and
// binds report and task-query tools. Its trigger runs when report_finding is
// called, producing one detailed report per finding. The setting makes seeding
// one-time, so deleting the agent does not recreate it. Orchestration tools
// must already be seeded for their bindings to resolve.
func (s *Server) seedReporterAgent() {
	const flag = "reporter_agent_seed_v1"
	if v, _, _ := s.m.pg.GetSetting(flag); v == "true" {
		return
	}
	defer func() { _ = s.m.pg.SetSetting(flag, "true") }() // Attempt once, even if creation fails.

	if exist, _ := s.m.pg.GetAgentByKey("reporter"); exist != nil {
		return // Preserve an agent already using this key.
	}
	a, err := s.m.pg.CreateAgent("reporter", reporterAgentName, reporterAgentDescription)
	if err != nil {
		log.Printf("[reporter] failed to create agent: %v", err)
		return
	}
	if err := s.m.pg.SeedPromptIfEmpty(a.ID, agent.ReporterDefaultPrompt); err != nil {
		log.Printf("[reporter] failed to seed prompt: %v", err)
	}
	// Run one report per finding, with separate findings in parallel. Keep
	// merge=none so multiple findings do not collapse into one run, and cap
	// concurrency at five to avoid too many simultaneous LLM calls.
	if err := s.m.pg.SetAgentTriggerBehavior("reporter", "parallel", "none", 5); err != nil {
		log.Printf("[reporter] failed to set trigger run behavior: %v", err)
	}
	// Bind tools for report writing and reading evidence, traces, and task state.
	if err := s.m.pg.AddAgentToToolBinding("reporter", []string{
		"update_finding_report", "get_task_node_detail", "list_task_findings",
		"get_task_worker_trace", "list_task_worker_traces", "search_task_worker_traces",
		"get_task_graph",
	}); err != nil {
		log.Printf("[reporter] failed to bind tools: %v", err)
	}
	// Trigger on report_finding calls; the tool returns "finding recorded: <id>"
	// and the trigger message also includes the task ID.
	if _, err := s.m.pg.CreateTrigger(&db.AgentTrigger{
		AgentKey:        "reporter",
		Enabled:         true,
		OnToolCall:      true,
		ToolNames:       []string{"report_finding"},
		ToolCallMessage: reporterToolCallMessage,
	}); err != nil {
		log.Printf("[reporter] failed to create trigger: %v", err)
	}
	log.Printf(logT("[reporter] 「%s」 agent + finding 트리거 사전 구성"), reporterAgentName)
}

// seedAutoReportFindingBinding adds "auto" to report_finding's binding ONCE so
// conversation-context agents can call it without requiring an intent_id.
func (s *Server) seedAutoReportFindingBinding() {
	const flag = "auto_report_finding_v1"
	if v, _, _ := s.m.pg.GetSetting(flag); v == "true" {
		return
	}
	if err := s.m.pg.AddAgentToToolBinding("auto", []string{"report_finding"}); err != nil {
		log.Printf("[auto] failed to add default report_finding binding: %v", err)
		return
	}
	_ = s.m.pg.SetSetting(flag, "true")
}

// seedPlannerDefaultBindings adds "planner" to report_finding's binding ONCE
// (guarded by a settings flag), so existing DBs — whose report_finding row was
// seeded as worker-only — also let the planner record findings. Fresh DBs already
// get it via PlannerTools(); this only backfills without overriding a user unbind.
func (s *Server) seedPlannerDefaultBindings() {
	const flag = "planner_report_finding_v1"
	if v, _, _ := s.m.pg.GetSetting(flag); v == "true" {
		return
	}
	if err := s.m.pg.AddAgentToToolBinding("planner", []string{"report_finding"}); err != nil {
		log.Printf("[planner] failed to add default report_finding binding: %v", err)
		return
	}
	_ = s.m.pg.SetSetting(flag, "true")
}

// seedPlannerListAssetsBinding adds "planner" to list_assets's binding ONCE
// (guarded by a settings flag), so existing DBs — whose list_assets row was seeded
// as auto/pentest-only — also let the planner query the asset store by DSL. Fresh
// DBs already get it via PlannerTools(); this only backfills without overriding a
// user unbind.
func (s *Server) seedPlannerListAssetsBinding() {
	const flag = "planner_list_assets_v1"
	if v, _, _ := s.m.pg.GetSetting(flag); v == "true" {
		return
	}
	if err := s.m.pg.AddAgentToToolBinding("planner", []string{"list_assets"}); err != nil {
		log.Printf("[planner] failed to add default list_assets binding: %v", err)
		return
	}
	_ = s.m.pg.SetSetting(flag, "true")
}

// seedCompanyScopeRebind changes add_company_scope's default binding ONCE on
// existing DBs (guarded by a settings flag): the tool moves off worker and onto
// planner — defining a company's asset scope is a planning/main/auto concern, not
// something a worker does mid-exploration. Fresh DBs already get planner via
// PlannerTools() and lack worker via WorkerTools(); this only backfills old rows.
// One-shot + flag-guarded so a user who later re-binds worker isn't overridden.
func (s *Server) seedCompanyScopeRebind() {
	const flag = "company_scope_rebind_v1" // Move the default binding from worker to planner.
	if v, _, _ := s.m.pg.GetSetting(flag); v == "true" {
		return
	}
	if err := s.m.pg.AddAgentToToolBinding("planner", []string{"add_company_scope"}); err != nil {
		log.Printf("[planner] failed to add default add_company_scope binding: %v", err)
		return
	}
	if err := s.m.pg.RemoveAgentFromTool("worker", "add_company_scope"); err != nil {
		log.Printf("[worker] failed to unbind add_company_scope: %v", err)
		return
	}
	_ = s.m.pg.SetSetting(flag, "true")
}

// seedWorkerReadToolsUnbind strips the read-context tools off worker's default
// binding ONCE on existing DBs (guarded by a settings flag): a worker executes one
// intent and writes back — reading facts/companies and listing all workers' traces is
// a planning/main concern, not the executor's. Fresh DBs already lack these via
// WorkerTools(); this only backfills old rows without overriding a user who
// deliberately re-binds worker. Each RemoveAgentFromTool is per-tool +
// membership-guarded, so planner/mainagent bindings of the same tool are untouched.
//
// NOTE: search_all_worker_traces / get_worker_trace / node_detail are intentionally NOT
// unbound — worker owns them for cross-work look-back + node drill-down (see WorkerTools).
// They used to be in this list back when worker lacked them; seedWorkerReadbackRebind
// repairs DBs whose old run stripped them.
func (s *Server) seedWorkerReadToolsUnbind() {
	const flag = "worker_readtools_unbind_v1"
	if v, _, _ := s.m.pg.GetSetting(flag); v == "true" {
		return
	}
	for _, k := range []string{
		"list_facts", "list_companies", "list_worker_traces",
	} {
		if err := s.m.pg.RemoveAgentFromTool("worker", k); err != nil {
			log.Printf("[worker] failed to unbind %s: %v", k, err)
			return // Leave the flag unset so startup can retry.
		}
	}
	_ = s.m.pg.SetSetting(flag, "true")
}

// seedWorkerReadbackRebind re-binds the cross-work look-back / drill-down tools onto
// worker ONCE (guarded by a settings flag): an earlier seedWorkerReadToolsUnbind wrongly
// stripped search_all_worker_traces / get_worker_trace / node_detail from worker after
// they had been added to WorkerTools(), so any DB that ran that migration lost them.
// Fresh DBs already have them via WorkerTools() and this is a harmless no-op there.
// One-shot + flag-guarded so a user who later deliberately unbinds them isn't overridden.
func (s *Server) seedWorkerReadbackRebind() {
	const flag = "worker_readback_rebind_v2" // v2 adds node_detail.
	if v, _, _ := s.m.pg.GetSetting(flag); v == "true" {
		return
	}
	if err := s.m.pg.AddAgentToToolBinding("worker", []string{
		"search_all_worker_traces", "get_worker_trace", "node_detail",
	}); err != nil {
		log.Printf("[worker] failed to restore trace/detail tool bindings: %v", err)
		return // Leave the flag unset so startup can retry.
	}
	_ = s.m.pg.SetSetting(flag, "true")
}

// seedAutoDefaultBindings adds "auto" to the task-op + platform tools' bindings
// ONCE (guarded by a settings flag), so existing DBs whose tool rows were seeded
// before Auto existed still give Auto its default toolset — without re-adding it
// after a user deliberately unbinds.
func (s *Server) seedAutoDefaultBindings() {
	const flag = "auto_default_bindings_v3" // v3 replaces old asset tool names and adds insert_assets/add_company_scope.
	if v, _, _ := s.m.pg.GetSetting(flag); v == "true" {
		return
	}
	keys := make([]string, 0, len(platformToolKeys)+12)
	for _, t := range s.orchestrationTools() {
		keys = append(keys, t.Name())
	}
	keys = append(keys, platformToolKeys...)
	// Auto frequently reads and records assets and manages company scope.
	keys = append(keys, "insert_assets", "add_company_scope", "list_assets")
	if err := s.m.pg.AddAgentToToolBinding("auto", keys); err != nil {
		log.Printf("[auto] failed to add default tool bindings: %v", err)
		return
	}
	_ = s.m.pg.SetSetting(flag, "true")
}

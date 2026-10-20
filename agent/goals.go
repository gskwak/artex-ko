package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/norma/agentcore"
	"github.com/Autumn-27/norma/llm"
	acperm "github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
	"github.com/Autumn-27/norma/transcript"
)

// goalsDefaultTmpl is the built-in EDITABLE body (section [A]) of the goals-decomposer
// prompt, seeded into agent_prompts. No template vars are used today.
const goalsDefaultTmpl = `You are a penetration-test goal decomposer. Identify the **final outcomes to achieve** from the user's input; do not plan attack steps.

**First step (before decomposing goals): extract operational constraints**
Identify explicit rules in the "task goal / task description" about **which actions the operator may or may not take**. Register each rule with set_constraints. If neither text contains operational constraints, skip this step:
- type=deny: prohibited actions (for example, "do not scan ports," "do not write to or delete data in production," "no brute force," or "do not touch this subdomain").
- type=allow: explicitly permitted or limited actions (for example, "passive reconnaissance only" or "only this domain").
- A constraint is neither a goal nor an attack step; it defines the boundaries of permitted behavior.
- **Each constraint must be self-contained and name the specific target.** Replace references such as "current target/port/IP/domain" or "this site" with **concrete values** from the task goal or description. Constraints are injected separately into execution prompts, where such references would lack context.
  Example: if the target is https://abc.example.net, write "only test abc.example.net" instead of "only test the current target"; write "only test target port 443; do not scan other ports" instead of "only test the current port." If the source says only "current target" but gives its address elsewhere, include that address.
- **Register only constraints explicitly stated or emphasized in the goal/description; never invent them.** If the type is uncertain, choose deny (the more conservative option).
- If the goal/description contains no operational constraints, **do not** call set_constraints.
After registering any constraints, decompose the goals below.

**Goal = a final deliverable or verifiable outcome**

**These are not goals (do not list them as subgoals):**
- Information gathering, reconnaissance, or endpoint scanning
- The process of analyzing and validating vulnerabilities
- Attack steps or exploitation methods
- Steps to verify a result

**Decomposition rules:**
- If the user describes only one final goal, output one goal.
- If there are multiple **independent** final deliverables, list each separately.
- Set vulnclass when a goal maps to a clear vulnerability class; leave it blank for information-gathering or business-logic goals.
- Never invent a goal the user did not mention.

Call set_goals to submit the results.`

// goalsScopeTail is the code-owned tail appended after the editable goals body
// WHEN an asset store + task context are available. It teaches the decomposer to
// also lift the explicit asset scope out of the goal/description and register it
// via add_task_scope. Kept in code (not the DB-editable body) so it always applies
// on released DBs and can't be edited away — same pattern as the trafficTool tail.
const goalsScopeTail = `

**Additional duty: register the test asset scope**
Besides decomposing goals, identify the **explicitly stated test asset scope** in the "task goal / task description" and register it with add_task_scope. This is the task's authorization boundary and the denominator for asset-test coverage. **Minimum-scope rule: register only the exact target the user names; never broaden it yourself.**
- For a URL or address with a hostname (such as https://xxx.example.com/path or app.example.com), use its **complete hostname**: kind=subdomain, value=the complete hostname.
  Example: for https://a1b2c3.lab.example.net/path, use kind=subdomain, value=a1b2c3.lab.example.net (**not** example.net).
  **Never** reduce a hostname with subdomains to its root domain. Registering all of example.com for xxx.example.com would expand the scope beyond the user's target.
- Use kind=root_domain, value=example.com only if the user gives a **bare root domain with no subdomain** (such as example.com) or explicitly says "the entire site," "all subdomains," or "the entire domain."
- For an IP address or network block, use kind=ip / cidr and value=the IP address or CIDR.
- **Do not** register company scope (company). At task creation, the company usually does not yet exist in the asset system, so registration would fail. Leave company-wide scope for the later planning stage.
Other rules:
- Register only scope **explicitly stated in the goal/description**; never invent or infer an unmentioned domain or IP address.
- In reason, briefly cite the phrase that establishes the scope for auditability.
- If the goal/description contains no explicit asset scope, **do not** call add_task_scope.
Register any scope with add_task_scope first, then submit the goals with set_goals.`

// goalsSystem assembles the goals-decomposer system prompt: the rendered body
// [A] (DB-overridable), the code-owned scope-extraction tail when add_task_scope
// is wired (withScope), and the code-owned Korean output-language tail [C] last —
// mirroring chatSystem/plannerSystem so a DB-edited body can never drop the tail.
// DecomposeGoalsWithProvider and the localization test share this one assembly, so
// the langDirective tail can't drift between runtime and test. EngagementDescription
// is intentionally left empty: the task description rides in the user message, not
// the {{.EngagementDescription}} var (see DecomposeGoalsWithProvider).
func goalsSystem(dataDir string, withScope bool) string {
	sys := renderSystem("goals", goalsDefaultTmpl, GoalsVars{DataDir: dataDir, Now: nowStr()})
	if withScope {
		sys += goalsScopeTail
	}
	return sys + langDirective(resolveOutputLang())
}

// GoalSpec is one decomposed objective.
type GoalSpec struct {
	Text      string `json:"text"`
	VulnClass string `json:"vulnclass,omitempty"`
}

// DecomposeGoals asks the LLM to break a pentest task goal into discrete,
// independently-verifiable objectives (each becomes a goal node). Returns nil if
// no provider is configured or the call yields nothing — the caller then falls
// back to a rule-based split so goal nodes always exist.
//
// prov is supplied by the caller (rather than built here from a Config) so goal
// decomposition rides the SAME provider instance as the rest of the engine — it
// shares the rate limiter, gets recorded by llmrec, and participates in LLM
// failover instead of quietly bypassing all three.
//
// desc is the task's free-text description (target scope, flag count,
// engagement rules, and other background).
// It is fed alongside the goal so the decomposer no longer splits blind — the
// prompt still forbids inventing anything the two texts don't state.
//
// emit, when non-nil, receives every LLM step (thinking/tool_use/result) with
// Worker="planner" so the round-0 goal-decomposition activity is visible in the UI.
//
// as + taskID, when non-nil/positive, wire the add_task_scope tool so the
// decomposer can register the explicit asset scope it extracts from the goal.
//
// ts is the task's exploration store: set_goals writes the decomposed goal nodes
// straight into it (the same managed tool the main agent uses to add goals at
// runtime). The returned specs are read back from the store so callers can emit
// per-goal activity and detect the "LLM produced nothing" case for their fallback.
func DecomposeGoals(ctx context.Context, prov llm.Provider, dataDir, goalText, desc string, as *db.AssetStore, ts *db.ExplorationStore, taskID int64, emit func(db.Activity)) []GoalSpec {
	if prov == nil {
		return nil
	}
	return DecomposeGoalsWithProvider(ctx, prov, dataDir, goalText, desc, as, ts, taskID, false, 0, emit)
}

// DecomposeGoalsWithProvider is the task-runtime variant used when a task has an
// ordered provider chain. It preserves the same tools and write behavior while
// letting the caller own provider selection/failover. maxTokens is the profile's
// per-reply output cap (0 = send none).
func DecomposeGoalsWithProvider(ctx context.Context, prov llm.Provider, dataDir, goalText, desc string, as *db.AssetStore, ts *db.ExplorationStore, taskID int64, nonStreaming bool, maxTokens int, emit func(db.Activity)) []GoalSpec {
	if prov == nil {
		return nil
	}
	// Goal decomposition is a one-shot call without a transcript store, so
	// agentcore does not attach a session ID to ctx (it does so only with a writer;
	// see agentcore.Prompt). Gateways that cache prompts or use sticky routing by
	// session-id header read that value from ctx. For example, opencode zen returns
	// 400 MissingSessionID without x-opencode-session. Without it, chat works but
	// decomposition fails. Attach a stable ID shared by decomposition requests in
	// the same exploration for caching, distinct from planner/worker IDs so
	// llmrec.parseSession can attribute it correctly.
	if ts != nil {
		ctx = transcript.WithSessionID(ctx, fmt.Sprintf("exp%d-goals", ts.ID()))
	}
	// worker="goals" tags the goal nodes' provenance; ts/taskID let set_goals link
	// each goal under the task root. This is the catalog's real set_goals tool, so a
	// web-edited description/schema on it applies here too.
	tsx := &ToolSet{as: as, ts: ts, taskID: taskID, worker: "goals"}
	// Wire add_task_scope only when we have a real asset store + task to write to.
	// goalsSystem appends the scope-extraction tail in lockstep (withScope) so the
	// prompt never asks for a tool that isn't present, and it owns the output-language
	// tail last so a DB-edited body can't drop it. Description rides in the user
	// message, NOT the {{.EngagementDescription}} var, so a prompt can't inject it twice.
	withScope := as != nil && taskID > 0
	sys := goalsSystem(dataDir, withScope)
	// set_constraints is always available (it does not depend on the asset store).
	// The editable body already says to extract constraints before goals; wire the tool here.
	tools := []actool.CoreTool{tsx.setGoals(), tsx.setConstraints()}
	if withScope {
		tools = append(tools, tsx.addTaskScope())
	}
	userMsg := "Task goal:\n" + goalText
	if d := strings.TrimSpace(desc); d != "" {
		userMsg += "\n\nTask description (background that may include target scope, flag count, or engagement rules; use it only as context and do not invent anything it does not state):\n" + d
	}
	// Use captureRun so every LLM step is emitted as an activity record (visible in
	// the plan tab under the round-0 marker). Falls back gracefully when emit is nil.
	captureEmit := func(r db.Activity) {
		if emit != nil {
			r.Worker = "planner"
			emit(r)
		}
	}
	captureRun(ctx, agentcore.Options{
		Provider:               prov,
		SystemPrompt:           []string{sys},
		Tools:                  tools,
		PermissionMode:         acperm.ModeBypass,
		DisableBackgroundTasks: true,
		// The three steps (extract constraints, register scope, decompose goals) each
		// need a tool call; allow enough turns to call set_goals before wrap-up.
		MaxTurns:     8,
		NonStreaming: nonStreaming, // Use Provider.Complete for a non-streaming profile.
		MaxTokens:    maxTokens,    // 0 = omit the cap and use the server default.
	}, userMsg, captureEmit)
	// set_goals persisted the goals directly; read them back so the caller sees what
	// was written (empty slice ⇒ the LLM produced nothing ⇒ caller falls back).
	if ts == nil {
		return nil
	}
	nodes, _ := ts.ListByKind(db.KindGoal, 10000)
	var out []GoalSpec
	for _, n := range nodes {
		var p struct {
			Text      string `json:"text"`
			VulnClass string `json:"vulnclass"`
		}
		_ = json.Unmarshal(n.Payload, &p)
		if strings.TrimSpace(p.Text) != "" {
			out = append(out, GoalSpec{Text: p.Text, VulnClass: p.VulnClass})
		}
	}
	return out
}

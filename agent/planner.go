package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/intercept"
	"github.com/Autumn-27/norma/agentcore"
	"github.com/Autumn-27/norma/llm"
	"github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
	"github.com/Autumn-27/norma/transcript"
)

// Planner is the event-driven LLM planner (docs §4.3): each time the asset or
// exploration graph changes (debounced), it reads the exploration route, queries
// assets, judges whether the task goal is met, and emits 0..N exploration intents
// into the frontier. It is the sole intent generator.
type Planner struct {
	findingRecorder   FindingRecorder
	prov              llm.Provider
	model             string
	tx                *transcript.Store                      // raw LLM conversation persistence (nil = off)
	window            int                                    // context window in tokens (for compaction)
	windowFn          func() int                             // optional dynamic task-chain minimum
	maxTurns          int                                    // max agent turns per run (0 = unlimited)
	killWork          func(intentID int64) error             // engine callback to terminate a running work (nil = off)
	steerWork         func(intentID int64, msg string) error // engine callback to steer a running work mid-run (nil = off)
	proxyAddr         string                                 // recording proxy for WebFetch (empty = direct)
	proxyCACert       string                                 // recording proxy's CA cert path (HTTPS verify)
	webSearch         WebSearchOpts                          // web_search tool backend selection (off by default)
	workDir           string                                 // shared work dir (surfaced in prompt as artifact-output target)
	injectConstraints func() bool                            // resolver: inject task operation constraints into system prompt? (nil = yes)
	nonStreamingFn    func() bool                            // resolver: use non-streaming (Complete) path? (nil = streaming)
	noaEnabledFn      func() bool                            // resolver: use experimental noa compaction? (nil = off)
	maxTokensFn       func() int                             // resolver: per-reply output cap (nil/0 = send no cap)
	compactor         *Compactor                             // cold-node compaction (§7); nil = disabled

	// todos keeps ONE plan-scratchpad per task (keyed by exploration id) so the
	// planner's multi-step plan survives across wake-ups — each Plan() is a fresh
	// session, but the shared store lets it record a serial exploit chain once and
	// dispatch it step-by-step over rounds instead of front-loading it in parallel.
	todoMu sync.Mutex
	todos  map[int64]*actool.TodoStore
}

func NewPlanner(prov llm.Provider, model, workDir string, tx *transcript.Store, window, maxTurns int) *Planner {
	return &Planner{prov: prov, model: model, workDir: workDir, tx: tx, window: window, maxTurns: maxTurns, todos: map[int64]*actool.TodoStore{}}
}

func (p *Planner) SetCompactionWindowResolver(fn func() int) { p.windowFn = fn }

// SetCompactor wires the cold-node compactor (cold-digest §7). Called each
// planner wake-up to advance the round counter, maintain cold stamps, and
// (off the hot path) fold cold nodes into digests. nil = feature disabled.
func (p *Planner) SetCompactor(c *Compactor) { p.compactor = c }

// SetNonStreaming wires a resolver deciding whether runs use the non-streaming
// model path (true = non-streaming). nil/unset = streaming (default).
func (p *Planner) SetNonStreaming(fn func() bool) { p.nonStreamingFn = fn }

func (p *Planner) nonStreaming() bool { return p.nonStreamingFn != nil && p.nonStreamingFn() }

// SetNoaEnabled wires a resolver deciding whether runs use the experimental noa
// context-compression mechanism. nil/unset = off (built-in compaction). Read per
// run so the settings toggle takes effect without rebuilding the agent.
func (p *Planner) SetNoaEnabled(fn func() bool) { p.noaEnabledFn = fn }

// SetMaxTokens wires a resolver for the per-reply output cap. nil/unset or 0 =
// send no cap and let the endpoint decide. Read per run, like nonStreaming.
func (p *Planner) SetMaxTokens(fn func() int) { p.maxTokensFn = fn }

func (p *Planner) maxTokens() int {
	if p.maxTokensFn == nil {
		return 0
	}
	return p.maxTokensFn()
}

func (p *Planner) compactionWindow() int {
	if p.windowFn != nil {
		return p.windowFn()
	}
	return p.window
}

// SetProxy points the planner's WebFetch at the recording proxy plus the CA cert
// it trusts to verify HTTPS through it (empty addr = direct).
func (p *Planner) SetProxy(addr, caCert string) { p.proxyAddr, p.proxyCACert = addr, caCert }

// SetWebSearch selects the web_search backend for the planner (off by default).
func (p *Planner) SetWebSearch(o WebSearchOpts) { p.webSearch = o }

// SetConstraintInject wires a resolver deciding whether this task's operation
// constraints get injected into the planner system prompt. Read per round so the
// settings toggle takes effect without rebuilding the agent. nil = inject (default).
func (p *Planner) SetConstraintInject(fn func() bool) { p.injectConstraints = fn }

// wantConstraints reports whether constraint injection is enabled (default yes).
func (p *Planner) wantConstraints() bool { return p.injectConstraints == nil || p.injectConstraints() }

// todoFor returns the task's persistent planning todo store, creating it on first
// use. Shared across all of this task's planner wake-ups.
func (p *Planner) todoFor(expID int64) *actool.TodoStore {
	p.todoMu.Lock()
	defer p.todoMu.Unlock()
	s := p.todos[expID]
	if s == nil {
		s = actool.NewTodoStore()
		p.todos[expID] = s
	}
	return s
}

// SetKillWork wires the engine's per-work terminate callback so the planner's
// kill_work tool can stop a single running worker.
func (p *Planner) SetKillWork(fn func(intentID int64) error) { p.killWork = fn }

// SetSteerWork wires the engine's per-work steering callback so the planner's
// steer_work tool can inject a mid-run course-correction into a running worker.
func (p *Planner) SetSteerWork(fn func(intentID int64, msg string) error) { p.steerWork = fn }

// renderPlannerTodos formats the persistent planning todo for injection into the
// wake-up prompt (empty when there are no todos yet — first wake-up).
func renderPlannerTodos(items []actool.Todo) string {
	if len(items) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\nYour planning to-do list (retained across wake-ups; written in a previous round):\n")
	for _, it := range items {
		mark := map[actool.TodoStatus]string{actool.TodoPending: "☐", actool.TodoInProgress: "▶", actool.TodoCompleted: "✔"}[it.Status]
		if mark == "" {
			mark = "☐"
		}
		b.WriteString(fmt.Sprintf("  %s %s\n", mark, it.Content))
	}
	b.WriteString("Use this list to proceed: dispatch the next intent only when its prerequisite steps are complete or its required facts exist. Update the list with TodoWrite, marking steps satisfied by facts as completed. Do not dispatch steps already marked pending/in_progress again.")
	return b.String()
}

// TriggerEvent describes what concretely caused this planning round to fire, so
// the planner looks first at the actual change instead of re-scanning the whole
// overview. Kind:
//
//	"done"    — a worker finished intent IntentID (its output conclusion is fetched).
//	"finding" — a worker reported a finding on intent IntentID (Detail = summary).
//	"goal"    — the human added one or more goals in one set_goals call through
//	            the main agent (Goals = the new goal texts; set_goals supports batches).
//	"goal_deleted" — the human deleted a goal in the overview (Detail = deleted goal text).
//	"goal_edited"  — the human edited a goal in the overview (OldGoal and NewGoal texts).
//	"cancelled" — the human deleted intent IntentID (Detail = deletion reason). The intent is
//	            stopped (not deleted) and the reason is attached to it as a fact.
type TriggerEvent struct {
	Kind     string
	IntentID int64
	Detail   string
	Summary  string   // Kind=="cancelled": summary captured before deletion, while the node was available.
	Goals    []string // Kind=="goal": one or more goal texts added by this set_goals call.
	OldGoal  string   // Kind=="goal_edited": goal text before the edit.
	NewGoal  string   // Kind=="goal_edited": goal text after the edit.
	Hints    []string // Kind=="hint": one or more hints added by this add_hint call.
}

// renderTriggers spells out the change(s) that fired this round: for a finished
// worker — which intent + its output conclusion; for a finding — which intent +
// what was found. Empty for time/heartbeat wakes. Reads the store (best-effort;
// a blank field never blocks the round).
func renderTriggers(ts *db.ExplorationStore, evs []TriggerEvent) string {
	if len(evs) == 0 || ts == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\nChanges that triggered this round (review these before deciding whether to add directions):")
	for _, ev := range evs {
		switch ev.Kind {
		case "goal":
			if len(ev.Goals) == 1 {
				b.WriteString(fmt.Sprintf("\n- The human (through the main agent) added a goal: %s. This is a new outcome to achieve; add an exploration direction if no intent covers it.", ev.Goals[0]))
			} else {
				b.WriteString(fmt.Sprintf("\n- The human (through the main agent) added %d goals: %s. For each new outcome without a covering intent, add an exploration direction.", len(ev.Goals), strings.Join(ev.Goals, "; ")))
			}
		case "hint":
			if len(ev.Hints) == 1 {
				b.WriteString(fmt.Sprintf("\n- The human (through the main agent) added a strategic hint: %s. It is attached to the exploration graph; adjust or add a direction if no intent covers it.", ev.Hints[0]))
			} else {
				b.WriteString(fmt.Sprintf("\n- The human (through the main agent) added %d strategic hints: %s. They are attached to the exploration graph; consider each when adjusting or adding directions.", len(ev.Hints), strings.Join(ev.Hints, "; ")))
			}
		case "goal_deleted":
			b.WriteString(fmt.Sprintf("\n- The human deleted the goal: %s. Reassess the remaining goals and directions; do not dispatch intents for the removed goal.", ev.Detail))
		case "goal_edited":
			b.WriteString(fmt.Sprintf("\n- The human changed a goal from %q to %q. Adjust exploration directions to match; stop dispatching an old direction if it no longer applies.", ev.OldGoal, ev.NewGoal))
		case "finding":
			b.WriteString(fmt.Sprintf("\n- The worker for intent #%d (%s) reported a finding: %s", ev.IntentID, intentSummary(ts, ev.IntentID), ev.Detail))
		case "cancelled":
			// Prefer the summary captured at deletion; the node may no longer exist.
			sm := ev.Summary
			if sm == "" {
				sm = intentSummary(ts, ev.IntentID)
			}
			b.WriteString(fmt.Sprintf("\n- The human deleted intent #%d (%s). Reason: %s. It will no longer run; replan accordingly.", ev.IntentID, sm, ev.Detail))
		default: // "done"
			b.WriteString(fmt.Sprintf("\n- The worker for intent #%d (%s) finished. Conclusion: %s", ev.IntentID, intentSummary(ts, ev.IntentID), workerOutput(ts, ev.IntentID)))
			if fids := factIDsYielded(ts, ev.IntentID); fids != "" {
				b.WriteString(fmt.Sprintf("; new fact IDs from this intent: %s ", fids))
			}
		}
	}
	b.WriteString("\n(Use node_detail / get_worker_output / list_findings for full details.)")
	return b.String()
}

// factIDsYielded lists the fact IDs an intent produced this run as "#12, #15", so the
// planner can jump straight to the round's incremental facts. Empty (best-effort) when
// the intent yielded no facts or the lookup fails.
func factIDsYielded(ts *db.ExplorationStore, id int64) string {
	ids, err := ts.FactsYielded(id)
	if err != nil || len(ids) == 0 {
		return ""
	}
	parts := make([]string, len(ids))
	for i, fid := range ids {
		parts[i] = fmt.Sprintf("#%d", fid)
	}
	return strings.Join(parts, ", ")
}

// intentSummary reads an intent node's one-line summary (best-effort, "?" on miss).
func intentSummary(ts *db.ExplorationStore, id int64) string {
	n, err := ts.GetNode(id)
	if err != nil || n == nil {
		return "?"
	}
	var p map[string]any
	if json.Unmarshal(n.Payload, &p) == nil {
		if s, ok := p["summary"].(string); ok && s != "" {
			return s
		}
	}
	return "?"
}

// workerOutput returns the finished worker's conclusion for an intent — the last
// 'result' (else 'text') activity's full detail, truncated. Same source get_worker_output uses.
func workerOutput(ts *db.ExplorationStore, id int64) string {
	acts, _, err := ts.ActivityList(&id, 0, 1000)
	if err != nil {
		return "(failed to retrieve output)"
	}
	var pick *db.Activity
	for i := range acts {
		if acts[i].Kind == "result" {
			pick = &acts[i]
		} else if acts[i].Kind == "text" && pick == nil {
			pick = &acts[i]
		}
	}
	if pick == nil {
		return "(this work has no recorded output yet)"
	}
	out, _ := ts.ActivityDetail(pick.ID)
	if out == "" {
		out = pick.Summary
	}
	return truncOutput(out, 800)
}

// truncOutput caps a worker-output blob so the trigger context doesn't bloat the
// system prompt every round; full text is one get_worker_output call away.
func truncOutput(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + " … (truncated; use get_worker_output for the full text)"
}

// renderGraphOverview folds the pre-computed graph_overview snapshot into the
// wake-up prompt so the planner starts each round with the full situation in
// hand — saving the round-trip it would otherwise spend calling the tool. It is
// the exact same JSON graph_overview would return; deeper detail is still one
// tool call away (node_detail / list_facts / …).
func renderGraphOverview(data map[string]any) string {
	b, err := json.Marshal(data)
	if err != nil {
		return "" // fall back to the model calling graph_overview itself
	}
	return "\n\nCurrent round overview (prefetched graph_overview result; use node_detail/list_facts as needed for details):\n" + string(b)
}

// plannerDefaultTmpl is the built-in EDITABLE body (section [A]) of the planner prompt,
// seeded into agent_prompts. Goal is a {{.Goal}} template var; the intermediate-artifact
// tail is code-owned (artifactSpec) and appended by plannerSystem after rendering.
const plannerDefaultTmpl = `You are the "planner" in an authorized penetration-testing system on a cybersecurity platform. You are awakened frequently whenever the graph changes. Your duties: read the situation, assess goals, and **add exploration intents only for genuinely new, uncovered directions**. You plan rather than execute: this round may produce only intent creation/refinement or goal assessment. Never carry out the exploration work in the planning round.

Task goal: {{.Goal}}

**Decide how many intents this round needs first:**
- **Hard floor (highest priority):** If a goal is unmet and there are no open or running intents (frontier_open=0 and running_intents is empty), you **must** produce at least one intent that advances the goal this round. With no running work to await and no queued direction, producing zero intents stalls the task. Even if all known directions appear only in recent_done, use the done/exhausted/blocked rules below to open or resume a direction.
- Outside that hard floor, **zero intents is a valid result only with a sound reason**, not a default preference for dispatching less: (1) **Already covered:** every direction you considered has an open/running intent. Rephrasing an existing intent as a duplicate is a serious error. (2) **Waiting on a dependency:** the next step requires output from work that is still running. Dispatching it now would leave the downstream worker without its prerequisite; wait for the next graph update.
- Conversely, dispatch a direction that is **uncovered and independent of running work**, or one that addresses an untested area in scope while a goal remains unmet. Do not use zero intents as an easy default.

**Decision process on each wake-up:**

1. **The full situation is attached below this prompt** as the graph_overview result; do not call it again. It includes task (original title, goal, and root node), asset counts, goals and statuses, open/running/recent_done intents, sites_without_endpoints (sites with no endpoints, suggesting possible directions), facts (exploration-fact count, distinct from findings), and recent_facts ({id,summary,confidence?}).
   - **Scope:** exploration nodes (goals/intents/facts/findings) belong only to this task. **The asset graph is shared globally** across tasks; asset counts reflect in-scope global assets, not assets unique to this task. Ignore assets unrelated to this task.
   - **Lineage:** each intent has parents (the upstream facts/intents it derives from) and yields (the downstream facts/findings it produced). Each recent_fact has from_intent. Use these to see which direction produced each fact and whether facts combine into a new direction.
   - **Negative or uncertain observations** in recent_facts (such as "port closed" or "not injectable") are worker observations, not final conclusions. Before accepting one, inspect its evidence with node_detail(id). Treat a direction as temporarily closed only if the evidence is solid, confidence=observed, and the methods were exhausted. If evidence is missing, the worker merely says "it looks like" or tried once, or confidence=inferred, treat it as **unresolved**. If it remains in scope and no other intent covers it, dispatch one review intent to confirm or refute it. **Review the same negative direction at most once**; if the review is also negative with reasonable evidence, respect that conclusion and do not dispatch it again.
   - **Fetch deeper detail only as needed:** list_facts (paginated, newest first, default 20, filter with q, page with before, includes total/has_more), list_findings (all findings), node_detail(id) (full evidence/detail; lists and recent_facts give summaries only), list_assets (pull with q search, type/company_id/task_id filters and pagination, or fetch by id/ids), and asset_neighbors. The asset graph is shared; do not fetch it all by default.

2. **Assess goals (core duty):** the goals field includes goals and statuses. For an unmet goal proven by a finding or fact, call prove_goal(goal_id, evidence_id, reason) to mark it met. **When you mark the final unmet goal, the system automatically completes the entire task.** Completion is driven by proving each goal; there is no one-click completion tool.
   - **Check quantitative acceptance criteria; never mark them met early.** If a goal specifies a measurable condition (X% coverage, N flags, a particular privilege), **verify the measured values** in graph_overview (coverage.pct, findings_total, etc.) before prove_goal. If the condition is not met, **do not** call prove_goal; dispatch intents to close the gap. Do not mark met because the goal is "mostly achieved" or the "main part is done." Example: required coverage is 100% but coverage.pct=40%; the goal is unmet, so dispatch more coverage work.

3. **Optional, very light discovery only at the start:** use Bash or similar for a very small number of read-only target checks (such as one or two curl requests for the homepage or fingerprint) only when the task is just starting, the graph has almost no facts (recent_facts is mostly empty), and the situation alone cannot make the initial intent specific. **The only valid output is a more precise one-sentence intent description.** Do not discover, verify, or exploit vulnerabilities, and do not enumerate endpoints, directories, or parameters; dispatch those as worker intents. Three hard limits:
   - Once a worker fact exists (facts>0 or recent_facts is nonempty), **do not probe yourself again**. Base decisions on existing facts. This round may only dispatch new intents or finish. To investigate a lead, send a worker an intent rather than running curl yourself.
   - Even at the start, stop after at most three checks, solely to clarify the initial intent. If you begin deep investigation instead of quickly choosing a direction (enumerating endpoints or directories one by one, trying IDs one by one, decoding a chain, repeatedly probing one API, or testing injection, unauthorized access, or any vulnerability), stop immediately and turn that work into an intent.
   - If the existing facts and situation suffice, do not probe.

4. **Choose which new directions to add:** "restraint" here means **do not duplicate existing intents**; it does not mean "dispatch as little as possible." While goals remain unmet, ask which deeper, stronger, uncovered approaches could advance them, rather than whether it is time to stop. An intent is an **open-ended exploration direction**, not a fixed type or menu item. Derive directions from known facts, assets, and goals, and compare each with open + running + recent_done:
   - If an open/running intent covers the direction, do not generate it again.
   - If it appears in recent_done, **inspect that intent's state to understand why it stopped before deciding:**
     - **done (finished normally):** the direction was covered; do not dispatch it unchanged. Judge whether it is a dead end from the facts in yields, not state alone. Dispatch it again only when a **materially new mechanism** appears (new fact/asset/parameter or a clearly different approach), and explain the difference in summary. Rewording it or hoping another attempt works does not count; do not retry on that basis.
     - **exhausted (budget ran out during exploration; only partial results were written) / blocked (model or network failure; little exploration happened):** both stopped prematurely with incomplete information. First use get_worker_trace / get_worker_output to see actual progress and the obstacle. If the budget cut off a near-breakthrough, dispatch an intent to continue from the prior progress. If an external failure prevented the attempt (often blocked), retry the same direction. If it repeatedly stalls in the same place, change approach or direction. Base the choice on the real trace, not state alone.
   - Generate a genuinely new direction that no intent covers.
   - If still-open/running intents cover every known direction, generate none and end this round; work is running or queued. If only recent_done covers them, no intents are open/running, and a goal is unmet, the hard floor above requires opening or resuming a direction.
   - **Depth takes priority over coverage:** coverage is a floor or acceptance criterion, not the exploration goal itself. After finding a high-value entry point that might lead to RCE, privilege escalation, or data exposure, prioritize an intent to follow that path deeply rather than spreading shallow tests across assets merely to raise coverage.
   - **Keep approaches diverse; do not converge too early:** while a goal is unmet, if existing intents cluster on one route or entry point and a **substantively different** direction is uncovered (another entry point, asset class, or exploitation chain), prioritize that divergent direction over a synonymous intent on the same route. Judge substance, not wording; if an existing intent covers it, do not generate it. Ideally, two or three routes with different mechanisms coexist (such as an upload chain and an authentication bypass). Concentrate resources on one route after it produces evidence of progress toward the goal. **Diversity always obeys the operational constraints above:** never create an intent for an excluded entry point, port, host, or action, even if it is different.

   **Dispatch serial exploitation chains step by step, not in parallel.** For a chain with strong dependencies (1 → 2 → 3, where each step needs the previous result), do not dispatch all steps at once; downstream workers would lack prerequisites and repeat work or stall. Record the entire chain with TodoWrite (one step per item). This round dispatch only the step whose prerequisites are satisfied (usually the first). After it yields a fact, dispatch the next step on a later wake-up (the to-do list will be in the prompt) and mark satisfied steps completed. Do not split one action into two intents ("confirm the trigger" and "trigger it" are the same step). Use parallel intents only for independent dimensions, such as unrelated endpoints.

5. **Submit:** call add_intent **once** with the selected directions in an intents array, up to four highest-value directions. Do not make separate calls for each:
   - **summary:** describe the direction in one natural-language sentence (full address of the target + what to do + why), without forcing it into a fixed category. Compare this mainly with existing summaries to avoid duplicates.
   - **asset_ids:** IDs of assets this direction will test or attack (provide them whenever possible; zero, one, or more from list_assets). If a direction concerns a concrete site, API, parameter, or host, include its asset IDs for coverage deduplication and links in the asset graph. Include all assets for a cross-asset direction. Leave empty only for purely global reconnaissance with no specific asset.
   - **parent_ids:** optional upstream node IDs that led to this direction (zero, one, or more). Include all contributing facts when combining them into one intent, and include the ID of an upstream intent or finding when deriving from it. Leave empty for a wholly new top-level direction.

Avoid duplication or forced additions; when a goal is unmet and a deeper uncovered approach exists, dispatch it. Be concise, focused, and efficient.`

func plannerSystem(goal, dataDir, workDir string) string {
	body := renderSystem("planner", plannerDefaultTmpl, PlannerVars{Goal: goal, DataDir: dataDir, Now: nowStr()})
	return body + artifactSpec(workDir) + langDirective(resolveOutputLang())
}

// Plan runs one planning round. emit, if non-nil, receives the planner's execution
// steps (so users can see how it reads the situation and judges goals — the
// planner is the intent generator and was previously a black box). Returns whether
// the planner judged the goal met.
// triggers carries the concrete change(s) that fired this round — worker(s) done
// and/or finding(s) reported (may be several — the engine debounces a burst; empty
// for time/heartbeat wakes). They are spelled out at the top of the prompt so the
// planner looks first at the actual change (which intent, its output/finding).
func (p *Planner) Plan(ctx context.Context, taskID int64, as *db.AssetStore, ts *db.ExplorationStore, goal string, triggers []TriggerEvent, emit func(db.Activity)) (met bool, reason string, err error) {
	// cold-digest §2.3/§7: advance this task's planner-round counter, maintain the
	// cold_since_round stamps, and (if a threshold is hit) kick off background
	// compaction. Synchronous part is cheap (a few queries); the LLM compaction
	// runs in a detached goroutine so it never adds latency to this round.
	p.compactor.OnPlannerRound(ctx, ts)
	tsx := NewToolSet(ts, "planner")
	tsx.SetFindingRecorder(p.findingRecorder)
	if as != nil {
		tsx.SetAssetStore(as, as.Companies())
	}
	tsx.SetTaskID(taskID)
	tsx.SetCoverageEnabled(as == nil || as.CoverageEnabled(taskID))
	tsx.killWork = p.killWork   // enable kill_work tool (nil = unavailable)
	tsx.steerWork = p.steerWork // enable steer_work tool (nil = unavailable)
	if origin, _ := ts.OriginFactID(); origin > 0 {
		tsx.SetOwnerNode(origin) // planner-side anchors default to the task root (origin fact)
	}
	// Domain tools plus the default tool set (Read/Write/Edit/MultiEdit/LS/Glob/Grep/Bash).
	// Remove add_task_scope/list_untested_assets when asset coverage is disabled.
	base := append(tsx.DropCoverageTools(tsx.PlannerTools()), actool.DefaultTools()...)
	ctx = WithRunInfo(ctx, RunInfo{TaskID: taskID, ExplorationID: explorationID(ts)})
	tools, def, cleanup := AugmentTools(ctx, "planner", base)
	defer cleanup()
	// Put the key situation (recently finished intents and prefetched graph) in
	// this round's user input, leaving only static planning text in system. This
	// keeps system stable for caching; a long round might compact the situation,
	// but planner rounds are usually short. Append situational to input below.
	situational := renderTriggers(ts, triggers) + renderGraphOverview(tsx.graphOverviewData())
	// Task deadline/final mode comes from ctx (see taskclock.go). In the final
	// round, append the task-timeout wrap-up as an instruction in this round's
	// user input so the planner only assesses goals and creates no new intents.
	tc := taskClockFrom(ctx)
	if tc.Final {
		situational += "\n\nFinal task wrap-up (special instruction for this round; overrides the normal planning process above): " + resolveTaskTimeoutWrapup("planner")
	}
	// Create this task's working directory at <workDir>/tasks/<taskID>.
	taskDir := ensureRunDir(p.workDir, taskID, 0)
	ctx = intercept.WithReviewContext(ctx, taskDir, intercept.ReviewBackground{})
	sysBody := plannerSystem(goal, p.workDir, taskDir)
	if p.wantConstraints() {
		sysBody += constraintBlock(ts) // Inject any operational constraints into system.
	}
	system, boundary := deferredSystem(sysBody, def)
	// The planner has no separate wall-clock budget. If a task deadline exists,
	// cap MaxDuration at the time remaining so an active round enters wrap-up
	// at the deadline (task-timeout wording for time, per-run wording for turns).
	maxDur, clamped := clampMaxDuration(tc.DeadlineUnix, 0)
	settle := wrapupSettlement("planner", nil)
	if tc.DeadlineUnix > 0 {
		settle = wrapupSettlementForTask("planner", nil, clamped)
	}
	opts := agentcore.Options{
		Provider:        p.prov,
		SystemPrompt:    system,
		DynamicBoundary: boundary,
		Tools:           tools,
		DeferredTools:   def.Deferred,
		UnlockSet:       def.Unlock,
		PermissionMode:  permission.ModeBypass,
		EnableWebFetch:  true, // Use recording proxy and its CA to verify re-signed HTTPS certificates.
		WebFetchProxy:   p.proxyAddr,
		WebFetchCACert:  p.proxyCACert,
		// Optional web search: ddgs needs no key; brave-free needs BraveKey; tavily needs TavilyKey.
		// WebSearchProxy is a separate egress proxy (http/https/socks5); empty means direct.
		EnableWebSearch:       p.webSearch.Enabled,
		WebSearchBackend:      p.webSearch.Backend,
		BraveSearchAPIKey:     p.webSearch.BraveKey,
		TavilySearchAPIKey:    p.webSearch.TavilyKey,
		DeepSeekSearchBaseURL: p.webSearch.DeepSeekBaseURL,
		DeepSeekSearchAPIKey:  p.webSearch.DeepSeekAPIKey,
		DeepSeekSearchModel:   p.webSearch.DeepSeekModel,
		WebSearchProxy:        p.webSearch.Proxy,
		BashEnv:               proxyEnv(p.proxyAddr, p.proxyCACert), // Bash subprocesses use proxy and trust its CA.
		WorkingDir:            taskDir,                              // This task's <workDir>/tasks/<taskID> directory.
		ToolOutputDir:         cmdOutDir(taskDir),
		MaxTurns:              p.maxTurns, // 0 = unlimited (configurable in agent management)
		MaxDuration:           maxDur,     // 0 = unlimited; with deadline, time remaining.
		Compaction:            compactionConfig(p.compactionWindow()),
		// Shared to-do store retains serial chains across wake-ups (new session, same store).
		Todos: p.todoFor(ts.ID()),
		// On this round's turn-budget exhaustion, the SDK wraps up by recording
		// decided actions (add_intent, provable prove_goal, serial-chain TodoWrite).
		// The planner can be awakened again. If clamped by task deadline, use
		// PromptByReason instead (see wrapupSettlementForTask).
		Settlement:   settle,
		NonStreaming: p.nonStreaming(), // Use Provider.Complete for a non-streaming profile.
		MaxTokens:    p.maxTokens(),    // 0 = omit the cap and use the server default.
	}
	if p.tx != nil { // persist raw LLM conversation; one accumulating file per task's planner
		opts.Transcript = p.tx
		opts.SessionID = fmt.Sprintf("exp%d-planner", ts.ID())
	}
	// Experimental noa compaction keeps persistent archives under <workDir>/noa/<SessionID>.
	noaSession := fmt.Sprintf("exp%d-planner", ts.ID())
	enableNoa(&opts, p.noaEnabledFn, p.workDir, noaSession, noaWarn(noaSession))
	// Put the situation (finished intents and full graph) in this round's user input,
	// along with instructions and the persistent to-do list. The to-do list is the
	// model's planning scratchpad and can be rebuilt. Lead with actual changes if
	// present; on heartbeat/hint/resume without a change, review running intents.
	lead := "A concrete change just occurred (see 'Changes that triggered this round' below). Plan the next step accordingly:"
	if len(triggers) == 0 {
		lead = "This wake-up is a **scheduled heartbeat / has no concrete change signal**; the graph may be unchanged. Also review running intents: use steer_work to correct stalled or drifting work, or kill_work if the entire direction is wrong. Then assess goals and decide whether to add directions:"
		// On heartbeat/no-change wake-ups with no open or running intents, explicitly
		// tell the planner exploration has stalled and require a new direction.
		if active, err := ts.HasActiveIntent(); err == nil && !active {
			lead = "This wake-up is a **scheduled heartbeat**, and there are **no open or running intents**. No worker is running and no direction is queued, so exploration has stalled. You **must** produce at least one new intent that advances the goal without duplicating graph intents (zero intents is forbidden). First assess the goals from the situation below; if unmet, add a direction now:"
		}
	}
	input := lead + situational + "\n\nAssess goals using the situation above. When a goal is **truly met** (the requested outcome was obtained or the target vulnerability confirmed), mark it with prove_goal. **Hard floor: whenever a goal remains unmet and there are no open or running intents (frontier_open=0 and running_intents is empty), produce at least one intent that advances the goal this round. There is no running work to await and no queued direction; zero intents would stall the task. You may produce no new intent only when an open/running intent is making progress or all goals are met.**" +
		renderPlannerTodos(opts.Todos.List())
	// At the wall-clock limit, MaxDuration now interrupts active tools and enters
	// wrap-up on a live ctx. No separate hard context deadline is needed; ctx
	// carries only pause, kill, and shutdown.
	_, _, err = captureRun(ctx, opts, input,
		func(r db.Activity) {
			if emit != nil {
				r.Worker = "planner" // planner activity has no intent_id (it generates them)
				emit(r)
			}
		})
	return tsx.GoalMet, tsx.Reason, err
}

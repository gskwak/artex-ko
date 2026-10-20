package agent

import (
	"context"
	"fmt"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/intercept"
	"github.com/Autumn-27/norma/agentcore"
	"github.com/Autumn-27/norma/llm"
	"github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
	"github.com/Autumn-27/norma/transcript"
)

// MainAgent is the thin human-interface orchestrator (docs §4.2 / §7). The human
// chats with it; it observes (read tools), and steers by injecting hints
// (→planner) or direct high-priority intents (→frontier). It does NOT run the
// autonomous intent-generation loop (that is the planner's job).
type MainAgent struct {
	findingRecorder FindingRecorder
	prov            llm.Provider
	model           string
	tx              *transcript.Store                      // raw LLM conversation persistence (nil = off)
	window          int                                    // context window in tokens (for compaction)
	windowFn        func() int                             // optional dynamic task-chain minimum
	maxTurns        int                                    // max agent turns per run (0 = unlimited)
	proxyAddr       string                                 // recording proxy for WebFetch (empty = direct)
	proxyCACert     string                                 // recording proxy's CA cert path (HTTPS verify)
	webSearch       WebSearchOpts                          // web_search tool backend selection (off by default)
	workDir         string                                 // shared work dir (surfaced in prompt as artifact-output target)
	steerWork       func(intentID int64, msg string) error // engine callback: steer a running work (nil = off)
	nonStreamingFn  func() bool                            // resolver: use non-streaming (Complete) path? (nil = streaming)
	noaEnabledFn    func() bool                            // resolver: use experimental noa compaction? (nil = off)
	maxTokensFn     func() int                             // resolver: per-reply output cap (nil/0 = send no cap)
}

// SetNoaEnabled wires a resolver deciding whether runs use the experimental noa
// context-compression mechanism. nil/unset = off (built-in compaction). Read per
// run so the settings toggle takes effect without rebuilding the agent.
func (m *MainAgent) SetNoaEnabled(fn func() bool) { m.noaEnabledFn = fn }

// SetNonStreaming wires a resolver deciding whether runs use the non-streaming
// model path (true = non-streaming). nil/unset = streaming (default).
func (m *MainAgent) SetNonStreaming(fn func() bool) { m.nonStreamingFn = fn }

func (m *MainAgent) nonStreaming() bool { return m.nonStreamingFn != nil && m.nonStreamingFn() }

// SetMaxTokens wires a resolver for the per-reply output cap. nil/unset or 0 =
// send no cap and let the endpoint decide. Read per run, like nonStreaming.
func (m *MainAgent) SetMaxTokens(fn func() int) { m.maxTokensFn = fn }

func (m *MainAgent) maxTokens() int {
	if m.maxTokensFn == nil {
		return 0
	}
	return m.maxTokensFn()
}

func NewMainAgent(prov llm.Provider, model, workDir string, tx *transcript.Store, window, maxTurns int) *MainAgent {
	return &MainAgent{prov: prov, model: model, workDir: workDir, tx: tx, window: window, maxTurns: maxTurns}
}

func (m *MainAgent) SetCompactionWindowResolver(fn func() int) { m.windowFn = fn }

func (m *MainAgent) compactionWindow() int {
	if m.windowFn != nil {
		return m.windowFn()
	}
	return m.window
}

// SetProxy points the main agent's WebFetch at the recording proxy plus the CA
// cert it trusts to verify HTTPS through it (empty addr = direct).
func (m *MainAgent) SetProxy(addr, caCert string) { m.proxyAddr, m.proxyCACert = addr, caCert }

// SetWebSearch selects the web_search backend for the main agent (off by default).
func (m *MainAgent) SetWebSearch(o WebSearchOpts) { m.webSearch = o }

// SetSteerWork wires the engine callback that lets the main agent's steer_work
// tool inject a mid-run course-correction into a running work (nil = tool off).
func (m *MainAgent) SetSteerWork(fn func(intentID int64, msg string) error) { m.steerWork = fn }

// mainAgentDefaultTmpl is the built-in EDITABLE body (section [A]) of the main
// agent prompt, seeded into agent_prompts. Goal is a {{.Goal}} template var;
// the intermediate-artifact rules are code-owned (artifactSpec) and appended
// after rendering.
const mainAgentDefaultTmpl = `You are the "main agent" in an authorized penetration-testing system and the interface for the human operator. You do not conduct exploration yourself or continuously generate intents on your own; that is the planner's job. Your duties are:

1. Observe: use graph_overview / list_findings / list_facts / list_assets / get_worker_output to answer questions about current progress.
2. Steer the system based on the human's requests:
   - If the human wants to "change direction / emphasize a vulnerability class / focus on an area," use add_hint to record guidance for the planner's next run.
   - If the human wants to "test a specific target now," use add_intent to inject one high-priority intent (priority 8-10). The system automatically resumes a completed task for a worker to execute this intent, then returns it to completed status.
     **When all task goals are already met** (all goals in graph_overview have status met): before dispatching the intent, decide whether it implies a "new outcome to achieve." If it does, briefly restate the goal you inferred and **ask whether the human wants to register it as a formal goal**. If yes, register it with set_goals; the task then returns to normal planning and the planner continues autonomously. If no, or if the human only wants a temporary check, dispatch only this intent with add_intent. After the worker finishes, the task returns to completed status without continuing autonomously. If the intent is clearly a one-time check with no new goal, call add_intent directly without asking every time.
   - If the human wants to "correct a running intent (work) in real time (stop doing X, focus on Y)," first inspect it with get_worker_output, then use steer_work. This takes effect before the worker's next action without interrupting it or losing progress. If the entire direction is wrong, use add_intent to dispatch a different intent instead.
   - If the human wants to "add a final outcome to achieve," add the goal with set_goals. The system writes it to the task graph and **automatically resumes a completed or paused task**; the planner will assess it next. The human need not click Resume.
   - If the human wants to "add or change testing constraints" (allowed or prohibited actions, such as "only test the current port," "no brute force," or "passive reconnaissance only"), register them with set_constraints (type=allow for permitted actions, type=deny for prohibited ones). Constraints are injected into planner/worker prompts on the next planning round to define exploration boundaries. They can also be added, edited, or removed under "Constraint Management" in the overview.
3. Reply concisely in plain language and explain what you did.

Current task goal: {{.Goal}}

Do not invent findings; answer only from actual data returned by tools.`

func mainAgentSystem(goal, dataDir, workDir string) string {
	body := renderSystem("mainagent", mainAgentDefaultTmpl, MainVars{Goal: goal, DataDir: dataDir, Now: nowStr()})
	return body + artifactSpec(workDir) + langDirective(resolveOutputLang())
}

// Chat handles one human message and returns the assistant reply. emit, if
// non-nil, receives each execution step (thinking / tool_use / tool_result /
// text / result) so the main-agent session shows its work — exactly like the
// worker/planner sessions — not just the final answer.
func (m *MainAgent) Chat(ctx context.Context, taskID int64, mainSeg int, as *db.AssetStore, ts *db.ExplorationStore, goal, message string, emit func(db.Activity), notify, resume func(), notifyGoal, notifyHint func([]string)) (string, error) {
	tsx := NewToolSet(ts, "human")
	tsx.SetFindingRecorder(m.findingRecorder)
	if as != nil {
		tsx.SetAssetStore(as, as.Companies())
	}
	tsx.SetTaskID(taskID)
	tsx.SetCoverageEnabled(as == nil || as.CoverageEnabled(taskID))
	tsx.SetNotify(notify)         // General debounced wake-up for writes without a dedicated callback.
	tsx.SetResumeTask(resume)     // New set_goals goals resume completed or paused tasks.
	tsx.SetNotifyGoal(notifyGoal) // New set_goals goals trigger the planner with a human-added-goal event.
	tsx.SetNotifyHint(notifyHint) // New add_hint hints trigger the planner with a human-added-hints event.
	tsx.steerWork = m.steerWork   // enable steer_work tool (nil = unavailable)
	// Domain tools plus the default tool set (Read/Write/Edit/MultiEdit/LS/Glob/Grep/Bash).
	// Remove add_task_scope/list_untested_assets when asset coverage is disabled.
	base := append(tsx.DropCoverageTools(tsx.MainAgentTools()), actool.DefaultTools()...)
	ctx = WithRunInfo(ctx, RunInfo{TaskID: taskID, ExplorationID: explorationID(ts)})
	tools, def, cleanup := AugmentTools(ctx, "mainagent", base)
	defer cleanup()
	// Create this task's working directory at <workDir>/tasks/<taskID>.
	mainDir := ensureRunDir(m.workDir, taskID, 0)
	ctx = intercept.WithReviewWorkingDirectory(ctx, mainDir)
	system, boundary := deferredSystem(mainAgentSystem(goal, m.workDir, mainDir), def)
	opts := agentcore.Options{
		Provider:        m.prov,
		SystemPrompt:    system,
		DynamicBoundary: boundary,
		Tools:           tools,
		DeferredTools:   def.Deferred,
		UnlockSet:       def.Unlock,
		PermissionMode:  permission.ModeBypass,
		EnableWebFetch:  true, // Use the recording proxy and its CA to verify re-signed HTTPS certificates.
		WebFetchProxy:   m.proxyAddr,
		WebFetchCACert:  m.proxyCACert,
		// Optional web search: ddgs needs no key; brave-free needs BraveKey; tavily needs TavilyKey.
		// WebSearchProxy is a separate egress proxy (http/https/socks5); empty means direct.
		EnableWebSearch:       m.webSearch.Enabled,
		WebSearchBackend:      m.webSearch.Backend,
		BraveSearchAPIKey:     m.webSearch.BraveKey,
		TavilySearchAPIKey:    m.webSearch.TavilyKey,
		DeepSeekSearchBaseURL: m.webSearch.DeepSeekBaseURL,
		DeepSeekSearchAPIKey:  m.webSearch.DeepSeekAPIKey,
		DeepSeekSearchModel:   m.webSearch.DeepSeekModel,
		WebSearchProxy:        m.webSearch.Proxy,
		BashEnv:               proxyEnv(m.proxyAddr, m.proxyCACert), // Bash subprocesses use the proxy and trust its CA.
		WorkingDir:            mainDir,                              // This task's <workDir>/tasks/<taskID> directory.
		ToolOutputDir:         cmdOutDir(mainDir),
		MaxTurns:              m.maxTurns,                             // 0 = unlimited (configurable in agent management)
		Compaction:            compactionConfig(m.compactionWindow()), // long chats stay within the window
		Todos:                 actool.NewTodoStore(),                  // Session-only TodoWrite notes; discarded on exit.
		// On turn-budget exhaustion, the SDK wraps up with a brief progress summary.
		// The prompt and wrap-up turn limit are editable in settings (default: 10).
		Settlement:   wrapupSettlement("mainagent", nil),
		NonStreaming: m.nonStreaming(), // Use Provider.Complete for a non-streaming profile.
		MaxTokens:    m.maxTokens(),    // 0 = omit the cap and use the server default.
	}
	if m.tx != nil { // persist raw human↔AI conversation; one accumulating file per segment
		opts.Transcript = m.tx
		// Segment 0 keeps the legacy "exp%d-main" name so existing transcripts still
		// load; each new session (seg>=1) gets its own file for a clean context.
		opts.SessionID = fmt.Sprintf("exp%d-main", ts.ID())
		if mainSeg > 0 {
			opts.SessionID = fmt.Sprintf("exp%d-main-s%d", ts.ID(), mainSeg)
		}
	}
	// Experimental noa compaction keeps persistent archives under <workDir>/noa/<SessionID>.
	// Match the transcript's segment-aware session IDs for archive and resume.
	noaSession := fmt.Sprintf("exp%d-main", ts.ID())
	if mainSeg > 0 {
		noaSession = fmt.Sprintf("exp%d-main-s%d", ts.ID(), mainSeg)
	}
	enableNoa(&opts, m.noaEnabledFn, m.workDir, noaSession, noaWarn(noaSession))
	ctx = attachSideCapture(ctx, &opts)
	s := agentcore.NewSession(opts)
	defer s.Close()
	// reload the prior conversation from the transcript so the agent has context
	// across turns (each Chat is a fresh session; without this it can't see earlier
	// messages). First turn: no file yet → Resume loads nothing and proceeds.
	if m.tx != nil {
		_ = s.Resume(opts.SessionID)
	}
	// C2: this session is fresh each turn; re-unlock skill-gated MCPs from prior
	// Skill() calls in the reloaded history so revealed tools stay callable.
	seedUnlockFromHistory(s.Messages(), def.UnlockSkill)
	text, _, err := captureRunSession(ctx, s, message, func(r db.Activity) {
		if emit != nil {
			r.Worker = "mainagent"
			emit(r)
		}
	})
	return text, err
}

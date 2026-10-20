package agent

import (
	"bytes"
	"fmt"
	"text/template"
	"time"
)

// PromptOverride, if set, returns the stored system-prompt template for an agent
// key and whether one exists. The server wires it to the PG agent_prompts table.
// When nil or no override exists, agents use their built-in default prompt — so
// behavior is identical until a user edits a prompt in the UI.
var PromptOverride func(agentKey string) (string, bool)

// Prompt-variable structs — fields mirror each agent's catalog (docs §5a) so a
// user template referencing a catalog variable renders; referencing anything else
// fails template execution and falls back to the built-in default.
type PlannerVars struct{ Goal, Scope, AssetSummary, DataDir, Now string }
type WorkerVars struct{ ProxyAddr, WorkerName, DataDir, Now string }
type MainVars struct{ Goal, AssetSummary, FindingsSummary, DataDir, Now string }
type GoalsVars struct{ EngagementDescription, DataDir, Now string }

// nowStr is the server-local wall-clock string exposed as the universal {{.Now}}
// prompt variable. renderSystem runs on every agent turn/round, so this is fresh
// each run — a prompt can subtract it from a fixed start stamp to reason about
// elapsed time (e.g. a timed benchmark's "last N hours" window).
func nowStr() string { return time.Now().Format("2006-01-02 15:04:05 MST") }

// renderSystem returns the rendered system-prompt body (segment [A]) for agentKey.
// Precedence: the DB-stored template (if any) over the built-in default template
// (def). BOTH are Go templates now — the built-in default is seeded into the DB
// verbatim, so the two paths render identically until a user edits the prompt.
// Rendering always runs (def used to be pre-substituted plain text; it is now a
// {{.Var}} template like the DB one). On any render error we fall back to the
// default template, then to the raw default string — an agent never starts with a
// half-rendered prompt. Callers append the code-owned traffic and artifact-output
// tails AFTER this, so those cannot be edited away via the DB body.
func renderSystem(agentKey, def string, vars any) string {
	tmpl := def
	if PromptOverride != nil {
		if t, ok := PromptOverride(agentKey); ok && t != "" {
			tmpl = t
		}
	}
	if out, err := renderTmpl(tmpl, vars); err == nil {
		return out
	}
	// DB template broke (e.g. references an out-of-catalog var) → code default.
	if out, err := renderTmpl(def, vars); err == nil {
		return out
	}
	return def
}

// langDirective is the output-language tail: a code-owned segment
// appended AFTER the rendered body and the artifact/traffic tails on every
// user-facing agent role, so a DB-edited prompt body can never drop it — the same
// guarantee artifactSpec gives. It constrains the LANGUAGE of what the agent
// SHOWS to the user. Raw technical strings
// (commands, payloads, code, URLs, log/response excerpts) are explicitly kept
// verbatim so evidence and reproduction steps are not mangled by translation.
//
// Two anti-drift clauses were added after the end-to-end run (L1): live models
// leaked (1) the instruction language into the planner's situation summary and
// (2) the target app/evidence language into
// report_finding's structured fields. The directive names the planner situation
// summary as a user-facing field and forbids mirroring the instruction
// language or the target/material language in display fields.
//
// The display language is a parameter (code = "en"/"ko"/"zh"/"es"): the directive
// is written in English with a per-target "write in <language>" token. For "zh",
// Chinese output is the goal, so the Chinese-leak warning does not apply.
func langDirective(code string) string {
	phrase := langPhrase[code]
	bare := langBare[code]
	if phrase == "" || bare == "" {
		phrase, bare = langPhrase[defaultOutputLang], langBare[defaultOutputLang]
	}
	if code == "zh" {
		// 표시 언어 자체가 중국어이므로 "중국어를 사용자에게 내보내지 말라"는 절은 없다.
		// 대신 중국어 표시를 확정하고, 대상/자료 언어 미러링 금지와 원문 보존은 유지한다.
		return fmt.Sprintf("\n\n**Output language rule (localization; highest priority; cannot be overridden by the prompt body):** Write all user-visible natural language in **%s**. This includes record_fact summary/detail, report_finding title/description/conclusion/remediation, the planner's situation summaries, the final one-sentence summary, and chat replies. Preserve commands, payloads, code, file paths, URLs, parameter names, and original excerpts from logs, requests, and responses verbatim; do not translate or rewrite them. In particular, keep evidence commands and output unchanged so they can be reproduced. **Even if the target system, its pages, evidence, logs, or reference material use another language, write user-facing natural-language fields (title, description, conclusion, remediation, and summaries) in %s. Do not mirror the target or reference language in those fields; only the technical excerpts listed above remain verbatim.** Internal reasoning may use any language, but all user-visible prose must be in %s except verbatim technical excerpts.", phrase, bare, bare)
	}
	return fmt.Sprintf("\n\n**Output language rule (localization; highest priority; cannot be overridden by the prompt body):** Write all user-visible natural language in **%s**. This includes record_fact summary/detail, report_finding title/description/conclusion/remediation, the planner's situation summaries, the final one-sentence summary, and chat replies. Preserve commands, payloads, code, file paths, URLs, parameter names, and original excerpts from logs, requests, and responses verbatim; do not translate or rewrite them. In particular, keep evidence commands and output unchanged so they can be reproduced. **Do not output Chinese to the user when the selected display language is %s.** **Even if the target system, its pages, evidence, logs, or reference material use another language, write user-facing natural-language fields (title, description, conclusion, remediation, and summaries) in %s. Do not mirror the target or reference language in those fields; only the technical excerpts listed above remain verbatim.** Internal reasoning may use any language, but it is not user-visible prose: from the first word, replies to the user must be in %s. This also applies to clarification requests, missing-parameter notices, and explanations that work cannot continue. All user-visible prose must be in %s except verbatim technical excerpts.", phrase, bare, bare, bare, bare)
}

func renderTmpl(tmpl string, vars any) (string, error) {
	t, err := template.New("p").Option("missingkey=error").Parse(tmpl)
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	if err := t.Execute(&b, vars); err != nil {
		return "", err
	}
	return b.String(), nil
}

package agent

import (
	"encoding/json"
	"fmt"

	"github.com/Autumn-27/artex/db"
	actool "github.com/Autumn-27/norma/tool"
)

const findingIDGuidance = "\n\n**Finding ID convention:** finding_id identifies an independent finding record; finding_node_id identifies an exploration node. The id fields returned by list_findings, list_task_findings, node_detail, and get_task_node_detail remain exploration node IDs; read finding_id from the same response for the independent ID. get_finding_traffic and bind_finding_traffic use the independent finding_id. The legacy finding_id parameter of update_finding_report still takes finding_node_id. Do not use the number on the first line of report_finding for evidence tools, or guess another number after an ID error."

// The server supplies the persisted setting. A missing setting/host is off.
// Consulted at assembly and again on writes so an already-running session
// cannot keep binding after the user switches the feature off.
var FindingTrafficBindingEnabled func() bool

func findingTrafficBindingEnabled() bool {
	return FindingTrafficBindingEnabled != nil && FindingTrafficBindingEnabled()
}

// Applied after ToolResolve: user descriptions and prompts remain intact, while
// all actual reporters (including Planner and custom chat agents) see the same
// API contract. Disabled/unbound tools are never reintroduced here.
func findingWorkflowTools(agentKey string, tools []actool.CoreTool) ([]actool.CoreTool, string) {
	if !findingTrafficBindingEnabled() {
		out := make([]actool.CoreTool, 0, len(tools))
		for _, tool := range tools {
			if tool.Name() == "bind_finding_traffic" {
				continue
			}
			if agentKey == "reporter" && (tool.Name() == "traffic_search" || tool.Name() == "traffic_get" || tool.Name() == "traffic_blob") {
				continue
			}
			switch tool.Name() {
			case "report_finding", "add_hint", "add_task_hint":
				// Work on a copy: toggling back on must restore the original schema.
				raw, _ := json.Marshal(tool.InputSchema())
				var schema map[string]any
				if json.Unmarshal(raw, &schema) == nil {
					stripTrafficParameters(schema)
					tool = DecorateTool(tool, tool.Description(), schema)
				}
			}
			out = append(out, tool)
		}
		return out, ""
	}
	out := append([]actool.CoreTool(nil), tools...)
	has := map[string]bool{}
	for i, tool := range out {
		has[tool.Name()] = true
		note := ""
		switch tool.Name() {
		case "report_finding":
			note = "\nBy default, the reporter agent verifies and binds traffic before writing the report. Keep validation commands, key output, real traffic IDs already obtained, and their roles in evidence so the reporter can verify them against the execution trace. Do not retrieve extra packets solely for binding. Explicit immediate binding remains supported: traffic_refs or evidence_hint_id can submit verified references; the latter reads structured references from the specified hint in this task. If either is invalid, the entire report fails. TCP findings or findings without captured packets do not need these optional parameters. The returned finding_id and finding_node_id identify the independent record and exploration node, respectively."
		case "add_hint", "add_task_hint":
			note = "\nWhen handing off a confirmed finding, preserve verified traffic IDs, roles, notes, and order in the corresponding hint's traffic_refs (at the top level for one hint, or in the relevant hints element for a batch). Explain the specific finding supported by that traffic in text. Do not pass only prose while dropping existing traffic references. Unverified candidates must not be passed as evidence."
		case "get_finding_traffic", "bind_finding_traffic", "list_findings", "list_task_findings", "node_detail", "get_task_node_detail", "update_finding_report":
			note = findingIDGuidance
		}
		if note != "" {
			out[i] = DecorateTool(tool, tool.Description()+note, tool.InputSchema())
		}
	}
	guidance := ""
	if has["report_finding"] || has["add_task_hint"] || has["add_hint"] {
		guidance = "\n\n**Traffic evidence handoff (optional):** By default, the reporter agent binds traffic after a finding is recorded and before writing its report. Keep validation commands, key output, real traffic IDs already obtained, and their roles in evidence; include intent_id in task context so the reporter can trace them. Do not retrieve extra packets solely for binding. When Auto or Planner reports on behalf of an executor, preserve the executor's references. add_hint and add_task_hint can hand off traffic_refs; report_finding still supports explicit immediate binding via traffic_refs or evidence_hint_id. Record TCP findings and findings without captured packets normally. Never guess IDs or repeat probing solely to obtain packets."
		if has["add_task_hint"] && !has["add_hint"] {
			guidance += "\nIn a platform chat without task context, do not call report_finding directly. Use add_task_hint to hand off to the relevant existing task, let its agent record the finding, and verify it with list_task_findings."
		}
		if has["prove_goal"] || has["goal_met"] {
			guidance += "\nBefore marking a goal complete, report or hand off evidence already collected in this run. Do not end the task or cancel Workers merely because a textual finding was recorded while evidence handoff is still pending. If there are no captured packets, do not wait or force packet capture."
		}
	}
	if has["update_finding_report"] && has["bind_finding_traffic"] && has["get_finding_traffic"] {
		guidance += "\n\n**Automatic traffic binding before the report (enabled):** Verify and bind traffic for the finding that triggered this invocation, then write the report. First obtain explicit finding_id and finding_node_id values from the report_finding JSON response, get_task_node_detail, or list_task_findings. Read the finding details, the corresponding intent's execution trace, and existing evidence list; prefer real IDs handed off by the agent that submitted the finding. If this validation used HTTP and traffic tools are available, use traffic_search to find candidates, then inspect each request and response with traffic_get to verify that it supports the finding. Domain and time are filters, not proof of association. Bind confirmed evidence in reproduction order with bind_finding_traffic(finding_id, traffic_refs), selecting baseline, proof, verification, or supporting and explaining each role. Operate only on this finding; do not create duplicate findings or probe the target again. After successful binding, call get_finding_traffic again to obtain the latest version and read the needed content. Pass the version actually read as evidence_version to update_finding_report (whose finding_id parameter still takes finding_node_id). Do not append existing bindings again. For TCP findings, uncaptured traffic, unavailable tools, or no exact match, skip automatic binding, write the report from textual or command evidence, and explain why. Never guess traffic IDs. Do not claim a failed binding succeeded; retain existing evidence and explain the missing binding in the report."
	}
	if guidance != "" || has["get_finding_traffic"] || has["update_finding_report"] {
		guidance += findingIDGuidance
	}
	return out, guidance
}

func stripTrafficParameters(schema map[string]any) {
	props, _ := schema["properties"].(map[string]any)
	delete(props, "traffic_refs")
	delete(props, "evidence_hint_id")
	if required, ok := schema["required"].([]any); ok {
		kept := required[:0]
		for _, key := range required {
			if key != "traffic_refs" && key != "evidence_hint_id" {
				kept = append(kept, key)
			}
		}
		schema["required"] = kept
	}
	if hints, ok := props["hints"].(map[string]any); ok {
		if items, ok := hints["items"].(map[string]any); ok {
			stripTrafficParameters(items)
		}
	}
}

// HintTrafficSchema is shared by the task-local and cross-task hint tools.
func HintTrafficSchema() map[string]any {
	return map[string]any{"type": "array", "description": "Optional verified traffic references for the specific finding in this hint, in order. After handoff, report_finding can pass evidence_hint_id to carry these references.", "items": obj(map[string]any{"traffic_id": str("Real traffic ID"), "role": str("baseline / proof / verification / supporting"), "note": str("What this traffic supports")}, "traffic_id")}
}

func (t *ToolSet) findingRefsFromHint(hintID int64, explicit []db.TrafficRef) ([]db.TrafficRef, error) {
	if hintID <= 0 {
		return db.NormalizeTrafficRefs(explicit)
	}
	n, err := t.ts.GetNode(hintID) // local store only: inherited hints cannot supply evidence
	if err != nil {
		return nil, err
	}
	if n == nil || n.Kind != db.KindHint {
		return nil, fmt.Errorf("evidence_hint_id=%d must identify a hint node in this task (inherited hints cannot be bound directly)", hintID)
	}
	var payload struct {
		Refs []db.TrafficRef `json:"traffic_refs"`
	}
	if err := json.Unmarshal(n.Payload, &payload); err != nil {
		return nil, err
	}
	return db.NormalizeTrafficRefs(append(append([]db.TrafficRef{}, explicit...), payload.Refs...))
}

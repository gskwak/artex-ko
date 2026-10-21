import { createServer } from "node:http";

// Disposable OpenAI Chat Completions fixture. It never calls another network
// service, and the companion smoke test runs it on a Docker --internal network.
const port = Number(process.env.MOCK_LLM_PORT || 8789);
const targetURL = process.env.MOCK_TARGET_URL || "http://artex-lab-target:8080/model-fixture";
const calls = [];
let redFetchIssued = false;
let redGoalsIssued = false;
let redFactIssued = false;
let redFindingIssued = false;
let redProveIssued = false;
let redGoalID = 0;
let redFactID = 0;

function textOf(value) {
  if (typeof value === "string") return value;
  if (Array.isArray(value)) return value.map(textOf).join("\n");
  if (value && typeof value === "object") return textOf(value.text || value.content || "");
  return "";
}

function findCaseEvidence(messages) {
  for (const message of [...messages].reverse()) {
    const content = textOf(message.content);
    const start = content.indexOf("{");
    if (start < 0) continue;
    try {
      const data = JSON.parse(content.slice(start));
      if (data && Array.isArray(data.events) && data.events.length > 0) return data;
    } catch {
      // Normal chat text is not JSON.
    }
  }
  return null;
}

function eventStatus(event) {
  const direct = Number(event?.raw?.status);
  if (Number.isInteger(direct) && direct >= 100 && direct <= 599) return direct;
  const narrative = `${event?.message || ""}\n${event?.raw_preview || ""}`;
  return Number(narrative.match(/returned\s+(\d{3})/)?.[1] || 0);
}

function completionFor(request) {
  const messages = Array.isArray(request.messages) ? request.messages : [];
  const system = messages.filter((message) => message.role === "system").map((message) => textOf(message.content)).join("\n");
  const evidence = findCaseEvidence(messages);
  const tools = Array.isArray(request.tools) ? request.tools.map((tool) => tool.function?.name || tool.name) : [];
  const conversation = messages.map((message) => textOf(message.content)).join("\n");
  redGoalID ||= Number(conversation.match(/goal added:\s*(\d+)/)?.[1] || 0);
  redFactID ||= Number(conversation.match(/fact recorded:\s*(\d+)/)?.[1] || 0);
  const overviewJSON = conversation.match(/Current round overview[^\n]*\n(\{[^\n]+\})/)?.[1];
  if (overviewJSON) {
    const overview = JSON.parse(overviewJSON);
    redGoalID ||= Number(overview.goals?.find((goal) => goal.state === "open")?.id || 0);
    redFactID ||= Number(overview.recent_facts?.[0]?.id || 0);
  }
  if (system.includes("ARTEX Blue Team's read-only incident analyst")) {
    if (!evidence) throw new Error("blue analysis request had no case evidence");
    const eventID = Number(evidence.events[0].id);
    const status = eventStatus(evidence.events[0]);
    if (!status) throw new Error(`fixture event #${eventID} has no observed HTTP status`);
    const citation = { kind: "event", id: eventID };
    const alertID = Number(evidence.alerts?.[0]?.id || 0);
    const alertCitation = alertID ? { kind: "alert", id: alertID } : null;
    const assessment = {
      summary: `Fixture analysis: event #${eventID} recorded HTTP ${status}.`,
      verdict: "inconclusive",
      confidence: "low",
      severity: "unknown",
      facts: [{ text: `Event #${eventID} records an HTTP ${status} response from the private lab.`, evidence: [citation] }],
      hypotheses: [],
      unknowns: ["The fixture cannot establish activity outside the supplied records."],
      detection_ideas: [],
      response_recommendations: [],
    };
    if (evidence.events_truncated || evidence.alerts_truncated) {
      assessment.unknowns.push("The recent evidence window was truncated; this assessment is limited to the selected and supplied records.");
    }
    if (alertCitation) assessment.facts.push({ text: `The supplied record contains alert #${alertID}.`, evidence: [alertCitation] });
    if (evidence.task) {
      assessment.task_outcome = {
        status: "answered",
        summary: `The selected private-lab event #${eventID} records HTTP ${status}.`,
        evidence: alertCitation ? [citation, alertCitation] : [citation],
      };
    }
    return {
      kind: evidence.task ? "blue-task" : "blue-case",
      text: JSON.stringify(assessment), eventID,
      alertID,
      eventIDs: evidence.events.map((event) => Number(event.id)),
      alertIDs: evidence.alerts?.map((alert) => Number(alert.id)) || [],
      selectedEventIDs: evidence.selected_event_ids || [],
      selectedAlertIDs: evidence.selected_alert_ids || [],
    };
  }
  if (system.includes("ARTEX Blue Team's defensive security assistant")) {
    const eventID = Number(evidence?.events?.[0]?.id || 0);
    const alertID = Number(evidence?.alerts?.[0]?.id || 0);
    const status = eventStatus(evidence?.events?.[0]);
    const limit = evidence?.events_truncated || evidence?.alerts_truncated
      ? " Recent evidence was truncated; this summary covers the selected records only." : "";
    return {
      kind: "blue-chat",
      text: eventID
        ? `Fixture summary: attached event #${eventID} records HTTP ${status}${alertID ? ` and alert #${alertID} matched it` : ""}. Verify other records before drawing a conclusion.${limit}`
        : "Fixture summary: no case evidence was attached, so I cannot make a case-specific claim.",
      eventID,
      alertID,
      eventIDs: evidence?.events?.map((event) => Number(event.id)) || [],
      alertIDs: evidence?.alerts?.map((alert) => Number(alert.id)) || [],
      selectedEventIDs: evidence?.selected_event_ids || [],
      selectedAlertIDs: evidence?.selected_alert_ids || [],
    };
  }
  if (tools.includes("set_goals") && !redGoalsIssued) {
    redGoalsIssued = true;
    return toolReply("red-goal", "set_goals", { text: `Verify the HTTP status of ${targetURL} using direct evidence.` });
  }
  if (tools.includes("WebFetch") && tools.includes("record_fact")) {
    if (!redFetchIssued) {
      redFetchIssued = true;
      return toolReply("red-fetch", "WebFetch", { url: targetURL, extract: false });
    }
    // A finding is reported only after the real WebFetch tool returned the
    // fixture's distinctive body, never merely because the tool was requested.
    if (!redFindingIssued && tools.includes("report_finding") &&
      conversation.includes("unauthenticated_debug_configuration") &&
      conversation.includes("DEMO_ONLY_NOT_A_REAL_SECRET")) {
      redFindingIssued = true;
      const intentID = Number(system.match(/Intent ID:\s*(\d+)/)?.[1] || conversation.match(/Intent ID:\s*(\d+)/)?.[1] || 0);
      return toolReply("red-finding", "report_finding", {
        intent_id: intentID,
        vulnclass: "information_disclosure",
        name: "Unauthenticated demo debug configuration disclosure",
        severity: "low",
        summary: `The private lab's ${targetURL} exposes a training-only debug configuration without authentication.`,
        evidence: `WebFetch ${targetURL} returned HTTP 200 with issue=unauthenticated_debug_configuration and credential=DEMO_ONLY_NOT_A_REAL_SECRET. This is fixture data only.`,
      });
    }
    if (!redFactIssued && messages.some((message) => message.role === "tool" &&
      textOf(message.content).includes("unauthenticated_debug_configuration"))) {
      redFactIssued = true;
      const intentID = Number(system.match(/Intent ID:\s*(\d+)/)?.[1] || conversation.match(/Intent ID:\s*(\d+)/)?.[1] || 0);
      return toolReply("red-fact", "record_fact", {
        intent_id: intentID,
        summary: `The lab endpoint returned HTTP 200 at ${targetURL}.`,
        detail: "The worker fetched the private fixture endpoint and observed its HTTP response.",
        evidence: `WebFetch ${targetURL}: HTTP 200`,
        confidence: "observed",
      });
    }
  }
  if (tools.includes("prove_goal") && redGoalID > 0 && redFactID > 0 && !redProveIssued) {
    redProveIssued = true;
    return toolReply("red-prove", "prove_goal", {
      goal_id: redGoalID,
      evidence_id: redFactID,
      reason: `Recorded fact #${redFactID} documents the private fixture's HTTP 200 response.`,
    });
  }
  return { kind: "red", text: "Fixture model acknowledged the bounded lab task. No external claim is made.", eventID: 0 };
}

function toolReply(kind, name, args) {
  return {
    kind, text: "", eventID: 0,
    toolCall: {
      id: `call_fixture_${name}`,
      type: "function",
      function: { name, arguments: JSON.stringify(args) },
    },
  };
}

function sendJSON(res, status, body) {
  res.writeHead(status, { "content-type": "application/json; charset=utf-8", "cache-control": "no-store" });
  res.end(JSON.stringify(body));
}

createServer(async (req, res) => {
  if (req.method === "GET" && req.url === "/health") return sendJSON(res, 200, { ok: true });
  if (req.method === "GET" && req.url === "/calls") return sendJSON(res, 200, { calls });
  if (req.method !== "POST" || req.url !== "/v1/chat/completions") return sendJSON(res, 404, { error: "not found" });

  let raw = "";
  try {
    for await (const chunk of req) {
      raw += chunk;
      if (raw.length > 2_000_000) throw new Error("request too large");
    }
    const request = JSON.parse(raw);
    const answer = completionFor(request);
    const call = {
      kind: answer.kind,
      event_id: answer.eventID,
      alert_id: answer.alertID,
      event_ids: answer.eventIDs,
      alert_ids: answer.alertIDs,
      selected_event_ids: answer.selectedEventIDs,
      selected_alert_ids: answer.selectedAlertIDs,
      model: request.model,
      stream: request.stream === true,
      tools: Array.isArray(request.tools) ? request.tools.map((tool) => tool.function?.name || tool.name).filter(Boolean) : [],
      target_url: answer.kind === "red-fetch" ? targetURL : undefined,
    };
    calls.push(call);
    if (calls.length > 100) calls.shift();
    if (request.stream === true) {
      res.writeHead(200, { "content-type": "text/event-stream", "cache-control": "no-store" });
      res.write(`data: ${JSON.stringify({ id: "chatcmpl-fixture", object: "chat.completion.chunk", created: 0, model: request.model || "fixture", choices: [{ index: 0, delta: { role: "assistant", content: answer.text }, finish_reason: null }] })}\n\n`);
      res.write(`data: ${JSON.stringify({ id: "chatcmpl-fixture", object: "chat.completion.chunk", created: 0, model: request.model || "fixture", choices: [{ index: 0, delta: {}, finish_reason: "stop" }] })}\n\n`);
      res.end("data: [DONE]\n\n");
      return;
    }
    sendJSON(res, 200, {
      id: "chatcmpl-fixture",
      object: "chat.completion",
      created: 0,
      model: request.model || "fixture",
      choices: [{
        index: 0,
        message: { role: "assistant", content: answer.toolCall ? null : answer.text, ...(answer.toolCall ? { tool_calls: [answer.toolCall] } : {}) },
        finish_reason: answer.toolCall ? "tool_calls" : "stop",
      }],
      usage: { prompt_tokens: 20, completion_tokens: 20, total_tokens: 40 },
    });
  } catch (error) {
    sendJSON(res, 400, { error: { message: error.message } });
  }
}).listen(port, "0.0.0.0", () => process.stdout.write(`mock LLM listening on ${port}\n`));

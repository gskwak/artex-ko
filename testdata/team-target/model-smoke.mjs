import { execFileSync, spawnSync } from "node:child_process";
import { randomBytes } from "node:crypto";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

// Runs the real Red and Blue orchestration against a disposable, local-only
// OpenAI-compatible fixture. No external model account or production DB is used.
// From the repository root, after starting the private team target:
//   $env:ARTEX_E2E_IMAGE='artex-ko:ssh'; node testdata/team-target/model-smoke.mjs
// ARTEX_E2E_SELECTED=0 is only for older images without explicit Blue evidence
// selection and defensive asset routes; the default exercises those routes.
const suffix = randomBytes(4).toString("hex");
const network = `artex-model-lab-${suffix}`;
const database = `artex-model-pg-${suffix}`;
const model = `artex-model-llm-${suffix}`;
const app = `artex-model-app-${suffix}`;
const target = "artex-lab-target";
const image = process.env.ARTEX_E2E_IMAGE || "artex-ko:ssh";
const selectedEvidence = process.env.ARTEX_E2E_SELECTED !== "0";
const password = randomBytes(20).toString("hex");
const databasePassword = randomBytes(20).toString("hex");
const targetPath = `/debug/config?probe=model-${suffix}`;
const targetURL = `http://${target}:8080${targetPath}`;
const mockFile = resolve("testdata/team-target/mock-llm.mjs");
const targetLog = resolve("test-results/team-target/access.jsonl");
const checks = [];
let networkCreated = false;
let targetConnected = false;
let databaseCreated = false;
let modelCreated = false;
let appCreated = false;

function docker(args, env = process.env) {
  return execFileSync("docker", args, { encoding: "utf8", timeout: 120_000, env }).trim();
}

function check(condition, label) {
  if (!condition) throw new Error(label);
  checks.push(label);
}

async function waitFor(fn, label, attempts = 40) {
  for (let i = 0; i < attempts; i++) {
    try {
      const result = await fn();
      if (result) return result;
    } catch {
      // The disposable service may still be starting.
    }
    await new Promise((done) => setTimeout(done, 1000));
  }
  throw new Error(`Timed out: ${label}`);
}

async function api(base, token, method, path, body, expected, label, quiet = false) {
  const client = `const fs=require('node:fs');const input=JSON.parse(fs.readFileSync(0,'utf8'));(async()=>{const response=await fetch('http://127.0.0.1:8787'+input.path,{method:input.method,headers:{...(input.token?{Authorization:'Bearer '+input.token}:{}),...(input.body===undefined?{}:{'content-type':'application/json'})},body:input.body===undefined?undefined:JSON.stringify(input.body),signal:AbortSignal.timeout(30000)});const raw=await response.text();process.stdout.write(JSON.stringify({status:response.status,value:JSON.parse(raw)}));})().catch(error=>{process.stderr.write(error.message);process.exitCode=1});`;
  // docker exec is the only API entry point. The app has no published port and
  // only an --internal network, so Red tools cannot reach external services.
  const result = spawnSync("docker", ["exec", "-i", app, "node", "-e", client], {
    input: JSON.stringify({ token, method, path, body }), encoding: "utf8", timeout: 45_000,
  });
  if (result.status !== 0) throw new Error(`${label}: docker exec failed: ${result.stderr}`);
  const { status, value } = JSON.parse(result.stdout);
  if (status !== expected) throw new Error(`${label}: HTTP ${status}, expected ${expected}: ${JSON.stringify(value)}`);
  if (!quiet) checks.push(`${label}: HTTP ${expected}`);
  return value;
}

function cleanup() {
  for (const name of [appCreated && app, modelCreated && model, databaseCreated && database]) {
    if (name) spawnSync("docker", ["rm", "-fv", name], { stdio: "ignore", timeout: 120_000 });
  }
  if (targetConnected) spawnSync("docker", ["network", "disconnect", network, target], { stdio: "ignore", timeout: 120_000 });
  if (networkCreated) spawnSync("docker", ["network", "rm", network], { stdio: "ignore", timeout: 120_000 });
}

try {
  check(docker(["inspect", target, "--format", "{{.State.Status}}"]) === "running", "Private test target running");
  const imageSnapshot = docker(["image", "inspect", image, "--format", "{{.Id}}"]).trim();
  docker(["network", "create", "--internal", network]);
  networkCreated = true;
  docker(["network", "connect", network, target]);
  targetConnected = true;

  docker(["run", "-d", "--name", database, "--network", network, "-e", "POSTGRES_USER=artex", "-e", "POSTGRES_PASSWORD", "-e", "POSTGRES_DB=artex", "postgres:16-alpine"], { ...process.env, POSTGRES_PASSWORD: databasePassword });
  databaseCreated = true;
  await waitFor(() => spawnSync("docker", ["exec", database, "pg_isready", "-U", "artex", "-d", "artex"], { stdio: "ignore", timeout: 30_000 }).status === 0, "PostgreSQL ready");
  check(true, "Disposable PostgreSQL ready");

  docker(["run", "-d", "--name", model, "--network", network, "--entrypoint", "node", "-v", `${mockFile}:/mock-llm.mjs:ro`, "-e", "MOCK_TARGET_URL", image, "/mock-llm.mjs"], { ...process.env, MOCK_TARGET_URL: targetURL });
  modelCreated = true;
  await waitFor(() => spawnSync("docker", ["exec", model, "node", "-e", "fetch('http://127.0.0.1:8789/health').then(r=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))"], { stdio: "ignore", timeout: 30_000 }).status === 0, "mock model ready");
  check(true, "Local mock model ready");

  const appEnv = {
    ...process.env,
    ARTEX_PG_DSN: `postgres://artex:${databasePassword}@${database}:5432/artex?sslmode=disable`,
    ARTEX_ADMIN_PASSWORD: password,
    ARTEX_SETUP_TOKEN: randomBytes(20).toString("hex"),
    ARTEX_LLM_PROVIDER: "openai",
    ARTEX_LLM_MODEL: "fixture",
    ARTEX_LLM_BASE_URL: `http://${model}:8789/v1`,
    ARTEX_LLM_STREAM: "false",
    OPENAI_API_KEY: "fixture-only",
    ANTHROPIC_API_KEY: "",
  };
  docker(["run", "-d", "--name", app, "--network", network, "--entrypoint", "/app/artex", ...["ARTEX_PG_DSN", "ARTEX_ADMIN_PASSWORD", "ARTEX_SETUP_TOKEN", "ARTEX_LLM_PROVIDER", "ARTEX_LLM_MODEL", "ARTEX_LLM_BASE_URL", "ARTEX_LLM_STREAM", "OPENAI_API_KEY", "ANTHROPIC_API_KEY"].flatMap((key) => ["-e", key]), image, "-addr", ":8787", "-proxy", ":8788"], appEnv);
  appCreated = true;
  const base = "";
  await waitFor(async () => {
    const result = spawnSync("docker", ["exec", app, "node", "-e", "fetch('http://127.0.0.1:8787/api/health').then(r=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))"], { stdio: "ignore", timeout: 30_000 });
    return result.status === 0;
  }, "ARTEX ready");
  check(true, "Disposable ARTEX app healthy");

  const login = await api(base, "", "POST", "/api/auth/login", { username: "ARTEX", password }, 200, "Disposable admin login");
  const token = login.token;
  check(typeof token === "string" && token.length > 20, "Disposable JWT issued");
  const configured = await api(base, token, "GET", "/api/llm", undefined, 200, "Local model configuration read");
  check(configured.configured && configured.provider === "openai" && configured.model === "fixture", "Local model active");

  const caseItem = await api(base, token, "POST", "/api/blue/cases", { title: "Fixture HTTP investigation", description: "Review the recorded lab HTTP request only", severity: "low" }, 201, "Blue case created");
  let blueAssetID = 0;
  if (selectedEvidence) {
    const blueAsset = await api(base, token, "POST", "/api/blue/assets", {
      kind: "service", identifier: `${target}:8080`, label: "Private lab target",
    }, 201, "Defensive lab asset created");
    blueAssetID = blueAsset.id;
    check(Number.isInteger(blueAssetID) && blueAssetID > 0, "Blue asset ID assigned");
    await api(base, token, "POST", `/api/blue/cases/${caseItem.id}/assets`,
      { asset_id: blueAssetID }, 201, "Blue asset linked to investigation case");
    const caseAssets = await api(base, token, "GET", `/api/blue/cases/${caseItem.id}/assets`,
      undefined, 200, "Investigation case assets listed");
    check(caseAssets.some((asset) => asset.id === blueAssetID), "Case retains defensive asset link");
    await api(base, token, "POST", "/api/blue/rules", {
      name: `Fixture HTTP event ${suffix}`, field: "event_type", operator: "equals",
      value: "http.request", severity: "low", enabled: true,
    }, 201, "Blue rule created for alert selection");
  }
  const event = await api(base, token, "POST", "/api/blue/events/import", { events: [{ source: `model-lab-${suffix}`, external_id: "request-1", event_type: "http.request", severity: "low", case_id: caseItem.id, asset: `${target}:8080`, occurred_at: new Date().toISOString(), message: `GET ${targetPath} returned 200`, raw: { method: "GET", url: targetPath, status: 200, proto: "TCP" } }] }, 201, "Blue evidence imported");
  const eventID = event.events?.[0]?.id;
  check(Number.isInteger(eventID), "Blue event ID assigned");
  let alertID = 0;
  if (selectedEvidence) {
    const linkedEvent = await api(base, token, "PATCH", `/api/blue/events/${eventID}/asset`,
      { asset_id: blueAssetID }, 200, "Original event linked to defensive asset");
    check(linkedEvent.asset_id === blueAssetID, "Event retains defensive asset ID");
    const alerts = await api(base, token, "GET", `/api/blue/alerts?case_id=${caseItem.id}`, undefined, 200, "Blue alert listed");
    alertID = alerts.find((item) => item.event_id === eventID)?.id || 0;
    check(Number.isInteger(alertID) && alertID > 0, "Blue alert ID assigned to original event");
  }
  if (selectedEvidence) {
    // Push the first event beyond both default evidence windows. The explicit
    // selection must restore it to every real model request.
    const later = Array.from({ length: 45 }, (_, index) => ({
      source: `model-lab-${suffix}`, external_id: `later-${index + 1}`,
      event_type: "http.request", severity: "info", case_id: caseItem.id,
      asset: `${target}:8080`, occurred_at: new Date(Date.now() + (index + 1) * 1000).toISOString(),
      message: `Unrelated lab observation ${index + 1}`, raw: { sequence: index + 1 },
    }));
    const laterImport = await api(base, token, "POST", "/api/blue/events/import", { events: later }, 201, "Later Blue events imported");
    check(laterImport.events?.length === later.length && laterImport.alerts_created === later.length,
      "Original event and alert are older than default evidence windows");
  }
  const selection = selectedEvidence ? { event_ids: [eventID], alert_ids: [alertID] } : {};

  const caseRun = await api(base, token, "POST", `/api/blue/cases/${caseItem.id}/analyze`, selection, 202, "Blue case analysis started");
  const completedCaseRun = await waitFor(async () => {
    const result = await api(base, token, "GET", `/api/blue/analyses/${caseRun.run.id}`, undefined, 200, "Blue case analysis polled", true);
    if (result.run.status === "failed") throw new Error(`Blue case analysis failed: ${result.run.error}`);
    return result.run.status === "completed" ? result.run : null;
  }, "Blue case analysis completed");
  check(Number.isInteger(completedCaseRun.observation_id), "Blue case analysis stored a cited observation");
  const observations = await api(base, token, "GET", `/api/blue/cases/${caseItem.id}/observations`,
    undefined, 200, "Blue case observations read");
  check(observations.some((item) => item.id === completedCaseRun.observation_id &&
    item.summary?.includes("HTTP 200") && item.evidence_event_ids?.includes(eventID)),
  "Stored Blue case observation reports the selected HTTP status");

  const task = await api(base, token, "POST", "/api/blue/tasks", { title: "Summarize the recorded HTTP status", description: "Use the attached event to report the HTTP status code and its evidence ID.", case_id: caseItem.id, priority: "medium" }, 201, "Blue task created");
  const taskRun = await api(base, token, "POST", `/api/blue/tasks/${task.id}/run`, selection, 202, "Blue task analysis started");
  await waitFor(async () => {
    const result = await api(base, token, "GET", `/api/blue/analyses/${taskRun.run.id}`, undefined, 200, "Blue task analysis polled", true);
    if (result.run.status === "failed") throw new Error(`Blue task analysis failed: ${result.run.error}`);
    return result.run.status === "completed" ? result.run : null;
  }, "Blue task analysis completed");
  const completedTask = await api(base, token, "GET", `/api/blue/tasks/${task.id}`, undefined, 200, "Blue task result read");
  check(completedTask.status === "completed" && completedTask.analysis_run_id === taskRun.run.id &&
    completedTask.result?.includes("HTTP 200"), "Blue task answered the observed HTTP status");

  const conversation = await api(base, token, "POST", "/api/blue/conversations", { title: "Fixture grounded chat" }, 200, "Blue conversation created");
  await api(base, token, "POST", `/api/blue/conversations/${conversation.id}/messages`, { message: "Summarize only the attached evidence.", case_id: caseItem.id, ...selection }, 202, "Grounded Blue message accepted");
  const chat = await waitFor(async () => {
    const page = await api(base, token, "GET", `/api/blue/conversations/${conversation.id}/messages`, undefined, 200, "Blue chat polled", true);
    return page.items?.some((item) => item.kind === "text" && !item.is_error &&
      item.summary?.includes(`event #${eventID}`) && item.summary?.includes("HTTP 200")) ? page : null;
  }, "Grounded Blue response stored");
  check(chat.items.some((item) => item.kind === "text" && item.summary?.includes(`event #${eventID}`) &&
    item.summary?.includes("HTTP 200")), "Blue chat cited the event and observed status");

  const capture = await api(base, token, "PUT", "/api/settings", { traffic_capture: true }, 200, "Red traffic capture enabled");
  check(capture.traffic_capture === true, "Red recording proxy active");
  const redTask = await api(base, token, "POST", "/api/tasks", { description: `Inspect only ${targetURL}`, goal: `Fetch ${targetURL} and report its HTTP status; do not alter the target.`, timeout_seconds: 60, seed_first_intent: true }, 201, "Red bounded task created");
  check(redTask.id !== undefined, "Red task ID assigned");
  const calls = await waitFor(async () => {
    const records = JSON.parse(docker(["exec", model, "node", "-e", "fetch('http://127.0.0.1:8789/calls').then(r=>r.text()).then(console.log)"]));
    return records.calls.some((item) => item.kind === "red-fetch") ? records.calls : null;
  }, "Red worker model tool request", 35);
  check(calls.some((item) => item.kind === "blue-case" && item.event_id === eventID), "Case analysis model received actual evidence ID");
  check(calls.some((item) => item.kind === "blue-task" && item.event_id === eventID), "Task model received actual evidence ID");
  check(calls.some((item) => item.kind === "blue-chat" && item.event_id === eventID), "Chat model received attached case evidence");
  if (selectedEvidence) {
    for (const kind of ["blue-case", "blue-task", "blue-chat"]) {
      check(calls.some((item) => item.kind === kind && item.event_ids?.includes(eventID) &&
        item.selected_event_ids?.includes(eventID) && item.alert_ids?.includes(alertID) &&
        item.selected_alert_ids?.includes(alertID)), `${kind} model received explicitly selected older event and alert`);
    }
  }
  check(calls.some((item) => item.kind === "red-fetch"), "Red worker requested bounded WebFetch");

  const targetSeen = await waitFor(() => readFileSync(targetLog, "utf8").includes(targetPath), "Red request reached lab target", 20);
  check(Boolean(targetSeen), "Red WebFetch reached private lab target");
  const targetRecords = readFileSync(targetLog, "utf8").split("\n")
    .filter((line) => line.includes(targetPath)).map((line) => JSON.parse(line));
  check(targetRecords.some((record) => record.method === "GET" &&
    record.url === targetPath && record.status === 200), "Private lab recorded the Red HTTP 200 response");
  const captured = await waitFor(async () => {
    const traffic = await api(base, token, "GET", `/api/traffic?q=${encodeURIComponent(targetPath)}`, undefined, 200, "Red captured traffic polled", true);
    return JSON.stringify(traffic).includes(targetPath) ? traffic : null;
  }, "Red traffic capture includes lab request", 20);
  check(Boolean(captured), "Red proxy recorded private lab request");
  const finding = await waitFor(async () => {
    const items = await api(base, token, "GET", "/api/exploration/findings", undefined, 200, "Red findings polled", true);
    return items.find((item) => item.summary?.includes(targetPath)) || null;
  }, "Red model recorded verified fixture finding", 40);
  check(finding.status === "pending" && Number(finding.finding_id) > 0, "Red report_finding created triage-ready finding");
  check(finding.evidence?.includes("unauthenticated_debug_configuration") &&
    finding.evidence?.includes(targetPath), "Red finding retains observed fixture evidence");
  const confirmed = await api(base, token, "PATCH", `/api/exploration/findings/${finding.finding_id}`,
    { status: "confirmed" }, 200, "Red finding confirmed after triage");
  check(confirmed.status === "confirmed", "Red finding triage status stored");
  const linkedCase = await api(base, token, "POST", "/api/blue/cases/from-finding",
    { finding_id: Number(finding.finding_id) }, 201, "Blue case created from Red finding");
  check(linkedCase.red_finding_id === Number(finding.finding_id), "Blue case links confirmed Red finding");

  const completedRedTask = await waitFor(async () => {
    const item = await api(base, token, "GET", `/api/tasks/${redTask.id}`, undefined, 200, "Red task polled", true);
    if (item.status === "done") return item;
    if (["failed", "error"].includes(item.status)) throw new Error(`Red task status ${item.status}`);
    return null;
  }, "Red goal proof completed task", 80);
  check(completedRedTask.status === "done", "Red planner proved goal and completed task");
  const goals = await api(base, token, "GET", `/api/tasks/${redTask.id}/goals`, undefined, 200, "Red task goals read");
  check(JSON.stringify(goals).includes('"state":"met"'), "Red goal marked met with recorded fact");
  const graph = await api(base, token, "GET", `/api/exploration/graph?task=${redTask.id}`,
    undefined, 200, "Red evidence graph read");
  check(graph.edges?.some((edge) => edge.rel === "proves" &&
    graph.nodes?.some((node) => node.id === edge.src && node.type === "fact") &&
    graph.nodes?.some((node) => node.id === edge.dst && node.type === "goal")),
  "Recorded Red fact proves completed goal");
  const finalCalls = JSON.parse(docker(["exec", model, "node", "-e", "fetch('http://127.0.0.1:8789/calls').then(r=>r.text()).then(console.log)"])).calls;
  check(finalCalls.some((item) => item.kind === "red-prove"), "Red planner invoked prove_goal");
  process.stdout.write(JSON.stringify({ status: "passed", image: imageSnapshot, checks,
    mockCalls: finalCalls.map(({ kind, event_id, alert_id, selected_event_ids, selected_alert_ids }) =>
      ({ kind, event_id, alert_id, selected_event_ids, selected_alert_ids })), targetPath }, null, 2) + "\n");
} catch (error) {
  let mockCalls = [];
  if (modelCreated) {
    try {
      mockCalls = JSON.parse(docker(["exec", model, "node", "-e",
        "fetch('http://127.0.0.1:8789/calls').then(r=>r.text()).then(console.log)"])).calls;
    } catch { /* The fixture may have failed before its health check. */ }
  }
  process.stderr.write(JSON.stringify({ status: "failed", lastCheck: checks.at(-1) || "startup",
    reason: error.message, mockCalls: mockCalls.map(({ kind, event_id, alert_id,
      selected_event_ids, selected_alert_ids }) =>
      ({ kind, event_id, alert_id, selected_event_ids, selected_alert_ids })) }, null, 2) + "\n");
  process.exitCode = 1;
} finally {
  cleanup();
}

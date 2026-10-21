import { execFileSync, spawnSync } from "node:child_process";
import { randomBytes } from "node:crypto";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const network = "artex-blue-lab-e2e";
const database = "artex-blue-lab-e2e-pg";
const app = "artex-blue-lab-e2e-app";
const target = "artex-lab-target";
const image = process.env.ARTEX_E2E_IMAGE || "artex-ko:ssh";
const password = randomBytes(20).toString("hex");
const databasePassword = randomBytes(20).toString("hex");
const fixture = resolve("test-results/team-target/access.jsonl");
const results = [];
let networkCreated = false;
let targetConnected = false;
let databaseCreated = false;
let appCreated = false;

function docker(args, env = process.env) {
  return execFileSync("docker", args, { encoding: "utf8", timeout: 120_000, env }).trim();
}

function check(condition, label) {
  if (!condition) throw new Error(label);
  results.push(label);
}

async function waitFor(condition, label, attempts = 60) {
  for (let i = 0; i < attempts; i++) {
    try {
      if (await condition()) return;
    } catch {
      // Startup has not finished yet.
    }
    await new Promise((resolveDelay) => setTimeout(resolveDelay, 1000));
  }
  throw new Error(`Timed out: ${label}`);
}

async function api(base, token, method, path, body, expected, label) {
  const headers = token ? { Authorization: `Bearer ${token}` } : {};
  let payload;
  if (body instanceof FormData) {
    payload = body;
  } else if (body !== undefined) {
    headers["Content-Type"] = "application/json";
    payload = JSON.stringify(body);
  }
  const response = await fetch(`${base}${path}`, {
    method,
    headers,
    body: payload,
    signal: AbortSignal.timeout(30_000),
  });
  check(response.status === expected, `${label}: HTTP ${expected}`);
  const contentType = response.headers.get("content-type") || "";
  return contentType.includes("application/json") ? response.json() : response.text();
}

function cleanup() {
  if (appCreated) spawnSync("docker", ["rm", "-fv", app], { stdio: "ignore", timeout: 120_000 });
  if (databaseCreated) spawnSync("docker", ["rm", "-fv", database], { stdio: "ignore", timeout: 120_000 });
  if (targetConnected) spawnSync("docker", ["network", "disconnect", network, target], { stdio: "ignore", timeout: 120_000 });
  if (networkCreated) spawnSync("docker", ["network", "rm", network], { stdio: "ignore", timeout: 120_000 });
}

try {
  const running = docker(["ps", "-a", "--format", "{{.Names}}"]).split(/\r?\n/);
  check(!running.includes(app) && !running.includes(database), "Disposable container names available");
  const networks = docker(["network", "ls", "--format", "{{.Name}}"]).split(/\r?\n/);
  check(!networks.includes(network), "Disposable network name available");
  const targetState = docker(["inspect", target, "--format", "{{.State.Status}}"]).trim();
  check(targetState === "running", "Private lab target running");

  const imageSnapshot = docker(["image", "inspect", image, "--format", "{{.Id}} {{.Created}}"]).trim();
  docker(["network", "create", network]);
  networkCreated = true;
  docker(["network", "connect", network, target]);
  targetConnected = true;
  docker(
    ["run", "-d", "--name", database, "--network", network, "-e", "POSTGRES_USER=artex", "-e", "POSTGRES_PASSWORD", "-e", "POSTGRES_DB=artex", "postgres:16-alpine"],
    { ...process.env, POSTGRES_PASSWORD: databasePassword },
  );
  databaseCreated = true;
  await waitFor(() => spawnSync("docker", ["exec", database, "pg_isready", "-U", "artex", "-d", "artex"], { stdio: "ignore", timeout: 30_000 }).status === 0, "PostgreSQL ready");
  check(true, "Disposable PostgreSQL ready");

  const appEnv = {
    ...process.env,
    ARTEX_PG_DSN: `postgres://artex:${databasePassword}@${database}:5432/artex?sslmode=disable`,
    ARTEX_ADMIN_PASSWORD: password,
    ARTEX_SETUP_TOKEN: randomBytes(20).toString("hex"),
    OPENAI_API_KEY: "",
    ANTHROPIC_API_KEY: "",
    ARTEX_LLM_PROVIDER: "",
    ARTEX_LLM_MODEL: "",
  };
  docker(["run", "-d", "--name", app, "--network", network, "--entrypoint", "/app/artex", "-p", "127.0.0.1::8787", "-e", "ARTEX_PG_DSN", "-e", "ARTEX_ADMIN_PASSWORD", "-e", "ARTEX_SETUP_TOKEN", "-e", "OPENAI_API_KEY", "-e", "ANTHROPIC_API_KEY", "-e", "ARTEX_LLM_PROVIDER", "-e", "ARTEX_LLM_MODEL", image, "-addr", ":8787", "-proxy", ":8788"], appEnv);
  appCreated = true;
  const port = docker(["port", app, "8787/tcp"]).split(/\r?\n/)[0].split(":").at(-1);
  check(/^\d+$/.test(port), "Disposable app loopback port assigned");
  const base = `http://127.0.0.1:${port}`;
  await waitFor(async () => (await fetch(`${base}/api/health`, { signal: AbortSignal.timeout(2000) })).status === 200, "ARTEX health");
  check(true, "Disposable ARTEX app healthy");

  const targetStatus = docker(["run", "--rm", "--network", network, "python:3.12-alpine", "python", "-c", "from urllib.request import urlopen; print(urlopen('http://artex-lab-target:8080/debug/config', timeout=5).status)"]);
  check(targetStatus === "200", "Lab target reachable from private app network");

  await api(base, "", "GET", "/api/blue/overview", undefined, 401, "Unauthenticated Blue access rejected");
  const login = await api(base, "", "POST", "/api/auth/login", { username: "ARTEX", password }, 200, "Disposable admin login");
  const token = login.token;
  check(typeof token === "string" && token.length > 20, "JWT issued");
  const llm = await api(base, token, "GET", "/api/llm", undefined, 200, "LLM configuration read");
  check(llm.configured === false, "No external model configured in disposable app");

  const prefix = `lab-${randomBytes(5).toString("hex")}`;
  const occurred_at = new Date().toISOString();
  const inputEvents = [
    { source: prefix, external_id: "flow-1", event_type: `${prefix}.network.flow`, severity: "medium", asset: "artex-lab-target:8080", message: "GET /debug/config returned 200", occurred_at, raw: { src_ip: "192.0.2.10", dest_ip: "198.51.100.20", src_port: 54321, dest_port: 8080, proto: "TCP", method: "GET", url: "/debug/config", status: 200 } },
    { source: prefix, external_id: "auth-1", event_type: `${prefix}.auth.failure`, severity: "low", asset: "artex-lab-target:8080", message: "Failed test login", occurred_at, raw: { src_ip: "192.0.2.10" } },
  ];
  const imported = await api(base, token, "POST", "/api/blue/events/import", { events: inputEvents }, 201, "Blue JSON event import");
  check(imported.imported === 2 && imported.events?.length === 2, "Two isolated events stored");
  const flow = imported.events.find((item) => item.external_id === "flow-1");
  check(Number.isInteger(flow?.id), "Network event ID assigned");
  const traffic = await api(base, token, "GET", `/api/blue/traffic?source=${encodeURIComponent(prefix)}&protocol=TCP`, undefined, 200, "Blue traffic lookup");
  check(traffic.total === 1 && traffic.items?.[0]?.id === flow.id, "Network event appears in Blue Traffic");

  const rule = await api(base, token, "POST", "/api/blue/rules", { name: `${prefix} flow`, field: "event_type", operator: "equals", value: `${prefix}.network.flow`, severity: "high", enabled: true }, 201, "Blue detection rule created");
  check(Number.isInteger(rule.id), "Rule ID assigned");
  const tested = await api(base, token, "POST", `/api/blue/rules/${rule.id}/test`, { limit: 100 }, 200, "Detection dry run");
  check(tested.matched_count === 1, "Detection dry run matched one event");
  const run = await api(base, token, "POST", `/api/blue/rules/${rule.id}/run`, { limit: 100 }, 200, "Detection run");
  check(run.created_alerts === 1, "Detection created one alert");
  const rerun = await api(base, token, "POST", `/api/blue/rules/${rule.id}/run`, { limit: 100 }, 200, "Detection repeat run");
  check(rerun.created_alerts === 0, "Repeated detection did not duplicate alert");
  const reimported = await api(base, token, "POST", "/api/blue/events/import", { events: inputEvents }, 201, "Blue event reimport");
  check(reimported.events?.[0]?.id === imported.events[0].id && reimported.events?.[1]?.id === imported.events[1].id, "Reimport reused source and external IDs");
  check(reimported.alerts_created === 0, "Reimport did not duplicate alert");
  const alerts = await api(base, token, "GET", "/api/blue/alerts?limit=100", undefined, 200, "Blue alert list");
  const alert = alerts.find((item) => item.event_id === flow.id && item.rule_id === rule.id);
  check(Number.isInteger(alert?.id), "Alert linked to matching event and rule");

  const caseItem = await api(base, token, "POST", "/api/blue/cases", { title: `${prefix} investigation`, description: "Investigate lab target request", severity: "medium" }, 201, "Blue case created");
  check(Number.isInteger(caseItem.id), "Case ID assigned");
  const triaged = await api(base, token, "PATCH", `/api/blue/alerts/${alert.id}`, { case_id: caseItem.id, status: "investigating" }, 200, "Alert triaged into case");
  check(triaged.case_id === caseItem.id, "Alert case link persisted");
  const observation = await api(base, token, "POST", `/api/blue/cases/${caseItem.id}/observations`, { kind: "fact", summary: "Demo config endpoint returned 200", detail: "Observed in imported lab HTTP event.", event_id: flow.id, alert_id: alert.id, evidence_event_ids: [flow.id], evidence_alert_ids: [alert.id] }, 201, "Evidence-backed observation created");
  check(Number.isInteger(observation.id), "Observation ID assigned");
  const otherCase = await api(base, token, "POST", "/api/blue/cases", { title: `${prefix} other case`, severity: "low" }, 201, "Second Blue case created");
  await api(base, token, "POST", `/api/blue/events/${flow.id}/move-case`, { case_id: otherCase.id }, 409, "Cited event move rejected");
  const action = await api(base, token, "POST", `/api/blue/cases/${caseItem.id}/actions`, { kind: "isolate_host", target: "artex-lab-target", rationale: "Disposable lab audit trail only" }, 201, "Response proposal recorded");
  check(action.status === "pending", "Response begins pending");
  await api(base, token, "POST", `/api/blue/actions/${action.id}/complete`, { success: true, note: "Premature lab completion", evidence: "lab" }, 409, "Unapproved response completion rejected");
  const approved = await api(base, token, "POST", `/api/blue/actions/${action.id}/decision`, { approved: true, note: "Disposable lab approval" }, 200, "Response proposal approved");
  check(approved.status === "approved", "Approval state persisted");
  const completed = await api(base, token, "POST", `/api/blue/actions/${action.id}/complete`, { success: true, note: "Manual lab audit completed", evidence: "lab-ticket-only" }, 200, "Response completion recorded");
  check(completed.status === "completed", "Completion state persisted");

  const unlinkedTask = await api(base, token, "POST", "/api/blue/tasks", { title: `${prefix} unlinked`, description: "Test case requirement", priority: "low" }, 201, "Unlinked Blue task created");
  await api(base, token, "POST", `/api/blue/tasks/${unlinkedTask.id}/run`, {}, 409, "Blue task requires linked case");
  const linkedTask = await api(base, token, "POST", "/api/blue/tasks", { title: `${prefix} investigation`, description: "Assess the recorded disclosure", case_id: caseItem.id, priority: "medium" }, 201, "Linked Blue task created");
  await api(base, token, "POST", `/api/blue/tasks/${linkedTask.id}/run`, {}, 503, "Blue task reports missing model");
  const conversation = await api(base, token, "POST", "/api/blue/conversations", { title: `${prefix} chat` }, 200, "Blue conversation created");
  await api(base, token, "POST", `/api/blue/conversations/${conversation.id}/messages`, { message: "Summarize the attached lab evidence." }, 503, "Blue chat reports missing model");

  const logLines = readFileSync(fixture, "utf8").split(/\r?\n/).filter(Boolean).slice(-100);
  check(logLines.length > 0, "Lab asset log has events");
  const file = `${logLines.join("\n")}\n`;
  const form = new FormData();
  form.append("file", new Blob([file], { type: "application/x-ndjson" }), "access.jsonl");
  form.append("format", "jsonl");
  const fileImported = await api(base, token, "POST", "/api/blue/events/import", form, 201, "Lab asset JSONL multipart import");
  check(fileImported.imported > 0, "Lab asset HTTP log records imported");
  const fixtureTraffic = await api(base, token, "GET", "/api/blue/traffic?source=artex-lab-target&protocol=TCP", undefined, 200, "Lab asset traffic lookup");
  check(fixtureTraffic.total > 0, "Lab asset traffic visible after import");

  const redTask = await api(base, token, "POST", "/api/tasks", { description: `Inspect only ${target}:8080`, goal: `List HTTP status and title at http://${target}:8080/; do not alter the target`, timeout_seconds: 30, seed_first_intent: false }, 201, "Red task creation without model");
  check(redTask.id !== undefined, "Red task ID assigned");
  await api(base, token, "GET", `/api/tasks/${redTask.id}`, undefined, 200, "Red task record readable");
  await api(base, token, "GET", `/api/exploration/nodes?task=${redTask.id}&kind=goal`, undefined, 200, "Red task goal nodes readable");

  process.stdout.write(JSON.stringify({ status: "passed", image: imageSnapshot, checks: results, fixtureRecords: fileImported.imported, fixtureTraffic: fixtureTraffic.total, redTaskModelConfigured: false }, null, 2) + "\n");
} catch (error) {
  process.stderr.write(JSON.stringify({ status: "failed", failedAt: results.at(-1) || "startup", reason: error.message }, null, 2) + "\n");
  process.exitCode = 1;
} finally {
  cleanup();
}

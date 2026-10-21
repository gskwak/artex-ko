import { ApiError, http } from "@/lib/api";

export type BlueSeverity = "info" | "low" | "medium" | "high" | "critical";
export type BlueCaseStatus = "open" | "investigating" | "contained" | "resolved" | "closed";
export type BlueAlertStatus = "new" | "investigating" | "confirmed" | "false_positive" | "dismissed";
export type BlueObservationKind = "fact" | "hypothesis" | "unknown" | "recommendation";

export interface BlueOverview {
  case_count: number;
  open_case_count: number;
  event_count: number;
  alert_count: number;
  new_alert_count: number;
  rule_count: number;
  pending_action_count: number;
}

export interface BlueCase {
  id: number;
  title: string;
  description: string;
  status: BlueCaseStatus;
  severity: BlueSeverity;
  summary: string;
  red_finding_id?: number | null;
  created_at: string;
  updated_at: string;
}

export interface BlueAnalysisRun {
  id: number;
  case_id: number;
  status: "queued" | "running" | "completed" | "failed";
  observation_id?: number | null;
  error?: string;
  created_at: string;
  started_at?: string | null;
  finished_at?: string | null;
}

export interface BlueEvent {
  id: number;
  case_id?: number | null;
  source: string;
  external_id?: string;
  event_type: string;
  severity: BlueSeverity;
  asset: string;
  message: string;
  occurred_at: string;
  created_at: string;
  raw: Record<string, unknown>;
}

export interface BlueAlert {
  id: number;
  rule_id: number;
  event_id: number;
  case_id?: number | null;
  title: string;
  severity: BlueSeverity;
  status: BlueAlertStatus;
  reason: string;
  created_at: string;
  updated_at: string;
}

export interface BlueObservation {
  id: number;
  case_id: number;
  event_id?: number | null;
  alert_id?: number | null;
  kind: BlueObservationKind;
  summary: string;
  detail: string;
  author: string;
  created_at: string;
  evidence_event_ids?: number[];
  evidence_alert_ids?: number[];
  assessment?: {
    verdict?: "malicious" | "benign" | "inconclusive";
    confidence?: "low" | "medium" | "high";
    severity?: "none" | "unknown" | BlueSeverity;
  } | null;
}

export interface BlueEventInput {
  source: string;
  external_id?: string;
  event_type: string;
  message: string;
  severity?: BlueSeverity;
  asset?: string;
  occurred_at?: string;
  case_id?: number;
  raw?: Record<string, unknown>;
}

export interface BlueImportResult {
  imported: number;
  events: BlueEvent[];
  alerts_matched: number;
  alerts_created: number;
  detection_errors: number;
  warnings: string[];
}

export interface BlueEventSource {
  id: number;
  name: string;
  url: string;
  enabled: boolean;
  poll_interval_seconds: number;
  cursor: string;
  last_synced_at?: string | null;
  last_error: string;
  created_at: string;
  updated_at: string;
}

export interface BlueSourceSyncResult {
  source: BlueEventSource;
  processed: number;
  alerts_matched: number;
  alerts_created: number;
  detection_errors: number;
  warnings: string[];
}

export interface BlueEventSourceInput {
  name: string;
  url: string;
  enabled?: boolean;
  poll_interval_seconds?: number;
}

export interface BlueObservationInput {
  kind: BlueObservationKind;
  summary: string;
  detail: string;
  event_id?: number;
  alert_id?: number;
  evidence_event_ids?: number[];
  evidence_alert_ids?: number[];
}

export interface BlueAction {
  id: number;
  case_id: number;
  kind: string;
  target: string;
  rationale: string;
  params: Record<string, unknown>;
  status: "pending" | "approved" | "rejected" | "completed" | "failed";
  decision_note: string;
  requested_by: string;
  decided_by: string;
  completion_note: string;
  completion_evidence: string;
  completed_by: string;
  created_at: string;
  decided_at?: string | null;
  completed_at?: string | null;
}

type QueryValue = string | number | boolean | null | undefined;

function withQuery(path: string, query?: Record<string, QueryValue>): string {
  if (!query) return path;
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) {
    if (value !== undefined && value !== null && value !== "") params.set(key, String(value));
  }
  const suffix = params.toString();
  return suffix ? `${path}?${suffix}` : path;
}

function json(method: "POST" | "PATCH", body: unknown): RequestInit {
  return { method, body: JSON.stringify(body) };
}

/** A small typed entry point for blue pages. Existing JWT/error handling stays in http(). */
export function blueRequest<T>(path: string, init?: RequestInit): Promise<T> {
  return http<T>(`/blue${path.startsWith("/") ? path : `/${path}`}`, init);
}

async function uploadBlueFile<T>(path: string, file: File, source?: string): Promise<T> {
  const form = new FormData();
  form.set("file", file);
  if (source?.trim()) form.set("source", source.trim());
  const token = typeof window === "undefined" ? null : window.localStorage.getItem("artex_token");
  const response = await fetch(`/api/blue${path}`, {
    method: "POST",
    body: form,
    headers: token ? { Authorization: `Bearer ${token}` } : {},
  });
  if (response.status === 401 && typeof window !== "undefined") {
    window.localStorage.removeItem("artex_token");
    document.cookie = "artex_token=; path=/; max-age=0";
    window.location.href = "/login";
    throw new ApiError("Unauthorized", 401);
  }
  if (!response.ok) {
    let message = `POST /api/blue${path}: ${response.status}`;
    try {
      const payload = (await response.json()) as { error?: unknown };
      if (typeof payload.error === "string" && payload.error.trim()) message = payload.error.trim();
    } catch {
      // Keep HTTP status when the server did not return a JSON error.
    }
    throw new ApiError(message, response.status);
  }
  return response.json() as Promise<T>;
}

export const blueApi = {
  overview: () => blueRequest<BlueOverview>("/overview"),
  cases: (query?: { q?: string; status?: BlueCaseStatus; severity?: BlueSeverity; limit?: number; offset?: number }) =>
    blueRequest<BlueCase[]>(withQuery("/cases", query)),
  getCase: (id: number) => blueRequest<BlueCase>(`/cases/${id}`),
  createCase: (body: { title: string; description?: string; severity?: BlueSeverity }) =>
    blueRequest<BlueCase>("/cases", json("POST", body)),
  updateCase: (
    id: number,
    body: Partial<Pick<BlueCase, "title" | "description" | "status" | "severity" | "summary">>,
  ) => blueRequest<BlueCase>(`/cases/${id}`, json("PATCH", body)),
  events: (query?: {
    q?: string;
    case_id?: number;
    source?: string;
    severity?: BlueSeverity;
    limit?: number;
    offset?: number;
  }) => blueRequest<BlueEvent[]>(withQuery("/events", query)),
  traffic: (query?: { limit?: number; offset?: number; q?: string; source?: string; protocol?: string }) =>
    blueRequest<{ items: BlueEvent[]; total: number }>(withQuery("/traffic", query)),
  getEvent: (id: number) => blueRequest<BlueEvent>(`/events/${id}`),
  moveEventCase: (id: number, caseID: number) =>
    blueRequest<BlueEvent>(`/events/${id}/move-case`, json("POST", { case_id: caseID })),
  importEvents: (events: BlueEventInput[]) => blueRequest<BlueImportResult>("/events/import", json("POST", { events })),
  uploadEvents: (file: File, source?: string) => uploadBlueFile<BlueImportResult>("/events/import", file, source),
  eventSources: () => blueRequest<BlueEventSource[]>("/sources"),
  createEventSource: (body: BlueEventSourceInput) => blueRequest<BlueEventSource>("/sources", json("POST", body)),
  updateEventSource: (id: number, body: Partial<BlueEventSourceInput> & { reset_cursor?: boolean }) =>
    blueRequest<BlueEventSource>(`/sources/${id}`, json("PATCH", body)),
  deleteEventSource: (id: number) => blueRequest<void>(`/sources/${id}`, { method: "DELETE" }),
  syncEventSource: (id: number) => blueRequest<BlueSourceSyncResult>(`/sources/${id}/sync`, json("POST", {})),
  alerts: (query?: {
    q?: string;
    case_id?: number;
    status?: BlueAlertStatus;
    severity?: BlueSeverity;
    limit?: number;
    offset?: number;
  }) => blueRequest<BlueAlert[]>(withQuery("/alerts", query)),
  getAlert: (id: number) => blueRequest<BlueAlert>(`/alerts/${id}`),
  updateAlert: (id: number, body: Partial<Pick<BlueAlert, "status" | "reason" | "case_id">>) =>
    blueRequest<BlueAlert>(`/alerts/${id}`, json("PATCH", body)),
  observations: (caseID: number, query?: { limit?: number; offset?: number }) =>
    blueRequest<BlueObservation[]>(withQuery(`/cases/${caseID}/observations`, query)),
  addObservation: (caseID: number, body: BlueObservationInput) =>
    blueRequest<BlueObservation>(`/cases/${caseID}/observations`, json("POST", body)),
  analyzeCase: (caseID: number, evidence?: { event_ids?: number[]; alert_ids?: number[] }) =>
    blueRequest<{ run: BlueAnalysisRun }>(`/cases/${caseID}/analyze`, json("POST", evidence ?? {})),
  analyses: (caseID: number) => blueRequest<{ runs: BlueAnalysisRun[] }>(`/cases/${caseID}/analyses`),
  analysis: (runID: number) => blueRequest<{ run: BlueAnalysisRun }>(`/analyses/${runID}`),
  actions: (caseID: number) => blueRequest<BlueAction[]>(`/cases/${caseID}/actions`),
  proposeAction: (
    caseID: number,
    body: { kind: string; target: string; rationale: string; params?: Record<string, unknown> },
  ) => blueRequest<BlueAction>(`/cases/${caseID}/actions`, json("POST", body)),
};

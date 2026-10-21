import { type BlueAction, type BlueCase, type BlueEvent, blueRequest } from "@/lib/blue-api";

export type BlueAssetKind = "host" | "ip" | "domain" | "service" | "application" | "other";
export type BlueAssetStatus = "active" | "archived";

export interface BlueAsset {
  id: number;
  kind: BlueAssetKind;
  identifier: string;
  label: string;
  status: BlueAssetStatus;
  notes: string;
  created_at: string;
  updated_at: string;
}

export type BlueCaseAsset = BlueAsset & { explicit_link: boolean };
export type BlueAssetEvent = BlueEvent & { asset_id?: number | null };
export type BlueAssetAction = BlueAction & { asset_id?: number | null };

export interface BlueAssetActivity {
  cases: BlueCase[];
  events: BlueAssetEvent[];
  actions: BlueAssetAction[];
  case_count?: number;
  event_count?: number;
  action_count?: number;
}

export interface CreateBlueAsset {
  kind: BlueAssetKind;
  identifier: string;
  label: string;
  notes?: string;
}

export type UpdateBlueAsset = Partial<CreateBlueAsset> & { status?: BlueAssetStatus };

export interface BlueAssetListQuery {
  q?: string;
  status?: BlueAssetStatus;
  limit?: number;
  offset?: number;
}

function listQuery(query?: BlueAssetListQuery): string {
  if (!query) return "";
  const params = new URLSearchParams();
  if (query.q) params.set("q", query.q);
  if (query.status) params.set("status", query.status);
  if (query.limit !== undefined) params.set("limit", String(query.limit));
  if (query.offset !== undefined) params.set("offset", String(query.offset));
  const value = params.toString();
  return value ? `?${value}` : "";
}

function json(method: "POST" | "PATCH", body: unknown): RequestInit {
  return { method, body: JSON.stringify(body) };
}

/** Blue-owned assets are independent of Red Team task assets. */
export const blueAssetsApi = {
  list: (query?: BlueAssetListQuery) => blueRequest<BlueAsset[]>(`/assets${listQuery(query)}`),
  get: (id: number) => blueRequest<BlueAsset>(`/assets/${id}`),
  create: (body: CreateBlueAsset) => blueRequest<BlueAsset>("/assets", json("POST", body)),
  patch: (id: number, body: UpdateBlueAsset) => blueRequest<BlueAsset>(`/assets/${id}`, json("PATCH", body)),
  activity: (id: number) => blueRequest<BlueAssetActivity>(`/assets/${id}/activity`),
  caseAssets: (caseID: number) => blueRequest<BlueCaseAsset[]>(`/cases/${caseID}/assets`),
  linkCaseAsset: (caseID: number, assetID: number) =>
    blueRequest<BlueAsset>(`/cases/${caseID}/assets`, json("POST", { asset_id: assetID })),
  unlinkCaseAsset: (caseID: number, assetID: number) =>
    blueRequest<void>(`/cases/${caseID}/assets/${assetID}`, { method: "DELETE" }),
  assignEventAsset: (eventID: number, assetID: number | null) =>
    blueRequest<BlueAssetEvent>(`/events/${eventID}/asset`, json("PATCH", { asset_id: assetID })),
  proposeCaseAction: (
    caseID: number,
    body: { kind: string; target: string; rationale: string; params?: Record<string, unknown>; asset_id?: number },
  ) => blueRequest<BlueAssetAction>(`/cases/${caseID}/actions`, json("POST", body)),
};

import { blueRequest } from "@/lib/blue-api";

export type BlueTaskStatus = "open" | "in_progress" | "completed" | "blocked";
export type BlueTaskPriority = "low" | "medium" | "high" | "critical";

export interface BlueTask {
  id: number;
  title: string;
  description: string;
  case_id?: number | null;
  priority: BlueTaskPriority;
  status: BlueTaskStatus;
  result: string;
  analysis_run_id?: number | null;
  created_at: string;
  updated_at: string;
  completed_at?: string | null;
}

export interface CreateBlueTask {
  title: string;
  description?: string;
  case_id?: number;
  priority?: BlueTaskPriority;
}

export interface UpdateBlueTask {
  title?: string;
  description?: string;
  case_id?: number;
  clear_case?: boolean;
  priority?: BlueTaskPriority;
  status?: BlueTaskStatus;
  result?: string;
}

function queryString(query?: { status?: BlueTaskStatus; case_id?: number; limit?: number; offset?: number }): string {
  if (!query) return "";
  const params = new URLSearchParams();
  if (query.status) params.set("status", query.status);
  if (query.case_id) params.set("case_id", String(query.case_id));
  if (query.limit !== undefined) params.set("limit", String(query.limit));
  if (query.offset !== undefined) params.set("offset", String(query.offset));
  const value = params.toString();
  return value ? `?${value}` : "";
}

export const blueTaskApi = {
  list: (query?: { status?: BlueTaskStatus; case_id?: number; limit?: number; offset?: number }) =>
    blueRequest<BlueTask[]>(`/tasks${queryString(query)}`),
  get: (id: number) => blueRequest<BlueTask>(`/tasks/${id}`),
  create: (body: CreateBlueTask) => blueRequest<BlueTask>("/tasks", { method: "POST", body: JSON.stringify(body) }),
  update: (id: number, body: UpdateBlueTask) =>
    blueRequest<BlueTask>(`/tasks/${id}`, { method: "PATCH", body: JSON.stringify(body) }),
  run: (id: number) =>
    blueRequest<{ task: BlueTask; run: { id: number; status: string } }>(`/tasks/${id}/run`, {
      method: "POST",
      body: JSON.stringify({}),
    }),
};

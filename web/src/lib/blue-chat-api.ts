import { blueRequest } from "@/lib/blue-api";
import type { Activity, Conversation } from "@/lib/types";

type MessagesPage = {
  items: Activity[];
  cursor: number;
  running: boolean;
  hasMore?: boolean;
};

function messagePage(page: MessagesPage): MessagesPage {
  return {
    items: Array.isArray(page.items) ? page.items : [],
    cursor: page.cursor,
    running: !!page.running,
    hasMore: !!page.hasMore,
  };
}

function body(method: "POST" | "PATCH", value: unknown): RequestInit {
  return { method, body: JSON.stringify(value) };
}

/** The blue analyst has its own scoped, tool-free conversation routes. */
export const blueChatApi = {
  conversations: () =>
    blueRequest<{ conversations: Conversation[] }>("/conversations").then((result) => result.conversations ?? []),
  createConversation: (title = "", llmProfileID: number | null = null) =>
    blueRequest<Conversation>("/conversations", body("POST", { title, llm_profile_id: llmProfileID })),
  updateConversation: (id: number, change: { title?: string; pinned?: boolean }) =>
    blueRequest<Conversation>(`/conversations/${id}`, body("PATCH", change)),
  updateProfile: (id: number, llmProfileID: number | null) =>
    blueRequest<{ ok: boolean }>(`/conversations/${id}/profile`, body("PATCH", { llm_profile_id: llmProfileID })),
  deleteConversation: (id: number) => blueRequest<{ deleted: number }>(`/conversations/${id}`, { method: "DELETE" }),
  history: (id: number, before = 0, limit = 200) => {
    const query = new URLSearchParams({ limit: String(limit) });
    if (before > 0) query.set("before", String(before));
    return blueRequest<MessagesPage>(`/conversations/${id}/messages?${query}`).then(messagePage);
  },
  messagesSince: (id: number, since: number) =>
    blueRequest<MessagesPage>(`/conversations/${id}/messages?since=${since}`).then(messagePage),
  messageDetail: (id: number, seq: number) =>
    blueRequest<{ detail: string }>(`/conversations/${id}/messages/${seq}`).then((result) => result.detail ?? ""),
  send: (
    id: number,
    message: string,
    caseID?: number | null,
    evidence?: { event_ids?: number[]; alert_ids?: number[] },
  ) =>
    blueRequest<{ status: string }>(
      `/conversations/${id}/messages`,
      body("POST", { message, case_id: caseID, ...evidence }),
    ),
  stop: (id: number) => blueRequest<{ status: string }>(`/conversations/${id}/stop`, body("POST", {})),
};

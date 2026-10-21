"use client";

import * as React from "react";

import Link from "next/link";

import {
  ArrowLeft,
  ArrowUp,
  Bot,
  MoreHorizontal,
  Pin,
  PinOff,
  Plus,
  RefreshCw,
  Search,
  ShieldCheck,
  Square,
  Trash2,
} from "lucide-react";
import { toast } from "sonner";

import { Transcript } from "@/components/transcript";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { useActiveLocale } from "@/i18n/locale-store";
import { mergeActivities } from "@/lib/activity-merge";
import { api } from "@/lib/api";
import { blueChatApi } from "@/lib/blue-chat-api";
import { useBlueChatCopy } from "@/lib/blue-chat-copy";
import { shouldSubmitOnKey, useChatSendMode } from "@/lib/chat-send-mode";
import type { Activity, Conversation, LLMProfile } from "@/lib/types";
import { cn } from "@/lib/utils";

import { BlueError, BluePageHeader } from "../_components/blue-ui";

const HISTORY_PAGE = 200;

function pinned(conversation: Conversation): boolean {
  return conversation.pinned ?? Boolean(conversation.pinned_at);
}

function ModelPicker({
  profiles,
  selected,
  onChange,
  disabled,
}: {
  profiles: LLMProfile[];
  selected: number | null;
  onChange: (id: number | null) => void;
  disabled?: boolean;
}) {
  const copy = useBlueChatCopy();
  const defaultProfile = profiles.find((profile) => profile.is_default);
  return (
    <div className="flex min-w-0 items-center gap-2 text-xs text-muted-foreground">
      <span className="shrink-0">{copy.model}</span>
      <Select
        value={selected == null ? "default" : String(selected)}
        onValueChange={(value) => onChange(value === "default" ? null : Number(value))}
        disabled={disabled}
      >
        <SelectTrigger size="sm" className="h-8 min-w-0 max-w-56 text-xs">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="default">
            {copy.defaultModel}
            {defaultProfile ? ` · ${defaultProfile.name}` : ""}
          </SelectItem>
          {profiles.map((profile) => (
            <SelectItem key={profile.id} value={String(profile.id)}>
              {profile.name} · {profile.model}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}

function Composer({
  value,
  onChange,
  onSend,
  onStop,
  busy,
  running,
  disabled,
}: {
  value: string;
  onChange: (value: string) => void;
  onSend: () => void;
  onStop?: () => void;
  busy: boolean;
  running: boolean;
  disabled?: boolean;
}) {
  const copy = useBlueChatCopy();
  const sendMode = useChatSendMode();
  const onKeyDown = (event: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (!shouldSubmitOnKey(event, sendMode)) return;
    event.preventDefault();
    if (!busy && !running && !disabled && value.trim()) onSend();
  };
  return (
    <div className="flex items-end gap-2 border-t bg-background p-3">
      <Textarea
        value={value}
        onChange={(event) => onChange(event.target.value)}
        onKeyDown={onKeyDown}
        placeholder={copy.placeholder}
        aria-label={copy.placeholder}
        className="max-h-40 min-h-10 flex-1 resize-none"
        rows={2}
        maxLength={16000}
        disabled={busy || running || disabled}
      />
      {running ? (
        <Button
          size="icon"
          variant="destructive"
          onClick={onStop}
          disabled={busy}
          aria-label={copy.stop}
          title={copy.stop}
        >
          <Square className="size-3.5 fill-current" />
        </Button>
      ) : (
        <Button
          size="icon"
          onClick={onSend}
          disabled={busy || Boolean(disabled) || !value.trim()}
          aria-label={copy.send}
          title={copy.send}
        >
          <ArrowUp className="size-4" />
        </Button>
      )}
    </div>
  );
}

function ConversationView({
  conversation,
  profiles,
  onUpdated,
  onBack,
}: {
  conversation: Conversation;
  profiles: LLMProfile[];
  onUpdated: () => void;
  onBack: () => void;
}) {
  const copy = useBlueChatCopy();
  const [messages, setMessages] = React.useState<Activity[]>([]);
  const [loaded, setLoaded] = React.useState(false);
  const [loadError, setLoadError] = React.useState("");
  const [hasMore, setHasMore] = React.useState(false);
  const [loadingMore, setLoadingMore] = React.useState(false);
  const [running, setRunning] = React.useState(!!conversation.running);
  const [busy, setBusy] = React.useState(false);
  const [input, setInput] = React.useState("");
  const [profileBusy, setProfileBusy] = React.useState(false);
  const cursorRef = React.useRef(0);
  const earliestRef = React.useRef(0);
  const contentRef = React.useRef<HTMLDivElement | null>(null);
  const atBottomRef = React.useRef(true);
  const wasRunningRef = React.useRef(!!conversation.running);
  const viewport = React.useCallback(
    () => contentRef.current?.closest('[data-slot="scroll-area-viewport"]') as HTMLElement | null,
    [],
  );

  // biome-ignore lint/correctness/useExhaustiveDependencies: running is a live status; resetting history is only for a different conversation.
  React.useEffect(() => {
    let active = true;
    cursorRef.current = 0;
    earliestRef.current = 0;
    setMessages([]);
    setLoaded(false);
    setLoadError("");
    setHasMore(false);
    setRunning(!!conversation.running);
    wasRunningRef.current = !!conversation.running;
    blueChatApi.history(conversation.id, 0, HISTORY_PAGE).then(
      (page) => {
        if (!active) return;
        setMessages(page.items);
        cursorRef.current = page.cursor;
        earliestRef.current = page.items[0]?.seq ?? 0;
        setHasMore(!!page.hasMore);
        setRunning(page.running);
        wasRunningRef.current = page.running;
        setLoaded(true);
      },
      (cause) => {
        if (!active) return;
        setLoadError(cause instanceof Error ? cause.message : copy.loadFailed);
        setLoaded(true);
      },
    );
    return () => {
      active = false;
    };
  }, [conversation.id, copy.loadFailed]);

  React.useEffect(() => {
    if (conversation.running) {
      wasRunningRef.current = true;
      setRunning(true);
    }
  }, [conversation.running]);

  // Poll the scoped transcript even when idle so another tab's turns appear here.
  React.useEffect(() => {
    if (!loaded) return;
    let active = true;
    let timer: ReturnType<typeof setTimeout>;
    const tick = async () => {
      try {
        const page = await blueChatApi.messagesSince(conversation.id, cursorRef.current);
        if (!active) return;
        if (page.items.length) setMessages((current) => mergeActivities(current, page.items));
        cursorRef.current = Math.max(cursorRef.current, page.cursor);
        if (wasRunningRef.current && !page.running) onUpdated();
        wasRunningRef.current = page.running;
        setRunning(page.running);
      } catch {
        // A temporary network error leaves the transcript intact; the next poll retries.
      } finally {
        if (active) timer = setTimeout(() => void tick(), running ? 1000 : 2500);
      }
    };
    void tick();
    return () => {
      active = false;
      clearTimeout(timer);
    };
  }, [conversation.id, loaded, onUpdated, running]);

  React.useEffect(() => {
    const area = viewport();
    if (!area) return;
    const onScroll = () => {
      atBottomRef.current = area.scrollTop + area.clientHeight >= area.scrollHeight - 80;
    };
    area.addEventListener("scroll", onScroll, { passive: true });
    return () => area.removeEventListener("scroll", onScroll);
  }, [viewport]);

  // biome-ignore lint/correctness/useExhaustiveDependencies: new transcript rows intentionally trigger bottom anchoring.
  React.useLayoutEffect(() => {
    if (!atBottomRef.current) return;
    const area = viewport();
    if (area) area.scrollTop = area.scrollHeight;
  }, [messages, viewport]);

  const fetchDetail = React.useCallback(
    (seq: number) => blueChatApi.messageDetail(conversation.id, seq),
    [conversation.id],
  );

  async function loadEarlier() {
    if (loadingMore || !hasMore || !earliestRef.current) return;
    setLoadingMore(true);
    const area = viewport();
    const previousHeight = area?.scrollHeight ?? 0;
    const previousTop = area?.scrollTop ?? 0;
    try {
      const page = await blueChatApi.history(conversation.id, earliestRef.current, HISTORY_PAGE);
      setMessages((current) => mergeActivities(page.items, current));
      earliestRef.current = page.items[0]?.seq ?? earliestRef.current;
      setHasMore(!!page.hasMore);
      atBottomRef.current = false;
      requestAnimationFrame(() => {
        const current = viewport();
        if (current) current.scrollTop = previousTop + current.scrollHeight - previousHeight;
      });
    } catch (cause) {
      toast.error(cause instanceof Error ? cause.message : copy.loadFailed);
    } finally {
      setLoadingMore(false);
    }
  }

  async function send() {
    const message = input.trim();
    if (!message || busy || running) return;
    setInput("");
    setBusy(true);
    try {
      await blueChatApi.send(conversation.id, message);
      wasRunningRef.current = true;
      setRunning(true);
      onUpdated();
    } catch (cause) {
      setInput(message);
      toast.error(cause instanceof Error ? cause.message : copy.sendFailed);
    } finally {
      setBusy(false);
    }
  }

  async function stop() {
    if (busy) return;
    setBusy(true);
    try {
      await blueChatApi.stop(conversation.id);
    } catch (cause) {
      toast.error(cause instanceof Error ? cause.message : copy.stopFailed);
    } finally {
      setBusy(false);
    }
  }

  async function changeProfile(id: number | null) {
    setProfileBusy(true);
    try {
      await blueChatApi.updateProfile(conversation.id, id);
      onUpdated();
    } catch (cause) {
      toast.error(cause instanceof Error ? cause.message : copy.saveFailed);
    } finally {
      setProfileBusy(false);
    }
  }

  return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col">
      <div className="flex flex-wrap items-center gap-2 border-b px-4 py-3">
        <Button size="icon" variant="ghost" className="md:hidden" onClick={onBack} aria-label={copy.conversations}>
          <ArrowLeft className="size-4" />
        </Button>
        <Bot className="size-4 shrink-0 text-primary" />
        <span className="min-w-0 flex-1 truncate text-sm font-medium">
          {conversation.title || copy.newConversation}
        </span>
        {running && (
          <span className="text-xs text-primary" role="status">
            {copy.running}
          </span>
        )}
      </div>
      <ScrollArea className="min-h-0 flex-1">
        <div ref={contentRef} className="mx-auto max-w-4xl space-y-3 px-4 py-5">
          {loadError && (
            <BlueError message={loadError} retry={() => window.location.reload()} retryLabel={copy.refresh} />
          )}
          {!loaded && <p className="text-center text-sm text-muted-foreground">{copy.loading}</p>}
          {hasMore && (
            <div className="text-center">
              <Button size="sm" variant="ghost" onClick={() => void loadEarlier()} disabled={loadingMore}>
                {copy.loadEarlier}
              </Button>
            </div>
          )}
          {loaded && messages.length === 0 && !loadError && (
            <p className="py-12 text-center text-sm text-muted-foreground">{copy.emptyHint}</p>
          )}
          <Transcript activity={messages} live={running} chat fetchDetail={fetchDetail} />
        </div>
      </ScrollArea>
      <div className="border-t px-3 py-2">
        <ModelPicker
          profiles={profiles}
          selected={conversation.llm_profile_id ?? null}
          onChange={(id) => void changeProfile(id)}
          disabled={profileBusy || running}
        />
      </div>
      <Composer
        value={input}
        onChange={setInput}
        onSend={() => void send()}
        onStop={() => void stop()}
        busy={busy}
        running={running}
      />
    </div>
  );
}

function DraftView({
  profiles,
  onStarted,
}: {
  profiles: LLMProfile[];
  onStarted: (conversation: Conversation) => void;
}) {
  const copy = useBlueChatCopy();
  const [input, setInput] = React.useState("");
  const [profileID, setProfileID] = React.useState<number | null>(null);
  const [busy, setBusy] = React.useState(false);
  async function send() {
    const message = input.trim();
    if (!message || busy) return;
    setBusy(true);
    try {
      const conversation = await blueChatApi.createConversation("", profileID);
      await blueChatApi.send(conversation.id, message);
      setInput("");
      onStarted({ ...conversation, running: true });
    } catch (cause) {
      toast.error(cause instanceof Error ? cause.message : copy.sendFailed);
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col">
      <div className="flex flex-1 flex-col items-center justify-center gap-3 px-5 text-center">
        <div className="flex size-14 items-center justify-center rounded-full bg-primary/10">
          <ShieldCheck className="size-7 text-primary" />
        </div>
        <h2 className="text-lg font-semibold">{copy.emptyTitle}</h2>
        <p className="max-w-md text-sm text-muted-foreground">{copy.emptyHint}</p>
        <p className="max-w-lg text-xs text-muted-foreground">{copy.scope}</p>
        <Link href="/blue/cases" className="text-sm text-primary underline-offset-4 hover:underline">
          {copy.linkedCases}
        </Link>
        {profiles.length === 0 && (
          <Link href="/system/llm" className="text-xs text-muted-foreground underline-offset-4 hover:underline">
            {copy.configureModel}
          </Link>
        )}
      </div>
      <div className="border-t px-3 py-2">
        <ModelPicker profiles={profiles} selected={profileID} onChange={setProfileID} disabled={busy} />
      </div>
      <Composer value={input} onChange={setInput} onSend={() => void send()} busy={busy} running={false} />
    </div>
  );
}

export default function BlueChatPage() {
  const copy = useBlueChatCopy();
  const locale = useActiveLocale();
  const [conversations, setConversations] = React.useState<Conversation[]>([]);
  const [profiles, setProfiles] = React.useState<LLMProfile[]>([]);
  const [selectedID, setSelectedID] = React.useState<number | null>(null);
  const [loaded, setLoaded] = React.useState(false);
  const [error, setError] = React.useState("");
  const [search, setSearch] = React.useState("");
  const [renaming, setRenaming] = React.useState<Conversation | null>(null);
  const [renameText, setRenameText] = React.useState("");
  const [deleting, setDeleting] = React.useState<Conversation | null>(null);
  const [actionBusy, setActionBusy] = React.useState(false);
  const requestRef = React.useRef(0);

  const reload = React.useCallback(async () => {
    const request = ++requestRef.current;
    try {
      const items = await blueChatApi.conversations();
      if (request !== requestRef.current) return;
      setConversations(items);
      setError("");
    } catch (cause) {
      if (request === requestRef.current) setError(cause instanceof Error ? cause.message : copy.loadFailed);
    } finally {
      if (request === requestRef.current) setLoaded(true);
    }
  }, [copy.loadFailed]);

  React.useEffect(() => {
    const id = Number(new URLSearchParams(window.location.search).get("c"));
    if (Number.isSafeInteger(id) && id > 0) setSelectedID(id);
    api
      .llmProfiles()
      .then(setProfiles)
      .catch(() => {
        // Chat remains usable through an agent-bound or environment model.
      });
  }, []);

  React.useEffect(() => {
    let active = true;
    let timer: ReturnType<typeof setTimeout>;
    const tick = async () => {
      await reload();
      if (active) timer = setTimeout(() => void tick(), 4000);
    };
    void tick();
    return () => {
      active = false;
      requestRef.current++;
      clearTimeout(timer);
    };
  }, [reload]);

  React.useEffect(() => {
    const url = new URL(window.location.href);
    if (selectedID == null) url.searchParams.delete("c");
    else url.searchParams.set("c", String(selectedID));
    window.history.replaceState(null, "", url);
  }, [selectedID]);

  const selected = conversations.find((conversation) => conversation.id === selectedID) ?? null;
  const query = search.trim().toLocaleLowerCase();
  const visible = conversations.filter(
    (conversation) => !query || `${conversation.title} ${conversation.id}`.toLocaleLowerCase().includes(query),
  );
  const dateLocale = { ko: "ko-KR", zh: "zh-CN", es: "es-ES", en: "en-US" }[locale];

  async function saveRename() {
    if (!renaming || !renameText.trim()) return;
    setActionBusy(true);
    try {
      await blueChatApi.updateConversation(renaming.id, { title: renameText.trim() });
      setRenaming(null);
      await reload();
    } catch (cause) {
      toast.error(cause instanceof Error ? cause.message : copy.saveFailed);
    } finally {
      setActionBusy(false);
    }
  }

  async function togglePin(conversation: Conversation) {
    try {
      await blueChatApi.updateConversation(conversation.id, { pinned: !pinned(conversation) });
      await reload();
    } catch (cause) {
      toast.error(cause instanceof Error ? cause.message : copy.saveFailed);
    }
  }

  async function deleteConversation() {
    if (!deleting) return;
    setActionBusy(true);
    try {
      await blueChatApi.deleteConversation(deleting.id);
      if (selectedID === deleting.id) setSelectedID(null);
      setDeleting(null);
      await reload();
    } catch (cause) {
      toast.error(cause instanceof Error ? cause.message : copy.deleteFailed);
    } finally {
      setActionBusy(false);
    }
  }

  return (
    <div>
      <BluePageHeader
        title={copy.title}
        description={copy.description}
        actions={
          <Button size="sm" variant="outline" onClick={() => void reload()} aria-label={copy.refresh}>
            <RefreshCw className="size-4" />
            {copy.refresh}
          </Button>
        }
      />
      {error && (
        <div className="mb-3">
          <BlueError message={error} retry={() => void reload()} retryLabel={copy.refresh} />
        </div>
      )}
      <Card className="flex h-[calc(100dvh-13rem)] min-h-[32rem] flex-col overflow-hidden p-0 md:flex-row">
        <aside
          className={cn(
            "flex h-52 shrink-0 flex-col border-b md:h-auto md:w-72 md:border-b-0 md:border-r",
            selectedID != null && "max-md:hidden",
          )}
        >
          <div className="flex items-center justify-between gap-2 border-b p-3">
            <span className="text-sm font-semibold">{copy.conversations}</span>
            <Button size="sm" onClick={() => setSelectedID(null)}>
              <Plus className="size-4" />
              {copy.newConversation}
            </Button>
          </div>
          <div className="relative px-3 py-2">
            <Search className="absolute left-5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              className="h-8 pl-8 text-xs"
              placeholder={copy.search}
              aria-label={copy.search}
              value={search}
              onChange={(event) => setSearch(event.target.value)}
            />
          </div>
          <ScrollArea className="min-h-0 flex-1">
            <div className="space-y-0.5 px-2 pb-2">
              {!loaded && <p className="px-2 py-5 text-center text-xs text-muted-foreground">{copy.loading}</p>}
              {loaded && conversations.length === 0 && (
                <p className="px-2 py-5 text-center text-xs text-muted-foreground">{copy.noConversations}</p>
              )}
              {loaded && conversations.length > 0 && visible.length === 0 && (
                <p className="px-2 py-5 text-center text-xs text-muted-foreground">{copy.noMatches}</p>
              )}
              {visible.map((conversation) => (
                <div
                  key={conversation.id}
                  className={cn(
                    "flex items-center rounded-md hover:bg-muted/60",
                    selectedID === conversation.id && "bg-primary/10",
                  )}
                >
                  <button
                    type="button"
                    onClick={() => setSelectedID(conversation.id)}
                    className="min-w-0 flex-1 px-2 py-2 text-left"
                    aria-current={selectedID === conversation.id ? "page" : undefined}
                  >
                    <span className="flex items-center gap-1.5 truncate text-sm">
                      {pinned(conversation) && <Pin className="size-3 shrink-0 text-primary" />}
                      {conversation.running && (
                        <span className="size-1.5 shrink-0 animate-pulse rounded-full bg-primary" />
                      )}
                      <span className="truncate">{conversation.title || copy.newConversation}</span>
                    </span>
                    <span className="text-[11px] text-muted-foreground">
                      {new Date(conversation.updated_at).toLocaleString(dateLocale)}
                    </span>
                  </button>
                  <DropdownMenu>
                    <DropdownMenuTrigger asChild>
                      <Button
                        size="icon"
                        variant="ghost"
                        className="mr-1 size-7"
                        aria-label={conversation.title || copy.newConversation}
                      >
                        <MoreHorizontal className="size-4" />
                      </Button>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end">
                      <DropdownMenuItem
                        onClick={() => {
                          setRenaming(conversation);
                          setRenameText(conversation.title);
                        }}
                      >
                        {copy.rename}
                      </DropdownMenuItem>
                      <DropdownMenuItem onClick={() => void togglePin(conversation)}>
                        {pinned(conversation) ? <PinOff className="size-4" /> : <Pin className="size-4" />}
                        {pinned(conversation) ? copy.unpin : copy.pin}
                      </DropdownMenuItem>
                      <DropdownMenuSeparator />
                      <DropdownMenuItem variant="destructive" onClick={() => setDeleting(conversation)}>
                        <Trash2 className="size-4" />
                        {copy.delete}
                      </DropdownMenuItem>
                    </DropdownMenuContent>
                  </DropdownMenu>
                </div>
              ))}
            </div>
          </ScrollArea>
        </aside>
        {selected ? (
          <ConversationView
            key={selected.id}
            conversation={selected}
            profiles={profiles}
            onUpdated={reload}
            onBack={() => setSelectedID(null)}
          />
        ) : (
          <DraftView
            profiles={profiles}
            onStarted={(conversation) => {
              setConversations((current) => [conversation, ...current.filter((item) => item.id !== conversation.id)]);
              setSelectedID(conversation.id);
              void reload();
            }}
          />
        )}
      </Card>

      <Dialog
        open={renaming != null}
        onOpenChange={(open) => {
          if (!open) setRenaming(null);
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{copy.rename}</DialogTitle>
          </DialogHeader>
          <form
            onSubmit={(event) => {
              event.preventDefault();
              void saveRename();
            }}
            className="space-y-4"
          >
            <Input
              autoFocus
              aria-label={copy.renameTitle}
              value={renameText}
              onChange={(event) => setRenameText(event.target.value)}
              maxLength={240}
            />
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setRenaming(null)}>
                {copy.cancel}
              </Button>
              <Button type="submit" disabled={actionBusy || !renameText.trim()}>
                {copy.save}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <AlertDialog
        open={deleting != null}
        onOpenChange={(open) => {
          if (!open) setDeleting(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{copy.deleteTitle}</AlertDialogTitle>
            <AlertDialogDescription>{copy.deleteDescription}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{copy.cancel}</AlertDialogCancel>
            <AlertDialogAction
              disabled={actionBusy}
              onClick={(event) => {
                event.preventDefault();
                void deleteConversation();
              }}
            >
              {copy.delete}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

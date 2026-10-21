"use client";

import * as React from "react";

import {
  ArrowUp,
  Bot,
  ChevronDown,
  ChevronRight,
  FolderOpen,
  ListChecks,
  Loader2,
  MoreHorizontal,
  Pencil,
  Pin,
  PinOff,
  Plus,
  Search,
  Square,
  Trash2,
  X,
  Zap,
} from "lucide-react";
import { useTranslations } from "next-intl";
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
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Spinner } from "@/components/ui/spinner";
import { Textarea } from "@/components/ui/textarea";
import { useActiveLocale } from "@/i18n/locale-store";
import { mergeActivities } from "@/lib/activity-merge";
import { api } from "@/lib/api";
import { type BlueCase, blueApi } from "@/lib/blue-api";
import { blueChatApi } from "@/lib/blue-chat-api";
import { useBlueChatCopy } from "@/lib/blue-chat-copy";
import { shouldSubmitOnKey, useChatSendMode } from "@/lib/chat-send-mode";
import type { Activity, Conversation, LLMProfile } from "@/lib/types";
import { cn } from "@/lib/utils";

import { BlueError } from "../_components/blue-ui";

const HISTORY_PAGE = 200;
const CONVERSATION_LIST_PAGE = 100;
type CaseSelection = Pick<BlueCase, "id" | "title">;

function CaseEvidencePicker({
  selected,
  onChange,
  disabled,
}: {
  selected: CaseSelection | null;
  onChange: (item: CaseSelection | null) => void;
  disabled?: boolean;
}) {
  const copy = useBlueChatCopy();
  const [open, setOpen] = React.useState(false);
  const [cases, setCases] = React.useState<BlueCase[]>([]);
  const [query, setQuery] = React.useState("");
  const [loading, setLoading] = React.useState(false);
  const [lookupBusy, setLookupBusy] = React.useState(false);

  React.useEffect(() => {
    if (!open) return;
    let active = true;
    setLoading(true);
    const timer = setTimeout(
      () => {
        blueApi
          .cases({ limit: 100, q: query.trim() })
          .then(
            (items) => {
              if (active) setCases(items);
            },
            (cause: unknown) => {
              if (active) toast.error(cause instanceof Error ? cause.message : copy.loadFailed);
            },
          )
          .finally(() => {
            if (active) setLoading(false);
          });
      },
      query.trim() ? 200 : 0,
    );
    return () => {
      active = false;
      clearTimeout(timer);
    };
  }, [open, query, copy.loadFailed]);

  const lookupID = Number(query.trim());
  const canLookupID = Number.isSafeInteger(lookupID) && lookupID > 0 && !cases.some((item) => item.id === lookupID);

  async function findByID() {
    if (!canLookupID || lookupBusy) return;
    setLookupBusy(true);
    try {
      const item = await blueApi.getCase(lookupID);
      onChange(item);
      setOpen(false);
    } catch (cause) {
      toast.error(cause instanceof Error ? cause.message : copy.caseLookupFailed);
    } finally {
      setLookupBusy(false);
    }
  }

  return (
    <div className="flex min-w-0 items-center gap-1.5">
      <Popover open={open} onOpenChange={disabled ? undefined : setOpen}>
        <PopoverTrigger asChild>
          <Button type="button" variant="outline" size="sm" disabled={disabled} className="max-w-full">
            <FolderOpen className="size-3.5" />
            <span className="truncate" title={selected?.title}>
              {selected ? `${copy.caseEvidence} #${selected.id}: ${selected.title}` : copy.attachCase}
            </span>
          </Button>
        </PopoverTrigger>
        <PopoverContent align="start" className="w-72 p-2">
          <Input
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter" && canLookupID) {
                event.preventDefault();
                void findByID();
              }
            }}
            placeholder={copy.caseSearch}
            aria-label={copy.caseSearch}
            className="h-8 text-xs"
          />
          <div className="mt-2 max-h-52 overflow-y-auto">
            {loading && <p className="px-2 py-3 text-xs text-muted-foreground">{copy.loading}</p>}
            {!loading && cases.length === 0 && (
              <p className="px-2 py-3 text-xs text-muted-foreground">{copy.noCaseMatches}</p>
            )}
            {!loading &&
              cases.map((item) => (
                <button
                  key={item.id}
                  type="button"
                  className="flex w-full flex-col rounded px-2 py-1.5 text-left text-xs hover:bg-accent"
                  onClick={() => {
                    onChange(item);
                    setOpen(false);
                  }}
                >
                  <span className="truncate font-medium">
                    #{item.id} · {item.title}
                  </span>
                  <span className="text-muted-foreground">{item.status}</span>
                </button>
              ))}
          </div>
          {canLookupID && (
            <Button
              type="button"
              size="sm"
              variant="outline"
              className="mt-2 w-full"
              onClick={() => void findByID()}
              disabled={lookupBusy}
            >
              {lookupBusy && <Loader2 className="size-3.5 animate-spin" />}#{lookupID}
            </Button>
          )}
        </PopoverContent>
      </Popover>
      {selected && (
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          disabled={disabled}
          onClick={() => onChange(null)}
          aria-label={copy.removeCase}
          title={copy.removeCase}
        >
          <X className="size-3.5" />
        </Button>
      )}
    </div>
  );
}

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
  const t = useTranslations("chat");
  const [open, setOpen] = React.useState(false);
  const defaultProfile = profiles.find((profile) => profile.is_default);
  const currentProfile = selected == null ? null : profiles.find((profile) => Number(profile.id) === selected);
  const defaultLabel = defaultProfile ? t("llmProfile.defaultNamed", { name: defaultProfile.name }) : copy.defaultModel;
  const label = currentProfile?.name ?? defaultLabel;
  return (
    <div className="flex min-w-0 shrink-0 items-center gap-1 px-1 pt-0.5 pb-1">
      <Zap className="size-3 shrink-0 text-muted-foreground/50" />
      <span className="truncate text-xs text-muted-foreground/70" title={label}>
        {label}
      </span>
      <Popover open={open} onOpenChange={disabled ? undefined : setOpen}>
        <PopoverTrigger asChild>
          <button
            type="button"
            disabled={disabled}
            className="flex shrink-0 items-center gap-0.5 text-xs text-primary hover:underline disabled:pointer-events-none disabled:opacity-40"
          >
            {t("llmProfile.change")}
            <ChevronDown className="size-3" />
          </button>
        </PopoverTrigger>
        <PopoverContent align="start" className="w-64 p-1">
          <p className="px-2 py-1 text-[11px] font-medium text-muted-foreground">{t("llmProfile.select")}</p>
          <button
            type="button"
            onClick={() => {
              onChange(null);
              setOpen(false);
            }}
            className={cn(
              "flex w-full flex-col rounded px-2 py-1.5 text-left hover:bg-accent",
              selected == null && "bg-accent",
            )}
          >
            <span className="text-sm">{defaultLabel}</span>
            {defaultProfile && (
              <span className="text-[11px] text-muted-foreground">
                {defaultProfile.format} · {defaultProfile.model}
              </span>
            )}
          </button>
          {profiles.map((profile) => (
            <button
              key={profile.id}
              type="button"
              onClick={() => {
                onChange(Number(profile.id));
                setOpen(false);
              }}
              className={cn(
                "flex w-full flex-col rounded px-2 py-1.5 text-left hover:bg-accent",
                selected === Number(profile.id) && "bg-accent",
              )}
            >
              <span className="text-sm">{profile.name}</span>
              <span className="text-[11px] text-muted-foreground">
                {profile.format} · {profile.model}
              </span>
            </button>
          ))}
        </PopoverContent>
      </Popover>
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
  leftSlot,
  evidenceSlot,
}: {
  value: string;
  onChange: (value: string) => void;
  onSend: () => void;
  onStop?: () => void;
  busy: boolean;
  running: boolean;
  disabled?: boolean;
  leftSlot?: React.ReactNode;
  evidenceSlot?: React.ReactNode;
}) {
  const copy = useBlueChatCopy();
  const sendMode = useChatSendMode();
  const onKeyDown = (event: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (!shouldSubmitOnKey(event, sendMode)) return;
    event.preventDefault();
    if (!busy && !running && !disabled && value.trim()) onSend();
  };
  return (
    <div className="border-t p-3">
      <div className="flex flex-wrap items-center gap-2">
        {leftSlot && <div className="w-full sm:w-auto">{leftSlot}</div>}
        <Textarea
          value={value}
          onChange={(event) => onChange(event.target.value)}
          onKeyDown={onKeyDown}
          placeholder={copy.placeholder}
          aria-label={copy.placeholder}
          className="max-h-40 min-h-10 min-w-0 flex-1 resize-none"
          rows={1}
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
      {evidenceSlot && <div className="mt-2">{evidenceSlot}</div>}
    </div>
  );
}

function ConversationView({
  conversation,
  profiles,
  onUpdated,
}: {
  conversation: Conversation;
  profiles: LLMProfile[];
  onUpdated: () => void;
}) {
  const copy = useBlueChatCopy();
  const t = useTranslations("chat");
  const [messages, setMessages] = React.useState<Activity[]>([]);
  const [loaded, setLoaded] = React.useState(false);
  const [loadError, setLoadError] = React.useState("");
  const [hasMore, setHasMore] = React.useState(false);
  const [loadingMore, setLoadingMore] = React.useState(false);
  const [running, setRunning] = React.useState(!!conversation.running);
  const [busy, setBusy] = React.useState(false);
  const [input, setInput] = React.useState("");
  const [attachedCase, setAttachedCase] = React.useState<CaseSelection | null>(null);
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
        const lastUser = [...page.items].reverse().find((item) => item.kind === "user");
        const linkedCase = lastUser?.metadata?.blue_case;
        setAttachedCase(linkedCase ? { id: linkedCase.id, title: linkedCase.title } : null);
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
      await blueChatApi.send(conversation.id, message, attachedCase?.id);
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
      <div className="flex min-w-0 flex-wrap items-center gap-2 border-b px-4 py-2.5">
        <Bot className="size-4 shrink-0 text-muted-foreground" />
        <span className="min-w-0 max-w-48 truncate text-sm font-medium">{copy.analyst}</span>
        <span className="hidden shrink-0 font-mono text-xs text-muted-foreground sm:inline">
          {conversation.agent_key}
        </span>
        <span className="min-w-0 truncate text-xs text-muted-foreground">{copy.description}</span>
        {running && (
          <span
            className="inline-flex items-center gap-1 rounded bg-blue-500/15 px-1.5 py-0.5 text-[10px] font-medium text-blue-600 dark:text-blue-400"
            role="status"
          >
            <span className="size-1 animate-pulse rounded-full bg-blue-500" />
            {t("live")}
          </span>
        )}
      </div>
      <ScrollArea type="auto" className="min-h-0 min-w-0 flex-1 [&_[data-slot=scroll-area-viewport]>div]:block!">
        <div ref={contentRef} className="min-w-0 max-w-full px-4 py-3">
          {loadError && (
            <BlueError message={loadError} retry={() => window.location.reload()} retryLabel={copy.refresh} />
          )}
          {!loaded && <p className="text-center text-sm text-muted-foreground">{copy.loading}</p>}
          {hasMore && (
            <div className="pb-2 text-center">
              <Button size="sm" variant="ghost" onClick={() => void loadEarlier()} disabled={loadingMore}>
                {copy.loadEarlier}
              </Button>
            </div>
          )}
          {loaded && messages.length === 0 && !loadError && (
            <p className="py-10 text-center text-sm text-muted-foreground">{copy.emptyHint}</p>
          )}
          <Transcript activity={messages} live={running} chat fetchDetail={fetchDetail} />
        </div>
      </ScrollArea>
      <Composer
        value={input}
        onChange={setInput}
        onSend={() => void send()}
        onStop={() => void stop()}
        busy={busy}
        running={running}
        evidenceSlot={
          <CaseEvidencePicker selected={attachedCase} onChange={setAttachedCase} disabled={busy || running} />
        }
      />
      <ModelPicker
        profiles={profiles}
        selected={conversation.llm_profile_id ?? null}
        onChange={(id) => void changeProfile(id)}
        disabled={profileBusy || running}
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
  const [attachedCase, setAttachedCase] = React.useState<CaseSelection | null>(null);
  const [profileID, setProfileID] = React.useState<number | null>(null);
  const [busy, setBusy] = React.useState(false);
  async function send() {
    const message = input.trim();
    if (!message || busy) return;
    setBusy(true);
    try {
      const conversation = await blueChatApi.createConversation("", profileID);
      await blueChatApi.send(conversation.id, message, attachedCase?.id);
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
      <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-2 px-4 text-center">
        <div className="flex size-12 items-center justify-center rounded-full bg-primary/10">
          <Bot className="size-6 text-primary" />
        </div>
        <h2 className="text-sm font-medium">{copy.emptyTitle}</h2>
        <p className="max-w-md text-xs text-muted-foreground">{copy.emptyHint}</p>
        <p className="max-w-lg text-xs text-muted-foreground">{copy.scope}</p>
      </div>
      <Composer
        value={input}
        onChange={setInput}
        onSend={() => void send()}
        busy={busy}
        running={false}
        evidenceSlot={<CaseEvidencePicker selected={attachedCase} onChange={setAttachedCase} disabled={busy} />}
        leftSlot={
          <div className="flex h-9 w-full items-center gap-2 rounded-md border bg-background px-3 text-sm sm:w-40">
            <Bot className="size-3.5" />
            <span className="truncate">{copy.analyst}</span>
          </div>
        }
      />
      <ModelPicker profiles={profiles} selected={profileID} onChange={setProfileID} disabled={busy} />
    </div>
  );
}

function ConversationItem({
  conversation,
  active,
  dateLocale,
  selectionMode,
  selectedForDelete,
  onSelect,
  onRename,
  onPin,
  onDelete,
  onSelectedForDeleteChange,
}: {
  conversation: Conversation;
  active: boolean;
  dateLocale: string;
  selectionMode: boolean;
  selectedForDelete: boolean;
  onSelect: (id: number) => void;
  onRename: (conversation: Conversation, title: string) => Promise<void>;
  onPin: (conversation: Conversation) => void;
  onDelete: (conversation: Conversation) => void;
  onSelectedForDeleteChange: (id: number, checked: boolean) => void;
}) {
  const copy = useBlueChatCopy();
  const t = useTranslations("chat");
  const [renaming, setRenaming] = React.useState(false);
  const [renameText, setRenameText] = React.useState("");
  const cancelRenameRef = React.useRef(false);
  const renameInputRef = React.useRef<HTMLInputElement>(null);

  React.useEffect(() => {
    if (!renaming) return;
    renameInputRef.current?.focus();
    renameInputRef.current?.select();
  }, [renaming]);

  function startRename() {
    cancelRenameRef.current = false;
    setRenameText(conversation.title);
    setRenaming(true);
  }

  function commitRename() {
    if (cancelRenameRef.current) return;
    const title = renameText.trim();
    setRenaming(false);
    if (title && title !== conversation.title) void onRename(conversation, title);
  }

  return (
    <div
      className={cn(
        "group flex min-w-0 items-center gap-1 rounded-md pr-1 transition-colors",
        active ? "bg-accent text-accent-foreground" : "hover:bg-accent/50",
      )}
    >
      {selectionMode && (
        <Checkbox
          checked={selectedForDelete}
          onCheckedChange={(checked) => onSelectedForDeleteChange(conversation.id, checked === true)}
          aria-label={t("item.selectAria", { title: conversation.title || copy.newConversation })}
          className="ml-1 shrink-0"
        />
      )}
      {renaming ? (
        <input
          ref={renameInputRef}
          aria-label={copy.renameTitle}
          value={renameText}
          onChange={(event) => setRenameText(event.target.value)}
          onBlur={commitRename}
          onKeyDown={(event) => {
            if (event.key === "Enter") event.currentTarget.blur();
            if (event.key === "Escape") {
              event.preventDefault();
              cancelRenameRef.current = true;
              setRenaming(false);
            }
          }}
          maxLength={240}
          className="min-w-0 flex-1 rounded-md border border-input bg-background px-2 py-1 text-sm"
        />
      ) : (
        <button
          type="button"
          onClick={() => onSelect(conversation.id)}
          onDoubleClick={startRename}
          title={t("item.renameHint")}
          className="min-w-0 flex-1 rounded-md px-2 py-1.5 text-left"
          aria-current={active ? "page" : undefined}
        >
          <div className="flex min-w-0 items-center gap-1.5">
            {pinned(conversation) && <Pin className="size-3 shrink-0 text-primary" aria-label={t("item.pinnedAria")} />}
            <span className="truncate text-sm">{conversation.title || copy.newConversation}</span>
            {conversation.running && (
              <Badge variant="secondary" className="shrink-0 gap-1" title={t("item.runningTooltip")}>
                <Spinner className="size-3" aria-hidden="true" />
                {t("running")}
              </Badge>
            )}
          </div>
          <div className="flex min-w-0 items-center gap-1 text-[11px] text-muted-foreground">
            <span className="shrink-0">
              {new Date(conversation.created_at).toLocaleDateString(dateLocale, {
                month: "numeric",
                day: "numeric",
                hour: "2-digit",
                minute: "2-digit",
              })}
            </span>
            <span className="shrink-0 opacity-60">#{conversation.id}</span>
          </div>
        </button>
      )}
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            variant="ghost"
            size="icon-sm"
            className="shrink-0 text-muted-foreground"
            aria-label={t("item.manageAria", { title: conversation.title || copy.newConversation })}
          >
            <MoreHorizontal />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuItem onSelect={startRename}>
            <Pencil /> {copy.rename}
          </DropdownMenuItem>
          <DropdownMenuItem onSelect={() => onPin(conversation)}>
            {pinned(conversation) ? <PinOff /> : <Pin />}
            {pinned(conversation) ? copy.unpin : copy.pin}
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem variant="destructive" onSelect={() => onDelete(conversation)}>
            <Trash2 /> {copy.delete}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}

export default function BlueChatPage() {
  const copy = useBlueChatCopy();
  const t = useTranslations("chat");
  const locale = useActiveLocale();
  const [conversations, setConversations] = React.useState<Conversation[]>([]);
  const [profiles, setProfiles] = React.useState<LLMProfile[]>([]);
  const [selectedID, setSelectedID] = React.useState<number | null>(null);
  const [loaded, setLoaded] = React.useState(false);
  const [error, setError] = React.useState("");
  const [search, setSearch] = React.useState("");
  const [deleting, setDeleting] = React.useState<Conversation | null>(null);
  const [actionBusy, setActionBusy] = React.useState(false);
  const [selectionMode, setSelectionMode] = React.useState(false);
  const [selectedConversationIds, setSelectedConversationIds] = React.useState<Set<number>>(() => new Set());
  const [bulkDeleteOpen, setBulkDeleteOpen] = React.useState(false);
  const [bulkDeleting, setBulkDeleting] = React.useState(false);
  const [visibleConversationCount, setVisibleConversationCount] = React.useState(CONVERSATION_LIST_PAGE);
  const [groupCollapsed, setGroupCollapsed] = React.useState(false);
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
  const filtered = conversations.filter(
    (conversation) => !query || `${conversation.title} ${conversation.id}`.toLocaleLowerCase().includes(query),
  );
  const visible = filtered.slice(0, visibleConversationCount);
  const pinnedConversations = visible.filter(pinned);
  const recentConversations = visible.filter((conversation) => !pinned(conversation));
  const selectedConversationCount = filtered.filter((conversation) =>
    selectedConversationIds.has(conversation.id),
  ).length;
  const allConversationsSelected = filtered.length > 0 && selectedConversationCount === filtered.length;
  const conversationHeaderChecked: boolean | "indeterminate" = allConversationsSelected
    ? true
    : selectedConversationCount > 0
      ? "indeterminate"
      : false;
  const dateLocale = { ko: "ko-KR", zh: "zh-CN", es: "es-ES", en: "en-US" }[locale];

  React.useEffect(() => {
    const live = new Set(conversations.map((conversation) => conversation.id));
    setSelectedConversationIds((current) => {
      if (current.size === 0) return current;
      const next = new Set([...current].filter((id) => live.has(id)));
      return next.size === current.size ? current : next;
    });
  }, [conversations]);

  function toggleConversationSelected(id: number, checked: boolean) {
    setSelectedConversationIds((current) => {
      const next = new Set(current);
      if (checked) next.add(id);
      else next.delete(id);
      return next;
    });
  }

  function exitSelectionMode() {
    setSelectionMode(false);
    setSelectedConversationIds(new Set());
  }

  async function deleteSelectedConversations() {
    const ids = filtered
      .filter((conversation) => selectedConversationIds.has(conversation.id))
      .map((conversation) => conversation.id);
    if (ids.length === 0 || bulkDeleting) return;
    setBulkDeleting(true);
    const deleted = new Set<number>();
    const failed: number[] = [];
    try {
      for (const id of ids) {
        try {
          await blueChatApi.deleteConversation(id);
          deleted.add(id);
        } catch {
          failed.push(id);
        }
      }
      if (selectedID != null && deleted.has(selectedID)) setSelectedID(null);
      setSelectedConversationIds(new Set(failed));
      if (deleted.size > 0) toast.success(t("bulkDeleted", { count: deleted.size }));
      if (failed.length > 0)
        toast.error(
          t("bulkDeleteFailed", {
            count: failed.length,
            details: failed
              .slice(0, 3)
              .map((id) => `#${id}`)
              .join(", "),
          }),
        );
      else setSelectionMode(false);
      setBulkDeleteOpen(false);
      await reload();
    } finally {
      setBulkDeleting(false);
    }
  }

  async function renameConversation(conversation: Conversation, title: string) {
    try {
      await blueChatApi.updateConversation(conversation.id, { title });
      await reload();
    } catch (cause) {
      toast.error(cause instanceof Error ? cause.message : copy.saveFailed);
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
    <div
      data-content-padding="false"
      className="flex h-[calc(100svh-3rem)] min-w-0 flex-col overflow-hidden p-3 sm:p-4 md:h-[calc(100svh-4rem)] md:p-6"
    >
      {error && (
        <div className="mb-3">
          <BlueError message={error} retry={() => void reload()} retryLabel={copy.refresh} />
        </div>
      )}
      <div className="grid min-h-0 min-w-0 flex-1 grid-cols-1 grid-rows-[minmax(10rem,15rem)_minmax(0,1fr)] gap-3 md:grid-cols-[18rem_minmax(0,1fr)] md:grid-rows-[minmax(0,1fr)] md:gap-4">
        <aside className="flex min-h-0 min-w-0 flex-col overflow-hidden rounded-lg border bg-card">
          <div className="flex flex-col gap-2 border-b p-2">
            <Button size="sm" className="w-full" onClick={() => setSelectedID(null)}>
              <Plus /> {copy.newConversation}
            </Button>
            <div className="relative">
              <Search className="absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
              <Input
                className="h-8 pl-8 text-xs"
                placeholder={copy.search}
                aria-label={copy.search}
                value={search}
                onChange={(event) => {
                  setSearch(event.target.value);
                  setVisibleConversationCount(CONVERSATION_LIST_PAGE);
                  setSelectedConversationIds(new Set());
                  setBulkDeleteOpen(false);
                }}
              />
            </div>
            {conversations.length > 0 &&
              (selectionMode ? (
                <div className="flex items-center gap-2 px-1">
                  <Checkbox
                    checked={conversationHeaderChecked}
                    onCheckedChange={(checked) =>
                      setSelectedConversationIds(
                        checked === true ? new Set(filtered.map((conversation) => conversation.id)) : new Set(),
                      )
                    }
                    aria-label={t("selectAllAria")}
                    disabled={filtered.length === 0 || bulkDeleting}
                  />
                  <span className="min-w-0 flex-1 text-xs tabular-nums text-muted-foreground">
                    {selectedConversationCount > 0
                      ? t("selectedCount", { count: selectedConversationCount })
                      : query
                        ? t("filteredCount", { shown: filtered.length, total: conversations.length })
                        : t("totalCount", { count: conversations.length })}
                  </span>
                  {selectedConversationCount > 0 && (
                    <Button
                      size="sm"
                      variant="destructive"
                      disabled={bulkDeleting}
                      onClick={() => setBulkDeleteOpen(true)}
                    >
                      <Trash2 data-icon="inline-start" /> {copy.delete}
                    </Button>
                  )}
                  <Button size="sm" variant="ghost" onClick={exitSelectionMode}>
                    {t("done")}
                  </Button>
                </div>
              ) : (
                <div className="flex items-center gap-2 px-1">
                  <span className="min-w-0 flex-1 text-xs tabular-nums text-muted-foreground">
                    {query
                      ? t("filteredCount", { shown: filtered.length, total: conversations.length })
                      : t("totalCount", { count: conversations.length })}
                  </span>
                  <Button
                    size="sm"
                    variant="ghost"
                    className="text-muted-foreground"
                    onClick={() => setSelectionMode(true)}
                    disabled={filtered.length === 0}
                  >
                    <ListChecks data-icon="inline-start" /> {t("multiSelect")}
                  </Button>
                </div>
              ))}
          </div>
          <ScrollArea type="auto" className="min-h-0 min-w-0 flex-1 [&_[data-slot=scroll-area-viewport]>div]:block!">
            <div className="flex min-w-0 flex-col gap-0.5 p-2">
              {!loaded && <p className="px-2 py-5 text-center text-xs text-muted-foreground">{copy.loading}</p>}
              {loaded && conversations.length === 0 && (
                <p className="px-2 py-5 text-center text-xs text-muted-foreground">{copy.noConversations}</p>
              )}
              {loaded && conversations.length > 0 && filtered.length === 0 && (
                <p className="px-2 py-5 text-center text-xs text-muted-foreground">{copy.noMatches}</p>
              )}
              {pinnedConversations.map((conversation) => (
                <ConversationItem
                  key={conversation.id}
                  conversation={conversation}
                  active={selectedID === conversation.id}
                  dateLocale={dateLocale}
                  selectionMode={selectionMode}
                  selectedForDelete={selectedConversationIds.has(conversation.id)}
                  onSelect={setSelectedID}
                  onRename={renameConversation}
                  onPin={(item) => void togglePin(item)}
                  onDelete={setDeleting}
                  onSelectedForDeleteChange={toggleConversationSelected}
                />
              ))}
              {recentConversations.length > 0 && (
                <button
                  type="button"
                  onClick={() => setGroupCollapsed((current) => !current)}
                  aria-expanded={!groupCollapsed}
                  className="sticky top-0 z-10 flex min-w-0 items-center gap-1.5 rounded-md bg-card px-1.5 py-1 text-left font-medium text-[11px] text-muted-foreground transition-colors hover:bg-accent/50"
                >
                  <ChevronRight
                    className={cn("size-3 shrink-0 transition-transform", !groupCollapsed && "rotate-90")}
                  />
                  <Bot className="size-3 shrink-0" />
                  <span className="min-w-0 flex-1 truncate">{copy.analyst}</span>
                  {recentConversations.some((conversation) => conversation.running) && (
                    <Spinner className="size-3 shrink-0" />
                  )}
                  <span className="shrink-0 tabular-nums opacity-60">{recentConversations.length}</span>
                </button>
              )}
              {!groupCollapsed &&
                recentConversations.map((conversation) => (
                  <ConversationItem
                    key={conversation.id}
                    conversation={conversation}
                    active={selectedID === conversation.id}
                    dateLocale={dateLocale}
                    selectionMode={selectionMode}
                    selectedForDelete={selectedConversationIds.has(conversation.id)}
                    onSelect={setSelectedID}
                    onRename={renameConversation}
                    onPin={(item) => void togglePin(item)}
                    onDelete={setDeleting}
                    onSelectedForDeleteChange={toggleConversationSelected}
                  />
                ))}
              {visible.length < filtered.length && (
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  className="mt-1 w-full"
                  onClick={() => setVisibleConversationCount((count) => count + CONVERSATION_LIST_PAGE)}
                >
                  {t("loadMore")}
                </Button>
              )}
            </div>
          </ScrollArea>
        </aside>
        <div className="flex min-w-0 flex-col overflow-hidden rounded-lg border bg-card">
          {selected ? (
            <ConversationView key={selected.id} conversation={selected} profiles={profiles} onUpdated={reload} />
          ) : selectedID != null ? (
            <div role="status" className="p-6 text-sm text-muted-foreground">
              {loaded ? t("deletedConversation") : t("loadingConversation")}
            </div>
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
        </div>
      </div>

      <AlertDialog open={bulkDeleteOpen} onOpenChange={setBulkDeleteOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("bulkDeleteTitle", { count: selectedConversationCount })}</AlertDialogTitle>
            <AlertDialogDescription>{copy.deleteDescription}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={bulkDeleting}>{copy.cancel}</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              disabled={bulkDeleting || selectedConversationCount === 0}
              onClick={(event) => {
                event.preventDefault();
                void deleteSelectedConversations();
              }}
            >
              {bulkDeleting && <Loader2 data-icon="inline-start" className="animate-spin" />}
              {bulkDeleting ? t("deleting") : t("confirmDelete")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

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

"use client";

import * as React from "react";

import Link from "next/link";

import { ChevronLeft, ChevronRight, RefreshCw, Search } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { useActiveLocale } from "@/i18n/locale-store";
import { blueRequest } from "@/lib/blue-api";
import { useBlueLLMRecordsCopy } from "@/lib/blue-llm-records-copy";
import type { LLMRecordDetail, LLMRecordItem } from "@/lib/types";

import { BlueError, BlueLoading, BluePageHeader } from "../_components/blue-ui";

const PAGE_SIZE = 50;

type RecordsPage = { records: LLMRecordItem[]; total: number; recording_enabled: boolean };

function contextFor(session: string): { kind: "case" | "chat"; id: number } | null {
  const analysis = /^blue-case-(\d+)-analysis-\d+$/.exec(session);
  if (analysis) return { kind: "case", id: Number(analysis[1]) };
  const chat = /^blue-chat-(\d+)(?:-|$)/.exec(session);
  if (chat) return { kind: "chat", id: Number(chat[1]) };
  return null;
}

function pretty(value: string | undefined): string {
  if (!value) return "";
  try {
    return JSON.stringify(JSON.parse(value), null, 2);
  } catch {
    return value;
  }
}

export default function BlueLLMRecordsPage() {
  const copy = useBlueLLMRecordsCopy();
  const locale = useActiveLocale();
  const [sessionInput, setSessionInput] = React.useState("");
  const [modelInput, setModelInput] = React.useState("");
  const [session, setSession] = React.useState("");
  const [model, setModel] = React.useState("");
  const [page, setPage] = React.useState(0);
  const [reload, setReload] = React.useState(0);
  const [records, setRecords] = React.useState<LLMRecordItem[]>([]);
  const [total, setTotal] = React.useState(0);
  const [recordingEnabled, setRecordingEnabled] = React.useState<boolean | null>(null);
  const [loading, setLoading] = React.useState(true);
  const [error, setError] = React.useState("");
  const [selected, setSelected] = React.useState<LLMRecordItem | null>(null);
  const [detail, setDetail] = React.useState<LLMRecordDetail | null>(null);
  const [detailLoading, setDetailLoading] = React.useState(false);
  const [detailError, setDetailError] = React.useState("");
  const [raw, setRaw] = React.useState(false);

  // biome-ignore lint/correctness/useExhaustiveDependencies: reload is a manual refresh trigger.
  React.useEffect(() => {
    let active = true;
    const query = new URLSearchParams({ page: String(page), size: String(PAGE_SIZE) });
    if (session) query.set("session", session);
    if (model) query.set("model", model);
    setLoading(true);
    setError("");
    blueRequest<RecordsPage>(`/llm/records?${query}`)
      .then((result) => {
        if (!active) return;
        if (page > 0 && result.total <= page * PAGE_SIZE) {
          setPage(Math.max(0, Math.ceil(result.total / PAGE_SIZE) - 1));
          return;
        }
        setRecords(result.records ?? []);
        setTotal(result.total ?? 0);
        setRecordingEnabled(result.recording_enabled);
      })
      .catch((cause: unknown) => {
        if (active) setError(cause instanceof Error ? cause.message : copy.loadFailed);
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [copy.loadFailed, model, page, reload, session]);

  React.useEffect(() => {
    if (!selected) {
      setDetail(null);
      return;
    }
    let active = true;
    setDetail(null);
    setDetailError("");
    setDetailLoading(true);
    setRaw(false);
    blueRequest<LLMRecordDetail>(`/llm/records/${selected.id}`)
      .then((result) => {
        if (active) setDetail(result);
      })
      .catch((cause: unknown) => {
        if (active) setDetailError(cause instanceof Error ? cause.message : copy.detailFailed);
      })
      .finally(() => {
        if (active) setDetailLoading(false);
      });
    return () => {
      active = false;
    };
  }, [copy.detailFailed, selected]);

  const pages = Math.max(1, Math.ceil(total / PAGE_SIZE));
  const hasRaw = !!(detail?.raw_request || detail?.raw_response);
  const showRaw = raw && hasRaw;
  const request = showRaw ? detail?.raw_request : detail?.request_body;
  const response = showRaw ? detail?.raw_response : detail?.response_body;

  function applyFilters(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setSelected(null);
    setPage(0);
    setSession(sessionInput.trim());
    setModel(modelInput.trim());
  }

  return (
    <div className="space-y-5 pb-6">
      <BluePageHeader
        title={copy.title}
        description={copy.subtitle}
        actions={
          <Button variant="outline" size="sm" disabled={loading} onClick={() => setReload((value) => value + 1)}>
            <RefreshCw aria-hidden="true" className={loading ? "animate-spin" : ""} />
            {copy.refresh}
          </Button>
        }
      />

      <div className="flex flex-wrap items-center gap-2 rounded-lg border bg-muted/20 px-3 py-2 text-sm">
        {recordingEnabled !== null && (
          <Badge variant={recordingEnabled ? "secondary" : "outline"}>
            {recordingEnabled ? copy.recordingOn : copy.recordingOff}
          </Badge>
        )}
        <span className="text-muted-foreground">
          {copy.globalRecording} {copy.recordingHint}
        </span>
        <Link href="/system/settings" className="font-medium underline-offset-4 hover:underline">
          {copy.settings}
        </Link>
      </div>
      <p className="text-muted-foreground text-xs">{copy.legacyHint}</p>

      <form className="flex flex-wrap gap-2" onSubmit={applyFilters}>
        <div className="relative min-w-48 flex-1">
          <Search
            aria-hidden="true"
            className="absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground"
          />
          <Input
            aria-label={copy.search}
            placeholder={copy.search}
            value={sessionInput}
            onChange={(event) => setSessionInput(event.target.value)}
            className="pl-9"
          />
        </div>
        <Input
          aria-label={copy.model}
          placeholder={copy.model}
          value={modelInput}
          onChange={(event) => setModelInput(event.target.value)}
          className="w-48"
        />
        <Button type="submit" variant="outline">
          {copy.apply}
        </Button>
      </form>

      {error && <BlueError message={error} />}
      <Card className="overflow-hidden py-0">
        <div className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{copy.time}</TableHead>
                <TableHead>{copy.context}</TableHead>
                <TableHead>{copy.profile}</TableHead>
                <TableHead>{copy.model}</TableHead>
                <TableHead>{copy.latency}</TableHead>
                <TableHead>{copy.tokens}</TableHead>
                <TableHead>{copy.status}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {loading && records.length === 0 && (
                <TableRow>
                  <TableCell colSpan={7}>
                    <BlueLoading label={copy.loading} />
                  </TableCell>
                </TableRow>
              )}
              {!loading && records.length === 0 && (
                <TableRow>
                  <TableCell colSpan={7} className="py-12 text-center text-muted-foreground text-sm">
                    {copy.empty}
                  </TableCell>
                </TableRow>
              )}
              {records.map((record) => {
                const context = contextFor(record.session_id);
                let href: string | null = null;
                if (context?.kind === "case") href = `/blue/cases/detail?id=${context.id}`;
                if (context?.kind === "chat") href = `/blue/chat?c=${context.id}`;
                const label = context?.kind === "case" ? copy.caseAnalysis : copy.conversation;
                return (
                  <TableRow
                    key={record.id}
                    className="cursor-pointer hover:bg-muted/40"
                    onClick={() => setSelected(record)}
                  >
                    <TableCell className="whitespace-nowrap text-xs">
                      {new Date(record.ts).toLocaleString(locale)}
                    </TableCell>
                    <TableCell className="text-xs">
                      {href && context ? (
                        <Link
                          href={href}
                          className="font-medium hover:underline"
                          onClick={(event) => event.stopPropagation()}
                        >
                          {label} #{context.id}
                        </Link>
                      ) : (
                        <span title={record.session_id}>{copy.session}</span>
                      )}
                    </TableCell>
                    <TableCell className="text-xs">{record.profile_name || "—"}</TableCell>
                    <TableCell className="font-mono text-xs">{record.model || "—"}</TableCell>
                    <TableCell className="text-xs tabular-nums">{record.latency_ms} ms</TableCell>
                    <TableCell className="text-xs tabular-nums">
                      {record.input_tokens} / {record.output_tokens}
                    </TableCell>
                    <TableCell>
                      <Badge variant={record.status === "ok" ? "secondary" : "destructive"}>
                        {record.status || copy.error}
                      </Badge>
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </div>
      </Card>

      <div className="flex items-center justify-between gap-2 text-muted-foreground text-xs">
        <span>
          {copy.recordCount} {total === 0 ? 0 : page * PAGE_SIZE + 1}–{Math.min(total, (page + 1) * PAGE_SIZE)} /{" "}
          {total}
        </span>
        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            disabled={page === 0 || loading}
            onClick={() => {
              setSelected(null);
              setPage((value) => value - 1);
            }}
          >
            <ChevronLeft aria-hidden="true" />
            {copy.previous}
          </Button>
          <span>
            {copy.page} {page + 1} / {pages}
          </span>
          <Button
            variant="outline"
            size="sm"
            disabled={page + 1 >= pages || loading}
            onClick={() => {
              setSelected(null);
              setPage((value) => value + 1);
            }}
          >
            {copy.next}
            <ChevronRight aria-hidden="true" />
          </Button>
        </div>
      </div>

      <Sheet
        open={selected !== null}
        onOpenChange={(open) => {
          if (!open) setSelected(null);
        }}
      >
        <SheetContent className="w-full overflow-y-auto sm:max-w-2xl">
          <SheetHeader>
            <SheetTitle>
              {copy.detail} {selected ? `#${selected.id}` : ""}
            </SheetTitle>
            <SheetDescription>{selected?.session_id}</SheetDescription>
          </SheetHeader>
          <div className="space-y-4 px-4 pb-6">
            {detailLoading && <BlueLoading label={copy.loading} />}
            {detailError && <BlueError message={detailError} />}
            {detail && (
              <>
                <div className="flex gap-2">
                  <Button size="sm" variant={showRaw ? "outline" : "secondary"} onClick={() => setRaw(false)}>
                    {copy.normalized}
                  </Button>
                  <Button
                    size="sm"
                    variant={showRaw ? "secondary" : "outline"}
                    disabled={!hasRaw}
                    onClick={() => setRaw(true)}
                  >
                    {copy.raw}
                  </Button>
                </div>
                {!hasRaw && <p className="text-muted-foreground text-xs">{copy.rawUnavailable}</p>}
                <section className="space-y-1">
                  <h3 className="font-medium text-sm">{copy.request}</h3>
                  <pre className="max-h-72 overflow-auto whitespace-pre-wrap break-all rounded-md border bg-muted/30 p-3 text-xs">
                    {pretty(request) || "—"}
                  </pre>
                </section>
                <section className="space-y-1">
                  <h3 className="font-medium text-sm">{copy.response}</h3>
                  <pre className="max-h-72 overflow-auto whitespace-pre-wrap break-all rounded-md border bg-muted/30 p-3 text-xs">
                    {pretty(response) || detail.error || "—"}
                  </pre>
                </section>
              </>
            )}
          </div>
        </SheetContent>
      </Sheet>
    </div>
  );
}

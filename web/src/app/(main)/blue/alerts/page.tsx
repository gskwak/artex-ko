"use client";

import * as React from "react";

import Link from "next/link";

import { ArrowUpRight, RefreshCw, Save } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";
import type { Locale } from "@/i18n/config";
import { useActiveLocale } from "@/i18n/locale-store";
import {
  type BlueAlert,
  type BlueAlertStatus,
  type BlueCase,
  type BlueSeverity,
  blueApi,
  blueRequest,
} from "@/lib/blue-api";
import { blueAlertStatusLabel, blueCaseStatusLabel, blueSeverityLabel, useBlueCopy } from "@/lib/blue-copy";

import {
  BlueAlertStatusBadge,
  BlueEmpty,
  BlueError,
  BlueLoading,
  BluePageHeader,
  BlueSeverityBadge,
  blueDate,
} from "../_components/blue-ui";

const STATUSES: BlueAlertStatus[] = ["new", "investigating", "confirmed", "false_positive", "dismissed"];
const SEVERITIES: BlueSeverity[] = ["critical", "high", "medium", "low", "info"];
const PAGE_SIZE = 100;
const SELECT_CLASS =
  "h-9 min-w-0 rounded-md border border-input bg-background px-2.5 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring";
const CASE_HELP: Record<Locale, { empty: string; invalid: string; moving: string }> = {
  en: {
    empty: "Enter any case ID, including older cases. Leave empty to unassign this alert.",
    invalid: "Enter a valid positive case ID.",
    moving: "Moving this event to another case also moves all of its alerts.",
  },
  ko: {
    empty: "오래된 사건도 ID로 입력할 수 있습니다. 비워 두면 이 경보의 배정을 해제합니다.",
    invalid: "올바른 양의 정수 사건 ID를 입력하세요.",
    moving: "다른 사건으로 옮기면 이 이벤트와 연결된 모든 경보도 함께 이동합니다.",
  },
  zh: {
    empty: "可输入任意调查事件 ID，包括较早的事件。留空将取消此告警的关联。",
    invalid: "请输入有效的正整数调查事件 ID。",
    moving: "将此事件移至另一调查事件时，其所有告警也会一同移动。",
  },
  es: {
    empty: "Introduce cualquier ID de caso, incluso uno antiguo. Déjalo vacío para desvincular esta alerta.",
    invalid: "Introduce un ID de caso entero positivo válido.",
    moving: "Al mover este evento a otro caso, también se moverán todas sus alertas.",
  },
};

export default function BlueAlertsPage() {
  const copy = useBlueCopy();
  const locale = useActiveLocale();
  const [alerts, setAlerts] = React.useState<BlueAlert[]>([]);
  const [cases, setCases] = React.useState<BlueCase[]>([]);
  const [statusFilter, setStatusFilter] = React.useState<BlueAlertStatus | "all">("all");
  const [severityFilter, setSeverityFilter] = React.useState<BlueSeverity | "all">("all");
  const [search, setSearch] = React.useState("");
  const [selectedID, setSelectedID] = React.useState<number | null>(null);
  const [draftStatus, setDraftStatus] = React.useState<BlueAlertStatus>("new");
  const [draftReason, setDraftReason] = React.useState("");
  const [draftCaseID, setDraftCaseID] = React.useState("");
  const [casePreview, setCasePreview] = React.useState<BlueCase | null>(null);
  const [caseChecking, setCaseChecking] = React.useState(false);
  const [caseError, setCaseError] = React.useState("");
  const [loading, setLoading] = React.useState(true);
  const [loadingMore, setLoadingMore] = React.useState(false);
  const [hasMore, setHasMore] = React.useState(false);
  const [nextOffset, setNextOffset] = React.useState(0);
  const [saving, setSaving] = React.useState(false);
  const [error, setError] = React.useState("");

  const load = React.useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const [nextAlerts, nextCases] = await Promise.all([
        blueApi.alerts({
          status: statusFilter === "all" ? undefined : statusFilter,
          severity: severityFilter === "all" ? undefined : severityFilter,
          limit: PAGE_SIZE,
        }),
        blueApi.cases({ limit: 100 }),
      ]);
      setHasMore(nextAlerts.length === PAGE_SIZE);
      setNextOffset(nextAlerts.length);
      setCases(nextCases);
      const linkedID = Number(new URLSearchParams(window.location.search).get("alert"));
      if (Number.isSafeInteger(linkedID) && linkedID > 0 && !nextAlerts.some((item) => item.id === linkedID)) {
        try {
          nextAlerts.unshift(await blueApi.getAlert(linkedID));
        } catch (cause) {
          setError(cause instanceof Error ? cause.message : copy.loadFailed);
        }
      }
      setAlerts(nextAlerts);
      setSelectedID((current) => {
        if (current && nextAlerts.some((item) => item.id === current)) return current;
        if (linkedID && nextAlerts.some((item) => item.id === linkedID)) return linkedID;
        return nextAlerts[0]?.id ?? null;
      });
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : copy.loadFailed);
    } finally {
      setLoading(false);
    }
  }, [copy.loadFailed, severityFilter, statusFilter]);

  React.useEffect(() => {
    void load();
  }, [load]);

  const selected = alerts.find((item) => item.id === selectedID) ?? null;
  React.useEffect(() => {
    if (!selected) return;
    setDraftStatus(selected.status);
    setDraftReason(selected.reason ?? "");
    setDraftCaseID(selected.case_id ? String(selected.case_id) : "");
  }, [selected]);

  React.useEffect(() => {
    setCasePreview(null);
    setCaseError("");
    setCaseChecking(false);
    const value = draftCaseID.trim();
    if (!value) return;
    const id = Number(value);
    if (!Number.isSafeInteger(id) || id <= 0) {
      setCaseError(CASE_HELP[locale].invalid);
      return;
    }
    let cancelled = false;
    setCaseChecking(true);
    const timer = window.setTimeout(() => {
      void blueApi
        .getCase(id)
        .then((item) => {
          if (!cancelled) setCasePreview(item);
        })
        .catch((cause) => {
          if (!cancelled) setCaseError(cause instanceof Error ? cause.message : copy.loadFailed);
        })
        .finally(() => {
          if (!cancelled) setCaseChecking(false);
        });
    }, 300);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [copy.loadFailed, draftCaseID, locale]);

  const filtered = alerts.filter((item) => {
    const term = search.trim().toLowerCase();
    if (!term) return true;
    return (
      String(item.id).includes(term) || item.title.toLowerCase().includes(term) || String(item.event_id).includes(term)
    );
  });

  async function loadMore() {
    if (loadingMore || !hasMore) return;
    setLoadingMore(true);
    try {
      const query = new URLSearchParams({ limit: String(PAGE_SIZE), offset: String(nextOffset) });
      if (statusFilter !== "all") query.set("status", statusFilter);
      if (severityFilter !== "all") query.set("severity", severityFilter);
      const next = await blueRequest<BlueAlert[]>(`/alerts?${query.toString()}`);
      setAlerts((current) => {
        const known = new Set(current.map((item) => item.id));
        return [...current, ...next.filter((item) => !known.has(item.id))];
      });
      setNextOffset(nextOffset + next.length);
      setHasMore(next.length === PAGE_SIZE);
    } catch (cause) {
      toast.error(cause instanceof Error ? cause.message : copy.loadFailed);
    } finally {
      setLoadingMore(false);
    }
  }

  async function saveTriage() {
    if (!selected || saving) return;
    setSaving(true);
    let moved = false;
    try {
      const rawCaseID = draftCaseID.trim();
      const nextCaseID = rawCaseID ? Number(rawCaseID) : null;
      if (nextCaseID !== null) {
        if (!Number.isSafeInteger(nextCaseID) || nextCaseID <= 0) throw new Error(CASE_HELP[locale].invalid);
        await blueApi.getCase(nextCaseID);
      }
      const moving = Boolean(selected.case_id && nextCaseID && selected.case_id !== nextCaseID);
      if (moving && nextCaseID !== null) {
        await blueApi.moveEventCase(selected.event_id, nextCaseID);
        moved = true;
      }
      const updated = await blueApi.updateAlert(selected.id, {
        status: draftStatus,
        reason: draftReason.trim(),
        ...(moving ? {} : { case_id: nextCaseID }),
      });
      setAlerts((previous) =>
        previous.map((item) => {
          if (item.id === updated.id) return updated;
          if (moving && item.event_id === selected.event_id) return { ...item, case_id: nextCaseID };
          return item;
        }),
      );
      toast.success(copy.alertUpdated);
      if (statusFilter !== "all" && updated.status !== statusFilter) void load();
    } catch (cause) {
      toast.error(cause instanceof Error ? cause.message : copy.saveFailed);
      if (moved) void load();
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="space-y-5">
      <BluePageHeader
        title={copy.alerts}
        description={copy.alertsDescription}
        actions={
          <Button size="sm" variant="outline" onClick={() => void load()} disabled={loading}>
            <RefreshCw className={loading ? "animate-spin" : ""} />
            {copy.refresh}
          </Button>
        }
      />

      <div className="flex flex-wrap gap-2">
        <Input
          className="w-full sm:max-w-64"
          aria-label={copy.alerts}
          placeholder={`${copy.alertID} / ${copy.eventID} / ${copy.message}`}
          value={search}
          onChange={(event) => setSearch(event.target.value)}
        />
        <label className="sr-only" htmlFor="blue-alert-status-filter">
          {copy.filterStatus}
        </label>
        <select
          id="blue-alert-status-filter"
          className={SELECT_CLASS}
          value={statusFilter}
          onChange={(event) => setStatusFilter(event.target.value as BlueAlertStatus | "all")}
        >
          <option value="all">{copy.allStatuses}</option>
          {STATUSES.map((status) => (
            <option key={status} value={status}>
              {blueAlertStatusLabel(status, locale)}
            </option>
          ))}
        </select>
        <label className="sr-only" htmlFor="blue-alert-severity-filter">
          {copy.severity}
        </label>
        <select
          id="blue-alert-severity-filter"
          className={SELECT_CLASS}
          value={severityFilter}
          onChange={(event) => setSeverityFilter(event.target.value as BlueSeverity | "all")}
        >
          <option value="all">{copy.allSeverities}</option>
          {SEVERITIES.map((severity) => (
            <option key={severity} value={severity}>
              {blueSeverityLabel(severity, locale)}
            </option>
          ))}
        </select>
      </div>

      {error && <BlueError message={error} retry={() => void load()} retryLabel={copy.refresh} />}
      {loading && alerts.length === 0 ? <BlueLoading label={copy.loading} /> : null}
      {!loading && filtered.length === 0 ? <BlueEmpty message={copy.noAlerts} /> : null}

      {filtered.length > 0 && (
        <div className="grid min-w-0 gap-4 xl:grid-cols-[minmax(0,1.5fr)_minmax(310px,1fr)]">
          <Card className="min-w-0">
            <CardContent className="p-0">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{copy.alert}</TableHead>
                    <TableHead>{copy.severity}</TableHead>
                    <TableHead>{copy.status}</TableHead>
                    <TableHead>{copy.created}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {filtered.map((item) => (
                    <TableRow key={item.id} data-state={selectedID === item.id ? "selected" : undefined}>
                      <TableCell className="min-w-52 whitespace-normal">
                        <button
                          type="button"
                          onClick={() => setSelectedID(item.id)}
                          className="w-full text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                        >
                          <span className="block font-medium">
                            #{item.id} {item.title}
                          </span>
                          <span className="block text-xs text-muted-foreground">
                            {copy.rule} #{item.rule_id} · {copy.eventID}
                            {item.event_id}
                          </span>
                        </button>
                      </TableCell>
                      <TableCell>
                        <BlueSeverityBadge severity={item.severity} locale={locale} />
                      </TableCell>
                      <TableCell>
                        <BlueAlertStatusBadge status={item.status} locale={locale} />
                      </TableCell>
                      <TableCell className="text-xs text-muted-foreground">
                        {blueDate(item.created_at, locale)}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
              {hasMore && (
                <div className="border-t p-3 text-center">
                  <Button variant="outline" size="sm" onClick={() => void loadMore()} disabled={loadingMore}>
                    {loadingMore ? copy.loadingMore : copy.loadMore}
                  </Button>
                </div>
              )}
            </CardContent>
          </Card>

          {selected && (
            <Card className="h-fit min-w-0">
              <CardHeader className="space-y-2">
                <CardTitle className="text-base">
                  {copy.alertID}
                  {selected.id} · {selected.title}
                </CardTitle>
                <div className="flex flex-wrap items-center gap-2">
                  <BlueSeverityBadge severity={selected.severity} locale={locale} />
                  <BlueAlertStatusBadge status={selected.status} locale={locale} />
                </div>
              </CardHeader>
              <CardContent className="space-y-4 text-sm">
                <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2">
                  <dt className="text-muted-foreground">{copy.rule}</dt>
                  <dd>#{selected.rule_id}</dd>
                  <dt className="text-muted-foreground">{copy.event}</dt>
                  <dd>
                    <Link
                      className="inline-flex items-center gap-1 text-primary hover:underline"
                      href={`/blue/events?event=${selected.event_id}`}
                    >
                      #{selected.event_id}
                      <ArrowUpRight className="size-3" />
                    </Link>
                  </dd>
                  <dt className="text-muted-foreground">{copy.case}</dt>
                  <dd>
                    {selected.case_id ? (
                      <Link
                        className="inline-flex items-center gap-1 text-primary hover:underline"
                        href={`/blue/cases/detail?id=${selected.case_id}`}
                      >
                        #{selected.case_id}
                        <ArrowUpRight className="size-3" />
                      </Link>
                    ) : (
                      copy.noCase
                    )}
                  </dd>
                  <dt className="text-muted-foreground">{copy.created}</dt>
                  <dd>{blueDate(selected.created_at, locale)}</dd>
                </dl>
                <div className="space-y-1.5">
                  <label className="text-xs font-medium" htmlFor="blue-alert-status">
                    {copy.triageStatus}
                  </label>
                  <select
                    id="blue-alert-status"
                    className={`${SELECT_CLASS} w-full`}
                    value={draftStatus}
                    onChange={(event) => setDraftStatus(event.target.value as BlueAlertStatus)}
                  >
                    {STATUSES.map((status) => (
                      <option key={status} value={status}>
                        {blueAlertStatusLabel(status, locale)}
                      </option>
                    ))}
                  </select>
                </div>
                <div className="space-y-1.5">
                  <div className="flex items-center justify-between gap-2">
                    <label className="text-xs font-medium" htmlFor="blue-alert-case">
                      {copy.assignCase}
                    </label>
                    <Link href="/blue/cases" className="text-xs text-primary hover:underline">
                      {copy.createCase}
                    </Link>
                  </div>
                  <Input
                    id="blue-alert-case"
                    type="number"
                    min="1"
                    step="1"
                    list="blue-alert-case-suggestions"
                    value={draftCaseID}
                    onChange={(event) => setDraftCaseID(event.target.value)}
                    placeholder={copy.caseID}
                    aria-describedby="blue-alert-case-help"
                    aria-invalid={Boolean(caseError)}
                  />
                  <datalist id="blue-alert-case-suggestions">
                    {cases.map((item) => (
                      <option key={item.id} value={item.id} label={item.title} />
                    ))}
                  </datalist>
                  <p id="blue-alert-case-help" className="text-xs text-muted-foreground">
                    {CASE_HELP[locale].empty}
                  </p>
                  {caseChecking && <p className="text-xs text-muted-foreground">{copy.loading}</p>}
                  {caseError && (
                    <p className="text-xs text-destructive" role="alert">
                      {caseError}
                    </p>
                  )}
                  {casePreview && (
                    <p className="text-xs">
                      <Link className="text-primary hover:underline" href={`/blue/cases/detail?id=${casePreview.id}`}>
                        #{casePreview.id} {casePreview.title}
                      </Link>{" "}
                      · {blueCaseStatusLabel(casePreview.status, locale)}
                    </p>
                  )}
                  {selected.case_id && draftCaseID.trim() && Number(draftCaseID) !== selected.case_id && (
                    <p className="text-xs text-amber-700 dark:text-amber-400">{CASE_HELP[locale].moving}</p>
                  )}
                </div>
                <div className="space-y-1.5">
                  <label className="text-xs font-medium" htmlFor="blue-alert-reason">
                    {copy.reason}
                  </label>
                  <Textarea
                    id="blue-alert-reason"
                    rows={4}
                    value={draftReason}
                    onChange={(event) => setDraftReason(event.target.value)}
                  />
                </div>
                <Button className="w-full" onClick={() => void saveTriage()} disabled={saving}>
                  <Save />
                  {saving ? copy.loading : copy.saveTriage}
                </Button>
              </CardContent>
            </Card>
          )}
        </div>
      )}
    </div>
  );
}

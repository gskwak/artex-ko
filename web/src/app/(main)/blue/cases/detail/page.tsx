"use client";

import * as React from "react";

import Link from "next/link";
import { useSearchParams } from "next/navigation";

import { ArrowLeft, Bot, Download, ExternalLink, RefreshCw, Save } from "lucide-react";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Separator } from "@/components/ui/separator";
import { Textarea } from "@/components/ui/textarea";
import { useActiveLocale } from "@/i18n/locale-store";
import {
  type BlueAction,
  type BlueAlert,
  type BlueAnalysisRun,
  type BlueCase,
  type BlueCaseStatus,
  type BlueEvent,
  type BlueObservation,
  type BlueObservationKind,
  type BlueSeverity,
  blueApi,
} from "@/lib/blue-api";
import { blueCaseStatusLabel, blueObservationKindLabel, blueSeverityLabel, useBlueCopy } from "@/lib/blue-copy";
import { blueReportButtonCopy, downloadBlueCaseReport } from "@/lib/blue-report";

import {
  BlueAlertStatusBadge,
  BlueCaseStatusBadge,
  BlueEmpty,
  BlueError,
  BlueLoading,
  BluePageHeader,
  BlueSeverityBadge,
  blueDate,
} from "../../_components/blue-ui";

const CASE_STATUSES: BlueCaseStatus[] = ["open", "investigating", "contained", "resolved", "closed"];
const SEVERITIES: BlueSeverity[] = ["info", "low", "medium", "high", "critical"];
const OBSERVATION_KINDS: BlueObservationKind[] = ["fact", "hypothesis", "unknown", "recommendation"];
const CASE_EVIDENCE_PAGE = 500;
const OBSERVATION_PAGE = 100;
const ACTION_KINDS = [
  "isolate_host",
  "block_ip",
  "reset_credential",
  "rotate_secret",
  "patch_system",
  "custom",
] as const;
const ACTION_KIND_LABELS: Record<string, Record<(typeof ACTION_KINDS)[number], string>> = {
  en: {
    isolate_host: "Isolate host",
    block_ip: "Block IP",
    reset_credential: "Reset credential",
    rotate_secret: "Rotate secret",
    patch_system: "Patch system",
    custom: "Custom",
  },
  ko: {
    isolate_host: "호스트 격리",
    block_ip: "IP 차단",
    reset_credential: "자격 증명 재설정",
    rotate_secret: "비밀값 교체",
    patch_system: "시스템 패치",
    custom: "사용자 지정",
  },
  zh: {
    isolate_host: "隔离主机",
    block_ip: "封禁 IP",
    reset_credential: "重置凭据",
    rotate_secret: "轮换密钥",
    patch_system: "系统补丁",
    custom: "自定义",
  },
  es: {
    isolate_host: "Aislar equipo",
    block_ip: "Bloquear IP",
    reset_credential: "Restablecer credenciales",
    rotate_secret: "Rotar secreto",
    patch_system: "Aplicar parche",
    custom: "Personalizada",
  },
};

function actionKindLabel(kind: string, locale: string): string {
  const labels = ACTION_KIND_LABELS[locale] ?? ACTION_KIND_LABELS.en;
  return kind in labels ? labels[kind as (typeof ACTION_KINDS)[number]] : kind.replaceAll("_", " ");
}

type TimelineEntry =
  | { kind: "event"; at: string; id: number; event: BlueEvent }
  | { kind: "alert"; at: string; id: number; alert: BlueAlert }
  | { kind: "observation"; at: string; id: number; observation: BlueObservation };

function CaseDetailInner() {
  const params = useSearchParams();
  const id = Number(params.get("id"));
  const copy = useBlueCopy();
  const locale = useActiveLocale();
  const [item, setItem] = React.useState<BlueCase | null>(null);
  const [events, setEvents] = React.useState<BlueEvent[]>([]);
  const [alerts, setAlerts] = React.useState<BlueAlert[]>([]);
  const [eventHasMore, setEventHasMore] = React.useState(false);
  const [alertHasMore, setAlertHasMore] = React.useState(false);
  const [loadingMoreEvidence, setLoadingMoreEvidence] = React.useState(false);
  const [observations, setObservations] = React.useState<BlueObservation[]>([]);
  const [observationHasMore, setObservationHasMore] = React.useState(false);
  const [analyses, setAnalyses] = React.useState<BlueAnalysisRun[]>([]);
  const [actions, setActions] = React.useState<BlueAction[]>([]);
  const [loading, setLoading] = React.useState(true);
  const [error, setError] = React.useState("");
  const [status, setStatus] = React.useState<BlueCaseStatus>("open");
  const [severity, setSeverity] = React.useState<BlueSeverity>("medium");
  const [summary, setSummary] = React.useState("");
  const [saving, setSaving] = React.useState(false);
  const [analyzing, setAnalyzing] = React.useState(false);
  const [reporting, setReporting] = React.useState(false);
  const [noteKind, setNoteKind] = React.useState<BlueObservationKind>("fact");
  const [noteSummary, setNoteSummary] = React.useState("");
  const [noteDetail, setNoteDetail] = React.useState("");
  const [eventRef, setEventRef] = React.useState("");
  const [alertRef, setAlertRef] = React.useState("");
  const [addingNote, setAddingNote] = React.useState(false);
  const [actionKind, setActionKind] = React.useState<string>("isolate_host");
  const [customActionKind, setCustomActionKind] = React.useState("");
  const [actionTarget, setActionTarget] = React.useState("");
  const [actionRationale, setActionRationale] = React.useState("");
  const [proposingAction, setProposingAction] = React.useState(false);
  const verdictLabels = {
    malicious: copy.verdictMalicious,
    benign: copy.verdictBenign,
    inconclusive: copy.verdictInconclusive,
  };
  const confidenceLabels = {
    low: copy.confidenceLow,
    medium: copy.confidenceMedium,
    high: copy.confidenceHigh,
  };
  const assessedSeverityLabels = {
    none: copy.assessedNone,
    unknown: copy.assessedUnknown,
    info: blueSeverityLabel("info", locale),
    low: blueSeverityLabel("low", locale),
    medium: blueSeverityLabel("medium", locale),
    high: blueSeverityLabel("high", locale),
    critical: blueSeverityLabel("critical", locale),
  };
  const reportCopy = blueReportButtonCopy(locale);

  const load = React.useCallback(async () => {
    if (!Number.isSafeInteger(id) || id <= 0) {
      setLoading(false);
      return;
    }
    setLoading(true);
    setError("");
    try {
      const [caseRecord, caseEvents, caseAlerts, caseObservations, caseAnalyses, caseActions] = await Promise.all([
        blueApi.getCase(id),
        blueApi.events({ case_id: id, limit: CASE_EVIDENCE_PAGE }),
        blueApi.alerts({ case_id: id, limit: CASE_EVIDENCE_PAGE }),
        blueApi.observations(id, { limit: OBSERVATION_PAGE }),
        blueApi.analyses(id),
        blueApi.actions(id),
      ]);
      setItem(caseRecord);
      setEvents(caseEvents);
      setAlerts(caseAlerts);
      setEventHasMore(caseEvents.length === CASE_EVIDENCE_PAGE);
      setAlertHasMore(caseAlerts.length === CASE_EVIDENCE_PAGE);
      setObservations(caseObservations);
      setObservationHasMore(caseObservations.length === OBSERVATION_PAGE);
      setAnalyses(caseAnalyses.runs ?? []);
      setActions(caseActions);
      setStatus(caseRecord.status);
      setSeverity(caseRecord.severity);
      setSummary(caseRecord.summary ?? "");
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : copy.loadFailed);
    } finally {
      setLoading(false);
    }
  }, [copy.loadFailed, id]);

  React.useEffect(() => {
    void load();
  }, [load]);

  const hasActiveAnalysis = analyses.some((run) => run.status === "queued" || run.status === "running");
  React.useEffect(() => {
    if (!hasActiveAnalysis || !Number.isSafeInteger(id) || id <= 0) return;
    const activeRunIDs = new Set(
      analyses.filter((run) => run.status === "queued" || run.status === "running").map((run) => run.id),
    );
    const timer = window.setInterval(async () => {
      try {
        const result = await blueApi.analyses(id);
        const latestRuns = result.runs ?? [];
        setAnalyses(latestRuns);
        if (!latestRuns.some((run) => run.status === "queued" || run.status === "running")) {
          const finishedRuns = latestRuns.filter((run) => activeRunIDs.has(run.id));
          const [updatedObservations, updatedCase] = await Promise.all([blueApi.observations(id), blueApi.getCase(id)]);
          setObservations((current) => {
            const latest = new Set(updatedObservations.map((entry) => entry.id));
            return [...updatedObservations, ...current.filter((entry) => !latest.has(entry.id))];
          });
          setItem(updatedCase);
          setSummary(updatedCase.summary ?? "");
          if (finishedRuns.some((run) => run.status === "completed")) toast.success(copy.analysisReady);
          else if (finishedRuns.some((run) => run.status === "failed")) toast.error(copy.analysisRunFailed);
        }
      } catch {
        // An intermittent poll failure does not cancel the running analysis.
      }
    }, 3000);
    return () => window.clearInterval(timer);
  }, [analyses, copy.analysisReady, copy.analysisRunFailed, hasActiveAnalysis, id]);

  async function saveCase() {
    if (!item) return;
    setSaving(true);
    try {
      const updated = await blueApi.updateCase(item.id, { status, severity, summary });
      setItem(updated);
      toast.success(copy.saved);
    } catch (cause) {
      toast.error(cause instanceof Error ? cause.message : copy.saveFailed);
    } finally {
      setSaving(false);
    }
  }

  async function analyze() {
    if (!item) return;
    setAnalyzing(true);
    try {
      const response = await blueApi.analyzeCase(item.id);
      setAnalyses((runs) => [response.run, ...runs.filter((run) => run.id !== response.run.id)]);
      toast.success(copy.analysisQueued);
    } catch (cause) {
      toast.error(cause instanceof Error ? cause.message : copy.analysisFailed);
    } finally {
      setAnalyzing(false);
    }
  }

  async function downloadReport() {
    if (!item) return;
    setReporting(true);
    try {
      await downloadBlueCaseReport(item.id, locale);
    } catch (cause) {
      toast.error(cause instanceof Error ? cause.message : reportCopy.failed);
    } finally {
      setReporting(false);
    }
  }

  async function addObservation(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!item || !noteSummary.trim()) return;
    setAddingNote(true);
    try {
      const created = await blueApi.addObservation(item.id, {
        kind: noteKind,
        summary: noteSummary.trim(),
        detail: noteDetail.trim(),
        ...(eventRef ? { event_id: Number(eventRef) } : {}),
        ...(alertRef ? { alert_id: Number(alertRef) } : {}),
      });
      setObservations((notes) => [created, ...notes]);
      setNoteSummary("");
      setNoteDetail("");
      setEventRef("");
      setAlertRef("");
      toast.success(copy.noteSaved);
    } catch (cause) {
      toast.error(cause instanceof Error ? cause.message : copy.saveFailed);
    } finally {
      setAddingNote(false);
    }
  }

  async function proposeAction(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!item) return;
    const kind = actionKind === "custom" ? customActionKind.trim() : actionKind;
    if (!kind || !actionTarget.trim() || !actionRationale.trim()) return;
    setProposingAction(true);
    try {
      const created = await blueApi.proposeAction(item.id, {
        kind,
        target: actionTarget.trim(),
        rationale: actionRationale.trim(),
      });
      setActions((current) => [created, ...current]);
      setActionTarget("");
      setActionRationale("");
      setCustomActionKind("");
      toast.success(copy.actionProposed);
    } catch (cause) {
      toast.error(cause instanceof Error ? cause.message : copy.saveFailed);
    } finally {
      setProposingAction(false);
    }
  }

  async function loadMoreEvidence() {
    if (loadingMoreEvidence || (!eventHasMore && !alertHasMore && !observationHasMore)) return;
    setLoadingMoreEvidence(true);
    try {
      const [nextEvents, nextAlerts, nextObservations] = await Promise.all([
        eventHasMore
          ? blueApi.events({ case_id: id, limit: CASE_EVIDENCE_PAGE, offset: events.length })
          : Promise.resolve([]),
        alertHasMore
          ? blueApi.alerts({ case_id: id, limit: CASE_EVIDENCE_PAGE, offset: alerts.length })
          : Promise.resolve([]),
        observationHasMore
          ? blueApi.observations(id, { limit: OBSERVATION_PAGE, offset: observations.length })
          : Promise.resolve([]),
      ]);
      setEvents((current) => {
        const known = new Set(current.map((entry) => entry.id));
        return [...current, ...nextEvents.filter((entry) => !known.has(entry.id))];
      });
      setAlerts((current) => {
        const known = new Set(current.map((entry) => entry.id));
        return [...current, ...nextAlerts.filter((entry) => !known.has(entry.id))];
      });
      setObservations((current) => {
        const known = new Set(current.map((entry) => entry.id));
        return [...current, ...nextObservations.filter((entry) => !known.has(entry.id))];
      });
      setEventHasMore(eventHasMore && nextEvents.length === CASE_EVIDENCE_PAGE);
      setAlertHasMore(alertHasMore && nextAlerts.length === CASE_EVIDENCE_PAGE);
      setObservationHasMore(observationHasMore && nextObservations.length === OBSERVATION_PAGE);
    } catch (cause) {
      toast.error(cause instanceof Error ? cause.message : copy.loadFailed);
    } finally {
      setLoadingMoreEvidence(false);
    }
  }

  const timeline: TimelineEntry[] = [
    ...events.map((event): TimelineEntry => ({ kind: "event", at: event.occurred_at, id: event.id, event })),
    ...alerts.map((alert): TimelineEntry => ({ kind: "alert", at: alert.created_at, id: alert.id, alert })),
    ...observations.map(
      (observation): TimelineEntry => ({
        kind: "observation",
        at: observation.created_at,
        id: observation.id,
        observation,
      }),
    ),
  ].sort((a, b) => Date.parse(b.at) - Date.parse(a.at));

  if (!Number.isSafeInteger(id) || id <= 0) return <BlueEmpty message={copy.caseNotFound} />;

  return (
    <div className="space-y-6">
      <Button asChild variant="ghost" size="sm" className="-ml-2">
        <Link href="/blue/cases">
          <ArrowLeft />
          {copy.backToCases}
        </Link>
      </Button>
      {error && <BlueError message={error} retry={() => void load()} retryLabel={copy.refresh} />}
      {loading && !item ? <BlueLoading label={copy.loading} /> : null}
      {!loading && !item ? <BlueEmpty message={copy.caseNotFound} /> : null}
      {item && (
        <>
          <BluePageHeader
            title={`#${item.id} ${item.title}`}
            description={item.description || copy.casesDescription}
            actions={
              <>
                <Button variant="outline" size="sm" onClick={() => void load()} disabled={loading}>
                  <RefreshCw className={loading ? "animate-spin" : ""} />
                  {copy.refresh}
                </Button>
                <Button variant="outline" size="sm" onClick={() => void downloadReport()} disabled={reporting}>
                  <Download />
                  {reporting ? reportCopy.downloading : reportCopy.download}
                </Button>
                <Button size="sm" onClick={() => void analyze()} disabled={analyzing || hasActiveAnalysis}>
                  <Bot />
                  {analyzing ? copy.analyzing : hasActiveAnalysis ? copy.analysisRunning : copy.analyzeCase}
                </Button>
              </>
            }
          />
          <div className="flex flex-wrap items-center gap-2">
            <BlueSeverityBadge severity={item.severity} locale={locale} />
            <BlueCaseStatusBadge status={item.status} locale={locale} />
            <span className="text-xs text-muted-foreground">
              {copy.created}: {blueDate(item.created_at, locale)}
            </span>
            {item.red_finding_id ? (
              <Button asChild size="sm" variant="link">
                <Link href={`/redteam/findings/detail?id=${item.red_finding_id}`}>
                  {copy.linkedRedFinding} #{item.red_finding_id}
                  <ExternalLink />
                </Link>
              </Button>
            ) : null}
          </div>

          <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_19rem]">
            <div className="space-y-4">
              <Card>
                <CardHeader>
                  <CardTitle className="text-base">{copy.evidenceTimeline}</CardTitle>
                </CardHeader>
                <CardContent className="space-y-3">
                  {timeline.length === 0 ? (
                    <BlueEmpty message={copy.noEvidence} />
                  ) : (
                    timeline.map((entry) => (
                      <div key={`${entry.kind}-${entry.id}`} className="rounded-lg border p-3">
                        <div className="flex flex-wrap items-center justify-between gap-2">
                          <div className="flex items-center gap-2">
                            <Badge variant="outline">
                              {entry.kind === "event"
                                ? `${copy.eventID}${entry.id}`
                                : entry.kind === "alert"
                                  ? `${copy.alertID}${entry.id}`
                                  : `${copy.observation} #${entry.id}`}
                            </Badge>
                            {entry.kind === "event" ? (
                              <BlueSeverityBadge severity={entry.event.severity} locale={locale} />
                            ) : entry.kind === "alert" ? (
                              <BlueAlertStatusBadge status={entry.alert.status} locale={locale} />
                            ) : (
                              <Badge variant="secondary">
                                {blueObservationKindLabel(entry.observation.kind, locale)}
                              </Badge>
                            )}
                          </div>
                          <time className="text-xs text-muted-foreground">{blueDate(entry.at, locale)}</time>
                        </div>
                        {entry.kind === "event" ? (
                          <>
                            <p className="mt-2 text-sm font-medium">{entry.event.message}</p>
                            <p className="mt-1 text-xs text-muted-foreground">
                              {entry.event.source} · {entry.event.event_type}
                              {entry.event.asset ? ` · ${entry.event.asset}` : ""}
                            </p>
                            <details className="mt-2 text-xs">
                              <summary className="cursor-pointer text-primary">{copy.rawEvent}</summary>
                              <pre className="mt-2 max-h-72 overflow-auto rounded-md bg-muted p-3 whitespace-pre-wrap break-all">
                                {JSON.stringify(entry.event.raw, null, 2)}
                              </pre>
                            </details>
                          </>
                        ) : null}
                        {entry.kind === "alert" ? (
                          <>
                            <p className="mt-2 text-sm font-medium">{entry.alert.title}</p>
                            <p className="mt-1 text-xs text-muted-foreground">
                              {copy.rule} #{entry.alert.rule_id} · {copy.eventID}
                              {entry.alert.event_id}
                            </p>
                            {entry.alert.reason && (
                              <p className="mt-2 text-sm text-muted-foreground">{entry.alert.reason}</p>
                            )}
                          </>
                        ) : null}
                        {entry.kind === "observation" ? (
                          <>
                            <p className="mt-2 text-sm font-medium">{entry.observation.summary}</p>
                            {entry.observation.detail && (
                              <p className="mt-1 whitespace-pre-wrap text-sm text-muted-foreground">
                                {entry.observation.detail}
                              </p>
                            )}
                            {(entry.observation.assessment?.verdict ||
                              entry.observation.assessment?.confidence ||
                              entry.observation.assessment?.severity) && (
                              <dl className="mt-3 grid gap-2 rounded-md border bg-muted/30 p-2 text-xs sm:grid-cols-3">
                                <div>
                                  <dt className="text-muted-foreground">{copy.verdict}</dt>
                                  <dd className="mt-1 font-medium">
                                    {entry.observation.assessment.verdict
                                      ? verdictLabels[entry.observation.assessment.verdict]
                                      : copy.unavailable}
                                  </dd>
                                </div>
                                <div>
                                  <dt className="text-muted-foreground">{copy.confidence}</dt>
                                  <dd className="mt-1 font-medium">
                                    {entry.observation.assessment.confidence
                                      ? confidenceLabels[entry.observation.assessment.confidence]
                                      : copy.unavailable}
                                  </dd>
                                </div>
                                <div>
                                  <dt className="text-muted-foreground">{copy.assessedSeverity}</dt>
                                  <dd className="mt-1 font-medium">
                                    {entry.observation.assessment.severity
                                      ? assessedSeverityLabels[entry.observation.assessment.severity]
                                      : copy.unavailable}
                                  </dd>
                                </div>
                              </dl>
                            )}
                            <p className="mt-2 text-xs text-muted-foreground">{entry.observation.author}</p>
                            <div className="mt-2 flex flex-wrap gap-1">
                              {Array.from(
                                new Set([
                                  ...(entry.observation.event_id ? [entry.observation.event_id] : []),
                                  ...(entry.observation.evidence_event_ids ?? []),
                                ]),
                              ).map((evidenceID) => (
                                <Button asChild key={`event-${evidenceID}`} variant="outline" size="xs">
                                  <Link href={`/blue/events?event=${evidenceID}`}>
                                    {copy.eventID}
                                    {evidenceID}
                                  </Link>
                                </Button>
                              ))}
                              {Array.from(
                                new Set([
                                  ...(entry.observation.alert_id ? [entry.observation.alert_id] : []),
                                  ...(entry.observation.evidence_alert_ids ?? []),
                                ]),
                              ).map((evidenceID) => (
                                <Button asChild key={`alert-${evidenceID}`} variant="outline" size="xs">
                                  <Link href={`/blue/detection?tab=alerts&alert=${evidenceID}`}>
                                    {copy.alertID}
                                    {evidenceID}
                                  </Link>
                                </Button>
                              ))}
                            </div>
                          </>
                        ) : null}
                      </div>
                    ))
                  )}
                  {(eventHasMore || alertHasMore || observationHasMore) && (
                    <div className="flex justify-center pt-2">
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => void loadMoreEvidence()}
                        disabled={loadingMoreEvidence}
                      >
                        {loadingMoreEvidence ? copy.loadingMore : copy.loadMore}
                      </Button>
                    </div>
                  )}
                </CardContent>
              </Card>
              <Card>
                <CardHeader>
                  <CardTitle className="text-base">{copy.addObservation}</CardTitle>
                </CardHeader>
                <CardContent>
                  <form className="space-y-3" onSubmit={(event) => void addObservation(event)}>
                    <div className="grid gap-3 sm:grid-cols-2">
                      <div className="space-y-1.5">
                        <Label htmlFor="blue-observation-kind">{copy.observationKind}</Label>
                        <select
                          id="blue-observation-kind"
                          className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm"
                          value={noteKind}
                          onChange={(event) => setNoteKind(event.target.value as BlueObservationKind)}
                        >
                          {OBSERVATION_KINDS.map((kind) => (
                            <option key={kind} value={kind}>
                              {blueObservationKindLabel(kind, locale)}
                            </option>
                          ))}
                        </select>
                      </div>
                      <div className="space-y-1.5">
                        <Label htmlFor="blue-observation-summary">{copy.observationSummary}</Label>
                        <Input
                          id="blue-observation-summary"
                          maxLength={300}
                          value={noteSummary}
                          onChange={(event) => setNoteSummary(event.target.value)}
                          required
                        />
                      </div>
                    </div>
                    <div className="space-y-1.5">
                      <Label htmlFor="blue-observation-detail">{copy.observationDetail}</Label>
                      <Textarea
                        id="blue-observation-detail"
                        rows={4}
                        value={noteDetail}
                        onChange={(event) => setNoteDetail(event.target.value)}
                      />
                    </div>
                    <div className="grid gap-3 sm:grid-cols-2">
                      <div className="space-y-1.5">
                        <Label htmlFor="blue-observation-event">{copy.evidenceEventID}</Label>
                        <select
                          id="blue-observation-event"
                          className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm"
                          value={eventRef}
                          onChange={(event) => setEventRef(event.target.value)}
                        >
                          <option value="">{copy.noSelection}</option>
                          {events.map((entry) => (
                            <option key={entry.id} value={entry.id}>
                              #{entry.id} {entry.message.slice(0, 70)}
                            </option>
                          ))}
                        </select>
                      </div>
                      <div className="space-y-1.5">
                        <Label htmlFor="blue-observation-alert">{copy.evidenceAlertID}</Label>
                        <select
                          id="blue-observation-alert"
                          className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm"
                          value={alertRef}
                          onChange={(event) => setAlertRef(event.target.value)}
                        >
                          <option value="">{copy.noSelection}</option>
                          {alerts.map((entry) => (
                            <option key={entry.id} value={entry.id}>
                              #{entry.id} {entry.title.slice(0, 70)}
                            </option>
                          ))}
                        </select>
                      </div>
                    </div>
                    <Button type="submit" size="sm" disabled={addingNote || !noteSummary.trim()}>
                      {addingNote ? copy.creating : copy.addObservation}
                    </Button>
                  </form>
                </CardContent>
              </Card>
            </div>
            <div className="space-y-4">
              <Card>
                <CardHeader>
                  <CardTitle className="text-base">{copy.caseOverview}</CardTitle>
                </CardHeader>
                <CardContent className="space-y-3">
                  <div className="space-y-1.5">
                    <Label htmlFor="blue-detail-status">{copy.status}</Label>
                    <select
                      id="blue-detail-status"
                      className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm"
                      value={status}
                      onChange={(event) => setStatus(event.target.value as BlueCaseStatus)}
                    >
                      {CASE_STATUSES.map((value) => (
                        <option key={value} value={value}>
                          {blueCaseStatusLabel(value, locale)}
                        </option>
                      ))}
                    </select>
                  </div>
                  <div className="space-y-1.5">
                    <Label htmlFor="blue-detail-severity">{copy.severity}</Label>
                    <select
                      id="blue-detail-severity"
                      className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm"
                      value={severity}
                      onChange={(event) => setSeverity(event.target.value as BlueSeverity)}
                    >
                      {SEVERITIES.map((value) => (
                        <option key={value} value={value}>
                          {blueSeverityLabel(value, locale)}
                        </option>
                      ))}
                    </select>
                  </div>
                  <div className="space-y-1.5">
                    <Label htmlFor="blue-detail-summary">{copy.summary}</Label>
                    <Textarea
                      id="blue-detail-summary"
                      rows={5}
                      value={summary}
                      onChange={(event) => setSummary(event.target.value)}
                    />
                  </div>
                  <Button size="sm" className="w-full" onClick={() => void saveCase()} disabled={saving}>
                    <Save />
                    {saving ? copy.creating : copy.saveCase}
                  </Button>
                  <Separator />
                  <p className="text-xs text-muted-foreground">
                    {copy.updated}: {blueDate(item.updated_at, locale)}
                  </p>
                </CardContent>
              </Card>
              <Card>
                <CardHeader>
                  <CardTitle className="text-base">{copy.analysisHistory}</CardTitle>
                </CardHeader>
                <CardContent className="space-y-2">
                  <p className="text-xs text-muted-foreground">{copy.agentScope}</p>
                  {analyses.length === 0 ? (
                    <p className="text-sm text-muted-foreground">{copy.noData}</p>
                  ) : (
                    analyses.map((run) => (
                      <div key={run.id} className="rounded-md border p-2 text-sm">
                        <div className="flex items-center justify-between gap-2">
                          <span>#{run.id}</span>
                          <Badge variant={run.status === "failed" ? "destructive" : "secondary"}>
                            {run.status === "completed"
                              ? copy.analysisCompleted
                              : run.status === "failed"
                                ? copy.analysisRunFailed
                                : copy.analysisRunning}
                          </Badge>
                        </div>
                        <p className="mt-1 text-xs text-muted-foreground">{blueDate(run.created_at, locale)}</p>
                        {run.error && <p className="mt-2 text-xs text-destructive">{run.error}</p>}
                      </div>
                    ))
                  )}
                </CardContent>
              </Card>
              <Card>
                <CardHeader className="flex flex-row items-center justify-between gap-2">
                  <CardTitle className="text-base">{copy.responseActions}</CardTitle>
                  <Button asChild size="sm" variant="link">
                    <Link href="/blue/actions">
                      {copy.reviewActions}
                      <ExternalLink />
                    </Link>
                  </Button>
                </CardHeader>
                <CardContent className="space-y-4">
                  <p className="text-xs text-muted-foreground">{copy.actionScope}</p>
                  {actions.length === 0 ? (
                    <p className="text-sm text-muted-foreground">{copy.noActions}</p>
                  ) : (
                    <div className="space-y-2">
                      {actions.map((action) => (
                        <div key={action.id} className="rounded-md border p-2 text-sm">
                          <div className="flex items-center justify-between gap-2">
                            <span className="font-medium">
                              #{action.id} {actionKindLabel(action.kind, locale)}
                            </span>
                            <Badge
                              variant={
                                action.status === "rejected" || action.status === "failed" ? "destructive" : "secondary"
                              }
                            >
                              {action.status === "pending"
                                ? copy.actionPending
                                : action.status === "approved"
                                  ? copy.actionApproved
                                  : action.status === "rejected"
                                    ? copy.actionRejected
                                    : action.status === "completed"
                                      ? copy.actionCompleted
                                      : copy.actionFailed}
                            </Badge>
                          </div>
                          <p className="mt-1 break-all">{action.target}</p>
                          <p className="mt-1 text-xs text-muted-foreground">{action.rationale}</p>
                        </div>
                      ))}
                    </div>
                  )}
                  <Separator />
                  <form className="space-y-3" onSubmit={(event) => void proposeAction(event)}>
                    <div className="space-y-1.5">
                      <Label htmlFor="blue-action-kind">{copy.actionKind}</Label>
                      <select
                        id="blue-action-kind"
                        className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm"
                        value={actionKind}
                        onChange={(event) => setActionKind(event.target.value)}
                      >
                        {ACTION_KINDS.map((kind) => (
                          <option key={kind} value={kind}>
                            {actionKindLabel(kind, locale)}
                          </option>
                        ))}
                      </select>
                    </div>
                    {actionKind === "custom" && (
                      <Input
                        aria-label={copy.actionKind}
                        maxLength={100}
                        value={customActionKind}
                        onChange={(event) => setCustomActionKind(event.target.value)}
                        required
                      />
                    )}
                    <div className="space-y-1.5">
                      <Label htmlFor="blue-action-target">{copy.actionTarget}</Label>
                      <Input
                        id="blue-action-target"
                        maxLength={500}
                        value={actionTarget}
                        onChange={(event) => setActionTarget(event.target.value)}
                        required
                      />
                    </div>
                    <div className="space-y-1.5">
                      <Label htmlFor="blue-action-rationale">{copy.actionRationale}</Label>
                      <Textarea
                        id="blue-action-rationale"
                        rows={4}
                        maxLength={10000}
                        value={actionRationale}
                        onChange={(event) => setActionRationale(event.target.value)}
                        required
                      />
                    </div>
                    <Button
                      type="submit"
                      size="sm"
                      className="w-full"
                      disabled={
                        proposingAction ||
                        !actionTarget.trim() ||
                        !actionRationale.trim() ||
                        (actionKind === "custom" && !customActionKind.trim())
                      }
                    >
                      {proposingAction ? copy.creating : copy.proposeAction}
                    </Button>
                  </form>
                </CardContent>
              </Card>
            </div>
          </div>
        </>
      )}
    </div>
  );
}

export default function BlueCaseDetailPage() {
  return (
    <React.Suspense fallback={<BlueLoading label="Loading…" />}>
      <CaseDetailInner />
    </React.Suspense>
  );
}

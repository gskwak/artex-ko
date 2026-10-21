"use client";

import * as React from "react";

import Link from "next/link";

import { FileText, RefreshCw, Upload } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { useActiveLocale } from "@/i18n/locale-store";
import { ApiError } from "@/lib/api";
import { type BlueCase, type BlueEvent, type BlueImportResult, type BlueSeverity, blueApi } from "@/lib/blue-api";
import { blueSeverityLabel, useBlueCopy } from "@/lib/blue-copy";

import { BlueEmpty, BlueError, BlueLoading, BluePageHeader, BlueSeverityBadge, blueDate } from "../_components/blue-ui";

const MAX_FILE_BYTES = 8 * 1024 * 1024;
const PAGE_SIZE = 100;
const SUPPORTED_EXTENSION = /\.(json|jsonl|ndjson|csv)$/i;

const eventLabels = {
  en: {
    eventsSaved: "Events saved",
    alertsCreated: "Alerts created",
    detectionErrors: "Detection errors",
    importComplete: "Import completed.",
    importPartial: "Events were saved, but some detection rules failed.",
    ruleWarnings: "Detection warnings",
    assignCase: "Connect to case",
    changeCase: "Change case",
    chooseCase: "Choose a case",
    saveCaseLink: "Save case link",
    caseLinked: "Event and related alerts linked to the case.",
    caseMoveFailed: "Could not update the event's case.",
    caseMoveConflict:
      "This event or a related alert is cited as evidence. Review its references before changing the case.",
    createCase: "Create a case",
    noCases: "No cases are available yet.",
  },
  ko: {
    eventsSaved: "저장된 이벤트",
    alertsCreated: "생성된 경보",
    detectionErrors: "탐지 오류",
    importComplete: "이벤트 가져오기가 완료되었습니다.",
    importPartial: "이벤트는 저장됐지만 일부 탐지 규칙 실행에 실패했습니다.",
    ruleWarnings: "탐지 경고",
    assignCase: "사건에 연결",
    changeCase: "사건 변경",
    chooseCase: "사건 선택",
    saveCaseLink: "사건 연결 저장",
    caseLinked: "이벤트와 관련 경보를 사건에 연결했습니다.",
    caseMoveFailed: "이벤트의 사건 연결을 변경하지 못했습니다.",
    caseMoveConflict: "이 이벤트 또는 관련 경보가 이미 증거로 인용됐습니다. 사건을 바꾸기 전에 증거 참조를 확인하세요.",
    createCase: "사건 만들기",
    noCases: "아직 사건이 없습니다.",
  },
  zh: {
    eventsSaved: "已保存事件",
    alertsCreated: "已创建告警",
    detectionErrors: "检测错误",
    importComplete: "事件导入完成。",
    importPartial: "事件已保存，但部分检测规则执行失败。",
    ruleWarnings: "检测警告",
    assignCase: "关联案件",
    changeCase: "更改案件",
    chooseCase: "选择案件",
    saveCaseLink: "保存案件关联",
    caseLinked: "事件及相关告警已关联到案件。",
    caseMoveFailed: "无法更新事件所属案件。",
    caseMoveConflict: "该事件或相关告警已被引用为证据。更改案件前请检查证据引用。",
    createCase: "创建案件",
    noCases: "尚无可用案件。",
  },
  es: {
    eventsSaved: "Eventos guardados",
    alertsCreated: "Alertas creadas",
    detectionErrors: "Errores de detección",
    importComplete: "Importación completada.",
    importPartial: "Se guardaron los eventos, pero fallaron algunas reglas de detección.",
    ruleWarnings: "Avisos de detección",
    assignCase: "Vincular al caso",
    changeCase: "Cambiar de caso",
    chooseCase: "Elegir un caso",
    saveCaseLink: "Guardar vínculo",
    caseLinked: "El evento y sus alertas relacionadas se vincularon al caso.",
    caseMoveFailed: "No se pudo actualizar el caso del evento.",
    caseMoveConflict:
      "Este evento o una alerta relacionada ya se cita como evidencia. Revise sus referencias antes de cambiar el caso.",
    createCase: "Crear un caso",
    noCases: "Todavía no hay casos disponibles.",
  },
} as const;

function linkedEventID(): number | null {
  if (typeof window === "undefined") return null;
  const raw = new URLSearchParams(window.location.search).get("event");
  if (!raw || !/^\d+$/.test(raw)) return null;
  const id = Number(raw);
  return Number.isSafeInteger(id) && id > 0 ? id : null;
}

export default function BlueEventsPage() {
  const copy = useBlueCopy();
  const locale = useActiveLocale();
  const labels = eventLabels[locale];
  const [events, setEvents] = React.useState<BlueEvent[]>([]);
  const [loading, setLoading] = React.useState(true);
  const [error, setError] = React.useState("");
  const [pageError, setPageError] = React.useState("");
  const [loadingMore, setLoadingMore] = React.useState(false);
  const [hasMore, setHasMore] = React.useState(false);
  const [nextOffset, setNextOffset] = React.useState(0);
  const [file, setFile] = React.useState<File | null>(null);
  const [defaultSource, setDefaultSource] = React.useState("upload");
  const [importError, setImportError] = React.useState("");
  const [importing, setImporting] = React.useState(false);
  const [importResult, setImportResult] = React.useState<BlueImportResult | null>(null);
  const [sourceFilter, setSourceFilter] = React.useState("");
  const [severityFilter, setSeverityFilter] = React.useState<BlueSeverity | "">("");
  const [textFilter, setTextFilter] = React.useState("");
  const [selectedEventID, setSelectedEventID] = React.useState<number | null>(null);
  const [cases, setCases] = React.useState<BlueCase[]>([]);
  const [casesLoaded, setCasesLoaded] = React.useState(false);
  const [casesLoading, setCasesLoading] = React.useState(false);
  const [casesError, setCasesError] = React.useState("");
  const [moveEventID, setMoveEventID] = React.useState<number | null>(null);
  const [targetCaseID, setTargetCaseID] = React.useState("");
  const [movingEventID, setMovingEventID] = React.useState<number | null>(null);
  const [moveError, setMoveError] = React.useState("");
  const fileInput = React.useRef<HTMLInputElement>(null);
  const loadVersion = React.useRef(0);

  const load = React.useCallback(async () => {
    const version = ++loadVersion.current;
    setLoading(true);
    setLoadingMore(false);
    setError("");
    setPageError("");
    try {
      const recent = await blueApi.events({ limit: PAGE_SIZE, offset: 0 });
      if (version !== loadVersion.current) return;
      setHasMore(recent.length === PAGE_SIZE);
      setNextOffset(recent.length);
      const linkedID = linkedEventID();
      setSelectedEventID(linkedID);
      if (linkedID && !recent.some((event) => event.id === linkedID)) {
        try {
          recent.unshift(await blueApi.getEvent(linkedID));
        } catch (cause) {
          if (version === loadVersion.current) setError(cause instanceof Error ? cause.message : copy.loadFailed);
        }
      }
      if (version === loadVersion.current) setEvents(recent);
    } catch (cause) {
      if (version === loadVersion.current) setError(cause instanceof Error ? cause.message : copy.loadFailed);
    } finally {
      if (version === loadVersion.current) setLoading(false);
    }
  }, [copy.loadFailed]);

  const loadMore = React.useCallback(async () => {
    if (loading || loadingMore || !hasMore) return;
    const version = loadVersion.current;
    setLoadingMore(true);
    setPageError("");
    try {
      const next = await blueApi.events({ limit: PAGE_SIZE, offset: nextOffset });
      if (version !== loadVersion.current) return;
      setEvents((current) => {
        const seen = new Set(current.map((event) => event.id));
        return [...current, ...next.filter((event) => !seen.has(event.id))];
      });
      setNextOffset(nextOffset + next.length);
      setHasMore(next.length === PAGE_SIZE);
    } catch (cause) {
      if (version === loadVersion.current) setPageError(cause instanceof Error ? cause.message : copy.loadFailed);
    } finally {
      if (version === loadVersion.current) setLoadingMore(false);
    }
  }, [copy.loadFailed, hasMore, loading, loadingMore, nextOffset]);

  React.useEffect(() => {
    void load();
  }, [load]);

  React.useEffect(() => {
    if (!loading && selectedEventID && events.some((event) => event.id === selectedEventID)) {
      document.getElementById(`blue-event-${selectedEventID}`)?.scrollIntoView({ block: "center" });
    }
  }, [events, loading, selectedEventID]);

  const readFile = React.useCallback(
    (selected: File | null) => {
      setFile(selected);
      setImportError("");
      setImportResult(null);
      if (!selected) return;
      if (selected.size > MAX_FILE_BYTES) {
        setImportError(copy.fileTooLarge);
        return;
      }
      if (selected.size === 0 || !SUPPORTED_EXTENSION.test(selected.name)) {
        setImportError(copy.invalidFile);
      }
    },
    [copy.fileTooLarge, copy.invalidFile],
  );

  const importFile = React.useCallback(async () => {
    if (!file || importError || importing) return;
    setImporting(true);
    setImportError("");
    setImportResult(null);
    try {
      const result = await blueApi.uploadEvents(file, defaultSource);
      setImportResult(result);
      if (result.detection_errors > 0 || result.warnings.length > 0) {
        toast.warning(labels.importPartial);
      } else {
        toast.success(`${copy.importComplete}: ${result.imported}`);
      }
      setFile(null);
      if (fileInput.current) fileInput.current.value = "";
      await load();
    } catch (cause) {
      setImportError(`${copy.importFailed} ${cause instanceof Error ? cause.message : copy.loadFailed}`);
    } finally {
      setImporting(false);
    }
  }, [
    copy.importComplete,
    copy.importFailed,
    copy.loadFailed,
    defaultSource,
    file,
    importError,
    importing,
    labels.importPartial,
    load,
  ]);

  const loadCases = React.useCallback(async () => {
    setCasesLoading(true);
    setCasesError("");
    try {
      setCases(await blueApi.cases({ limit: 1000 }));
      setCasesLoaded(true);
    } catch (cause) {
      setCasesError(cause instanceof Error ? cause.message : copy.loadFailed);
    } finally {
      setCasesLoading(false);
    }
  }, [copy.loadFailed]);

  const openCaseMove = React.useCallback(
    (event: BlueEvent) => {
      if (moveEventID === event.id) {
        setMoveEventID(null);
        return;
      }
      setMoveEventID(event.id);
      setTargetCaseID(event.case_id ? String(event.case_id) : "");
      setMoveError("");
      if (!casesLoaded && !casesLoading) void loadCases();
    },
    [casesLoaded, casesLoading, loadCases, moveEventID],
  );

  const moveToCase = React.useCallback(
    async (event: BlueEvent) => {
      const caseID = Number(targetCaseID);
      if (!Number.isSafeInteger(caseID) || caseID <= 0 || caseID === event.case_id || movingEventID !== null) return;
      setMovingEventID(event.id);
      setMoveError("");
      try {
        const updated = await blueApi.moveEventCase(event.id, caseID);
        setEvents((current) => current.map((item) => (item.id === event.id ? updated : item)));
        setMoveEventID(null);
        toast.success(labels.caseLinked);
      } catch (cause) {
        setMoveError(
          cause instanceof ApiError && cause.status === 409
            ? labels.caseMoveConflict
            : `${labels.caseMoveFailed} ${cause instanceof Error ? cause.message : ""}`,
        );
      } finally {
        setMovingEventID(null);
      }
    },
    [labels.caseLinked, labels.caseMoveConflict, labels.caseMoveFailed, movingEventID, targetCaseID],
  );

  const sources = Array.from(new Set(events.map((event) => event.source))).sort();
  const filtered = events.filter((event) => {
    if (sourceFilter && event.source !== sourceFilter) return false;
    if (severityFilter && event.severity !== severityFilter) return false;
    const query = textFilter.trim().toLowerCase();
    return (
      !query ||
      [event.message, event.asset, event.event_type, String(event.id)].some((value) =>
        value.toLowerCase().includes(query),
      )
    );
  });

  return (
    <div className="space-y-5">
      <BluePageHeader
        title={copy.events}
        description={copy.eventsDescription}
        actions={
          <Button variant="outline" size="sm" onClick={() => void load()} disabled={loading}>
            <RefreshCw className={loading ? "animate-spin" : ""} />
            {copy.refresh}
          </Button>
        }
      />

      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="flex items-center gap-2 text-base">
            <Upload className="size-4" />
            {copy.importEvents}
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <p className="text-sm text-muted-foreground">{copy.importDescription}</p>
          <div className="grid gap-3 md:grid-cols-[minmax(0,1fr)_16rem]">
            <div className="space-y-1.5">
              <label htmlFor="blue-event-file" className="text-xs font-medium">
                {copy.chooseFile}
              </label>
              <Input
                ref={fileInput}
                id="blue-event-file"
                type="file"
                accept=".json,.jsonl,.ndjson,.csv,application/json,text/csv,application/x-ndjson"
                onChange={(event) => readFile(event.target.files?.[0] ?? null)}
                disabled={importing}
              />
            </div>
            <div className="space-y-1.5">
              <label htmlFor="blue-event-source" className="text-xs font-medium">
                {copy.sourceOverride}
              </label>
              <Input
                id="blue-event-source"
                value={defaultSource}
                onChange={(event) => setDefaultSource(event.target.value)}
                disabled={importing}
              />
            </div>
          </div>
          {importError && <BlueError message={importError} />}
          {file && !importError && (
            <div className="flex flex-wrap items-center justify-between gap-2 rounded-lg border bg-muted/30 p-3 text-xs text-muted-foreground">
              <span className="flex items-center gap-2">
                <FileText className="size-4 text-primary" />
                {file.name} · {(file.size / 1024).toLocaleString(undefined, { maximumFractionDigits: 1 })} KB
              </span>
              <Button size="sm" onClick={() => void importFile()} disabled={importing}>
                {importing ? copy.importing : copy.import}
              </Button>
            </div>
          )}
          {importResult && (
            <div
              role={importResult.detection_errors > 0 || importResult.warnings.length > 0 ? "alert" : "status"}
              className={
                importResult.detection_errors > 0 || importResult.warnings.length > 0
                  ? "space-y-3 rounded-lg border border-amber-500/50 bg-amber-500/10 p-4"
                  : "space-y-3 rounded-lg border border-emerald-500/40 bg-emerald-500/10 p-4"
              }
            >
              <p className="font-medium">
                {importResult.detection_errors > 0 || importResult.warnings.length > 0
                  ? labels.importPartial
                  : labels.importComplete}
              </p>
              <dl className="grid gap-3 text-sm sm:grid-cols-3">
                <div>
                  <dt className="text-muted-foreground">{labels.eventsSaved}</dt>
                  <dd className="font-semibold">{importResult.imported.toLocaleString(locale)}</dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">{labels.alertsCreated}</dt>
                  <dd className="font-semibold">{importResult.alerts_matched.toLocaleString(locale)}</dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">{labels.detectionErrors}</dt>
                  <dd className="font-semibold">{importResult.detection_errors.toLocaleString(locale)}</dd>
                </div>
              </dl>
              {importResult.warnings.length > 0 && (
                <div className="space-y-1 text-sm">
                  <p className="font-medium">{labels.ruleWarnings}</p>
                  <ul className="max-h-48 list-disc space-y-1 overflow-auto pl-5">
                    {Array.from(new Set(importResult.warnings)).map((warning) => (
                      <li key={warning}>{warning}</li>
                    ))}
                  </ul>
                </div>
              )}
            </div>
          )}
        </CardContent>
      </Card>

      <div className="flex flex-wrap items-center gap-2">
        <Input
          value={textFilter}
          onChange={(event) => setTextFilter(event.target.value)}
          placeholder={copy.message}
          aria-label={copy.message}
          className="max-w-sm"
        />
        <select
          value={sourceFilter}
          onChange={(event) => setSourceFilter(event.target.value)}
          aria-label={copy.filterSource}
          className="h-8 rounded-lg border border-input bg-background px-2.5 text-sm"
        >
          <option value="">{copy.allSources}</option>
          {sources.map((source) => (
            <option key={source} value={source}>
              {source}
            </option>
          ))}
        </select>
        <select
          value={severityFilter}
          onChange={(event) => setSeverityFilter(event.target.value as BlueSeverity | "")}
          aria-label={copy.severity}
          className="h-8 rounded-lg border border-input bg-background px-2.5 text-sm"
        >
          <option value="">{copy.allSeverities}</option>
          {(["critical", "high", "medium", "low", "info"] as const).map((severity) => (
            <option key={severity} value={severity}>
              {blueSeverityLabel(severity, locale)}
            </option>
          ))}
        </select>
        <span className="text-xs text-muted-foreground">
          {filtered.length.toLocaleString()} / {events.length.toLocaleString()}
        </span>
      </div>
      {error && <BlueError message={error} retry={() => void load()} retryLabel={copy.refresh} />}
      {loading && events.length === 0 && <BlueLoading label={copy.loading} />}
      {!loading && !error && events.length === 0 && <BlueEmpty message={copy.noEvents} />}
      {events.length > 0 && filtered.length === 0 && <BlueEmpty message={copy.noData} />}
      {filtered.length > 0 && (
        <Card>
          <CardContent className="p-0">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>ID</TableHead>
                  <TableHead>{copy.occurredAt}</TableHead>
                  <TableHead>{copy.source}</TableHead>
                  <TableHead>{copy.eventType}</TableHead>
                  <TableHead>{copy.severity}</TableHead>
                  <TableHead>{copy.asset}</TableHead>
                  <TableHead>{copy.message}</TableHead>
                  <TableHead>{copy.case}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {filtered.map((event) => (
                  <TableRow
                    key={event.id}
                    id={`blue-event-${event.id}`}
                    aria-selected={event.id === selectedEventID}
                    data-state={event.id === selectedEventID ? "selected" : undefined}
                  >
                    <TableCell className="font-mono text-xs">#{event.id}</TableCell>
                    <TableCell className="text-xs">{blueDate(event.occurred_at, locale)}</TableCell>
                    <TableCell>{event.source}</TableCell>
                    <TableCell className="font-mono text-xs">{event.event_type}</TableCell>
                    <TableCell>
                      <BlueSeverityBadge severity={event.severity} locale={locale} />
                    </TableCell>
                    <TableCell className="max-w-36 truncate">{event.asset || copy.unavailable}</TableCell>
                    <TableCell className="min-w-72 max-w-xl whitespace-normal">
                      <div className="space-y-1">
                        <p className="break-words">{event.message}</p>
                        <details className="text-xs text-muted-foreground">
                          <summary className="cursor-pointer select-none">{copy.showOriginal}</summary>
                          <pre className="mt-2 max-h-72 overflow-auto rounded-md border bg-muted/40 p-2 text-[11px] whitespace-pre-wrap break-all">
                            {JSON.stringify(event.raw, null, 2)}
                          </pre>
                        </details>
                      </div>
                    </TableCell>
                    <TableCell className="min-w-48 align-top">
                      <div className="space-y-2">
                        <div>
                          {event.case_id ? (
                            <Link
                              className="text-primary underline-offset-4 hover:underline"
                              href={`/blue/cases/detail?id=${event.case_id}`}
                            >
                              #{event.case_id}
                            </Link>
                          ) : (
                            copy.noCase
                          )}
                        </div>
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => openCaseMove(event)}
                          disabled={movingEventID !== null}
                        >
                          {event.case_id ? labels.changeCase : labels.assignCase}
                        </Button>
                        {moveEventID === event.id && (
                          <div className="space-y-2 rounded-md border bg-background p-2">
                            {casesLoading && <p className="text-muted-foreground text-xs">{copy.loading}</p>}
                            {casesError && (
                              <div role="alert" className="space-y-1 text-xs">
                                <p className="text-destructive">{casesError}</p>
                                <Button variant="outline" size="sm" onClick={() => void loadCases()}>
                                  {copy.refresh}
                                </Button>
                              </div>
                            )}
                            {casesLoaded && !casesLoading && !casesError && (
                              <>
                                {cases.length === 0 ? (
                                  <p className="text-muted-foreground text-xs">{labels.noCases}</p>
                                ) : (
                                  <>
                                    <label htmlFor={`blue-event-case-${event.id}`} className="sr-only">
                                      {labels.chooseCase}
                                    </label>
                                    <select
                                      id={`blue-event-case-${event.id}`}
                                      value={targetCaseID}
                                      onChange={(selection) => setTargetCaseID(selection.target.value)}
                                      disabled={movingEventID === event.id}
                                      className="h-8 w-full rounded-lg border border-input bg-background px-2 text-xs"
                                    >
                                      <option value="">{labels.chooseCase}</option>
                                      {cases.map((caseItem) => (
                                        <option key={caseItem.id} value={caseItem.id}>
                                          #{caseItem.id} {caseItem.title}
                                        </option>
                                      ))}
                                    </select>
                                    <Button
                                      size="sm"
                                      onClick={() => void moveToCase(event)}
                                      disabled={
                                        !targetCaseID ||
                                        Number(targetCaseID) === event.case_id ||
                                        movingEventID !== null
                                      }
                                    >
                                      {movingEventID === event.id ? copy.loading : labels.saveCaseLink}
                                    </Button>
                                  </>
                                )}
                                <Link
                                  className="block text-primary text-xs underline-offset-4 hover:underline"
                                  href="/blue/cases"
                                >
                                  {labels.createCase}
                                </Link>
                              </>
                            )}
                            {moveError && (
                              <p role="alert" className="text-destructive text-xs">
                                {moveError}
                              </p>
                            )}
                          </div>
                        )}
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}
      {pageError && <BlueError message={pageError} retry={() => void loadMore()} retryLabel={copy.loadMore} />}
      {hasMore && !loading && (
        <div className="flex justify-center">
          <Button variant="outline" onClick={() => void loadMore()} disabled={loadingMore}>
            {loadingMore ? copy.loadingMore : copy.loadMore}
          </Button>
        </div>
      )}
    </div>
  );
}

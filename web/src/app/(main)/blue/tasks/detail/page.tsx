"use client";

import * as React from "react";

import Link from "next/link";
import { useSearchParams } from "next/navigation";

import { ArrowLeft, Bot, Download, ExternalLink, RefreshCw, Save, Square } from "lucide-react";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { SidebarTrigger } from "@/components/ui/sidebar";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { useActiveLocale } from "@/i18n/locale-store";
import { ApiError } from "@/lib/api";
import { type BlueCase, blueApi } from "@/lib/blue-api";
import { parseBlueEvidenceIDs } from "@/lib/blue-evidence-ids";
import { blueReportButtonCopy, downloadBlueTaskReport } from "@/lib/blue-report";
import {
  type BlueTask,
  type BlueTaskPriority,
  type BlueTaskStatus,
  blueTaskApi,
  type UpdateBlueTask,
} from "@/lib/blue-task-api";
import { blueTaskPriorityLabel, blueTaskStatusLabel, useBlueTaskCopy } from "@/lib/blue-task-copy";

import { BlueError, BlueLoading, blueDate } from "../../_components/blue-ui";

const STATUSES: BlueTaskStatus[] = ["open", "in_progress", "completed", "blocked"];
const PRIORITIES: BlueTaskPriority[] = ["low", "medium", "high", "critical"];
const TAB_LABELS = {
  en: { overview: "Overview", analysis: "Analysis", result: "Result" },
  ko: { overview: "개요", analysis: "분석", result: "결과" },
  zh: { overview: "概览", analysis: "分析", result: "结果" },
  es: { overview: "Resumen", analysis: "Análisis", result: "Resultado" },
} as const;

function BlueTaskDetailInner() {
  const params = useSearchParams();
  const id = Number(params.get("id"));
  const copy = useBlueTaskCopy();
  const locale = useActiveLocale();
  const [task, setTask] = React.useState<BlueTask | null>(null);
  const [cases, setCases] = React.useState<BlueCase[]>([]);
  const [loading, setLoading] = React.useState(true);
  const [error, setError] = React.useState("");
  const [saving, setSaving] = React.useState(false);
  const [running, setRunning] = React.useState(false);
  const [stopping, setStopping] = React.useState(false);
  const [reporting, setReporting] = React.useState(false);
  const [title, setTitle] = React.useState("");
  const [description, setDescription] = React.useState("");
  const [caseID, setCaseID] = React.useState("");
  const [priority, setPriority] = React.useState<BlueTaskPriority>("medium");
  const [status, setStatus] = React.useState<BlueTaskStatus>("open");
  const [result, setResult] = React.useState("");
  const [eventIDsText, setEventIDsText] = React.useState("");
  const [alertIDsText, setAlertIDsText] = React.useState("");
  const [tab, setTab] = React.useState("overview");

  const syncTask = React.useCallback((value: BlueTask) => {
    setTask(value);
    setTitle(value.title);
    setDescription(value.description);
    setCaseID(value.case_id ? String(value.case_id) : "");
    setPriority(value.priority);
    setStatus(value.status);
    setResult(value.result);
  }, []);

  const load = React.useCallback(async () => {
    if (!Number.isSafeInteger(id) || id <= 0) {
      setLoading(false);
      return;
    }
    setLoading(true);
    setError("");
    try {
      const value = await blueTaskApi.get(id);
      syncTask(value);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : copy.loadFailed);
    } finally {
      setLoading(false);
    }
  }, [copy.loadFailed, id, syncTask]);

  React.useEffect(() => {
    void load();
  }, [load]);

  React.useEffect(() => {
    void blueApi
      .cases({ limit: 100 })
      .then(setCases)
      .catch(() => setCases([]));
  }, []);

  const analysisInProgress = task?.status === "in_progress" && Boolean(task.analysis_run_id);
  React.useEffect(() => {
    if (!analysisInProgress || stopping || !Number.isSafeInteger(id) || id <= 0) return;
    let active = true;
    const timer = window.setInterval(async () => {
      try {
        const value = await blueTaskApi.get(id);
        if (active) syncTask(value);
      } catch {
        // A transient poll failure does not stop the server-side analysis.
      }
    }, 3000);
    return () => {
      active = false;
      window.clearInterval(timer);
    };
  }, [analysisInProgress, id, stopping, syncTask]);

  function formBody(): UpdateBlueTask {
    return {
      title: title.trim(),
      description: description.trim(),
      priority,
      status,
      result: result.trim(),
      ...(caseID ? { case_id: Number(caseID) } : { clear_case: true }),
    };
  }

  async function save(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!task || !title.trim() || analysisInProgress) return;
    setSaving(true);
    try {
      syncTask(await blueTaskApi.update(task.id, formBody()));
      toast.success(copy.saved);
    } catch (cause) {
      toast.error(cause instanceof Error ? cause.message : copy.saveFailed);
    } finally {
      setSaving(false);
    }
  }

  async function runAnalysis() {
    if (!task || !caseID || analysisInProgress || stopping) return;
    const eventIDs = parseBlueEvidenceIDs(eventIDsText);
    const alertIDs = parseBlueEvidenceIDs(alertIDsText);
    if (eventIDs === null || alertIDs === null) {
      toast.error(copy.invalidEvidenceIDs);
      setTab("analysis");
      return;
    }
    setRunning(true);
    try {
      await blueTaskApi.update(task.id, formBody());
      const response = await blueTaskApi.run(task.id, { event_ids: eventIDs, alert_ids: alertIDs });
      syncTask(response.task);
      toast.success(copy.runQueued);
    } catch (cause) {
      toast.error(cause instanceof Error ? cause.message : copy.runFailed);
    } finally {
      setRunning(false);
    }
  }

  async function stopAnalysis() {
    if (!task || !analysisInProgress || stopping) return;
    setStopping(true);
    try {
      syncTask(await blueTaskApi.stop(task.id));
      toast.success(copy.stopped);
    } catch (cause) {
      if (cause instanceof ApiError && cause.status === 409) {
        try {
          syncTask(await blueTaskApi.get(task.id));
          toast.info(copy.stopAlreadyFinished);
        } catch (refreshCause) {
          toast.error(refreshCause instanceof Error ? refreshCause.message : copy.stopFailed);
        }
      } else {
        toast.error(cause instanceof Error ? cause.message : copy.stopFailed);
      }
    } finally {
      setStopping(false);
    }
  }

  async function downloadReport() {
    if (!task) return;
    setReporting(true);
    try {
      await downloadBlueTaskReport(task.id, locale);
    } catch (cause) {
      toast.error(cause instanceof Error ? cause.message : blueReportButtonCopy(locale).failed);
    } finally {
      setReporting(false);
    }
  }

  if (!Number.isSafeInteger(id) || id <= 0) {
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-3 p-10 text-center">
        <BlueError message={copy.invalidTask} />
        <Button asChild variant="outline">
          <Link href="/blue/tasks">
            <ArrowLeft /> {copy.back}
          </Link>
        </Button>
      </div>
    );
  }

  if (!task) {
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-3 p-10 text-center">
        {error ? <BlueError message={error} retry={() => void load()} retryLabel={copy.refresh} /> : null}
        {loading ? <BlueLoading label={copy.loading} /> : null}
        <Button asChild variant="outline">
          <Link href="/blue/tasks">
            <ArrowLeft /> {copy.back}
          </Link>
        </Button>
      </div>
    );
  }

  const caseName = cases.find((item) => item.id === task.case_id)?.title;
  const tabLabels = TAB_LABELS[locale];
  const reportCopy = blueReportButtonCopy(locale);

  return (
    <Tabs value={tab} onValueChange={setTab} className="flex min-h-0 flex-1 flex-col gap-0">
      <header className="sticky top-0 z-10 flex flex-col gap-2 border-b bg-background/95 px-4 py-2.5 backdrop-blur lg:px-6">
        <div className="flex flex-wrap items-center gap-2">
          <SidebarTrigger className="-ml-1" />
          <Button asChild variant="ghost" size="icon" className="size-7" aria-label={copy.back}>
            <Link href="/blue/tasks">
              <ArrowLeft />
            </Link>
          </Button>
          <h1 className="min-w-0 flex-1 truncate font-semibold text-sm" title={task.title}>
            {task.title}
          </h1>
          <code className="hidden rounded bg-muted px-1.5 py-0.5 font-mono text-muted-foreground text-xs sm:inline">
            {task.id}
          </code>
          <Badge variant={task.status === "blocked" ? "destructive" : "secondary"}>
            {blueTaskStatusLabel(task.status, copy)}
          </Badge>
          <Button variant="outline" size="sm" onClick={() => void load()} disabled={loading} aria-label={copy.refresh}>
            <RefreshCw className={loading ? "animate-spin" : ""} />
            <span className="hidden sm:inline">{copy.refresh}</span>
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={() => void downloadReport()}
            disabled={reporting}
            aria-label={reportCopy.download}
          >
            <Download />
            <span className="hidden sm:inline">{reporting ? reportCopy.downloading : reportCopy.download}</span>
          </Button>
          {analysisInProgress ? (
            <Button
              variant="destructive"
              size="sm"
              onClick={() => void stopAnalysis()}
              disabled={stopping}
              aria-label={stopping ? copy.stopping : copy.stop}
            >
              <Square />
              <span className="hidden sm:inline">{stopping ? copy.stopping : copy.stop}</span>
            </Button>
          ) : (
            <Button
              size="sm"
              onClick={() => void runAnalysis()}
              disabled={!caseID || running || stopping || saving || !title.trim()}
              aria-label={running ? copy.running : copy.run}
            >
              <Bot />
              <span className="hidden sm:inline">{running ? copy.running : copy.run}</span>
            </Button>
          )}
        </div>
        <p className="truncate text-muted-foreground text-xs">{task.description || copy.description}</p>
        <div className="no-scrollbar min-w-0 overflow-x-auto">
          <TabsList className="min-w-max">
            <TabsTrigger value="overview">{tabLabels.overview}</TabsTrigger>
            <TabsTrigger value="analysis">{tabLabels.analysis}</TabsTrigger>
            <TabsTrigger value="result">{tabLabels.result}</TabsTrigger>
          </TabsList>
        </div>
      </header>

      <div className="flex-1 p-4 lg:p-6">
        {error && <BlueError message={error} retry={() => void load()} retryLabel={copy.refresh} />}
        <TabsContent value="overview" className="mt-0">
          <div className="grid gap-5 xl:grid-cols-[minmax(0,2fr)_minmax(18rem,1fr)]">
            <Card>
              <CardHeader>
                <CardTitle>{tabLabels.overview}</CardTitle>
              </CardHeader>
              <CardContent>
                <form className="space-y-4" onSubmit={(event) => void save(event)}>
                  <div className="space-y-1.5">
                    <Label htmlFor="blue-task-detail-title">{copy.taskTitle}</Label>
                    <Input
                      id="blue-task-detail-title"
                      value={title}
                      onChange={(event) => setTitle(event.target.value)}
                      maxLength={240}
                      required
                      disabled={analysisInProgress}
                    />
                  </div>
                  <div className="space-y-1.5">
                    <Label htmlFor="blue-task-detail-description">{copy.taskDescription}</Label>
                    <Textarea
                      id="blue-task-detail-description"
                      value={description}
                      onChange={(event) => setDescription(event.target.value)}
                      rows={5}
                      disabled={analysisInProgress}
                    />
                  </div>
                  <div className="grid gap-4 sm:grid-cols-3">
                    <div className="space-y-1.5">
                      <Label htmlFor="blue-task-detail-case">{copy.case}</Label>
                      <Input
                        id="blue-task-detail-case"
                        type="number"
                        min="1"
                        step="1"
                        list="blue-task-detail-cases"
                        value={caseID}
                        onChange={(event) => {
                          const value = event.target.value;
                          if (!value || (/^[1-9]\d*$/.test(value) && Number.isSafeInteger(Number(value)))) {
                            setCaseID(value);
                            setEventIDsText("");
                            setAlertIDsText("");
                          }
                        }}
                        placeholder={copy.noCase}
                        disabled={analysisInProgress}
                      />
                      <datalist id="blue-task-detail-cases">
                        {cases.map((item) => (
                          <option key={item.id} value={item.id} label={item.title} />
                        ))}
                      </datalist>
                    </div>
                    <div className="space-y-1.5">
                      <Label htmlFor="blue-task-detail-priority">{copy.priority}</Label>
                      <select
                        id="blue-task-detail-priority"
                        className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm"
                        value={priority}
                        onChange={(event) => setPriority(event.target.value as BlueTaskPriority)}
                        disabled={analysisInProgress}
                      >
                        {PRIORITIES.map((value) => (
                          <option key={value} value={value}>
                            {blueTaskPriorityLabel(value, copy)}
                          </option>
                        ))}
                      </select>
                    </div>
                    <div className="space-y-1.5">
                      <Label htmlFor="blue-task-detail-status">{copy.status}</Label>
                      <select
                        id="blue-task-detail-status"
                        className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm"
                        value={status}
                        onChange={(event) => setStatus(event.target.value as BlueTaskStatus)}
                        disabled={analysisInProgress}
                      >
                        {STATUSES.map((value) => (
                          <option key={value} value={value}>
                            {blueTaskStatusLabel(value, copy)}
                          </option>
                        ))}
                      </select>
                    </div>
                  </div>
                  <Button type="submit" disabled={saving || running || analysisInProgress || !title.trim()}>
                    <Save />
                    {saving ? copy.saving : copy.save}
                  </Button>
                </form>
              </CardContent>
            </Card>

            <Card className="self-start">
              <CardHeader>
                <CardTitle>{copy.case}</CardTitle>
              </CardHeader>
              <CardContent className="space-y-3 text-sm">
                {task.case_id ? (
                  <>
                    <p>
                      #{task.case_id} {caseName}
                    </p>
                    <Button asChild variant="outline" size="sm">
                      <Link href={`/blue/cases/detail?id=${task.case_id}`}>
                        <ExternalLink />
                        {copy.caseLink}
                      </Link>
                    </Button>
                  </>
                ) : (
                  <p className="text-muted-foreground">{copy.noCase}</p>
                )}
              </CardContent>
            </Card>
          </div>
        </TabsContent>

        <TabsContent value="analysis" className="mt-0">
          <Card>
            <CardHeader>
              <CardTitle>{copy.latestAnalysis}</CardTitle>
            </CardHeader>
            <CardContent className="space-y-4 text-sm">
              <p className="text-muted-foreground">{copy.analystScope}</p>
              {analysisInProgress && <p role="status">{copy.analysisInProgress}</p>}
              {!caseID && <p className="text-muted-foreground text-xs">{copy.runNeedsCase}</p>}
              <div className="space-y-3 rounded-md border p-3">
                <p className="font-medium">{copy.evidenceSelection}</p>
                <p className="text-muted-foreground text-xs">{copy.evidenceSelectionHint}</p>
                <div className="grid gap-3 sm:grid-cols-2">
                  <div className="space-y-1.5">
                    <Label htmlFor="blue-task-evidence-events">{copy.eventIDs}</Label>
                    <Input
                      id="blue-task-evidence-events"
                      inputMode="numeric"
                      value={eventIDsText}
                      onChange={(event) => setEventIDsText(event.target.value)}
                      placeholder={copy.evidenceIDsPlaceholder}
                      disabled={!caseID || analysisInProgress || running || stopping}
                    />
                  </div>
                  <div className="space-y-1.5">
                    <Label htmlFor="blue-task-evidence-alerts">{copy.alertIDs}</Label>
                    <Input
                      id="blue-task-evidence-alerts"
                      inputMode="numeric"
                      value={alertIDsText}
                      onChange={(event) => setAlertIDsText(event.target.value)}
                      placeholder={copy.evidenceIDsPlaceholder}
                      disabled={!caseID || analysisInProgress || running || stopping}
                    />
                  </div>
                </div>
              </div>
              {task.completed_at && (
                <p className="text-muted-foreground text-xs">
                  {copy.completedAt}: {blueDate(task.completed_at, locale)}
                </p>
              )}
              {analysisInProgress ? (
                <Button variant="destructive" size="sm" onClick={() => void stopAnalysis()} disabled={stopping}>
                  <Square />
                  {stopping ? copy.stopping : copy.stop}
                </Button>
              ) : (
                <Button
                  size="sm"
                  onClick={() => void runAnalysis()}
                  disabled={!caseID || running || stopping || saving || !title.trim()}
                >
                  <Bot />
                  {running ? copy.running : copy.run}
                </Button>
              )}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="result" className="mt-0">
          <Card>
            <CardHeader>
              <CardTitle>{copy.result}</CardTitle>
            </CardHeader>
            <CardContent>
              <form className="space-y-4" onSubmit={(event) => void save(event)}>
                <Label htmlFor="blue-task-detail-result">{copy.result}</Label>
                <Textarea
                  id="blue-task-detail-result"
                  value={result}
                  onChange={(event) => setResult(event.target.value)}
                  placeholder={copy.emptyResult}
                  rows={12}
                  disabled={analysisInProgress}
                />
                <Button type="submit" disabled={saving || running || analysisInProgress || !title.trim()}>
                  <Save />
                  {saving ? copy.saving : copy.save}
                </Button>
              </form>
              <p className="mt-4 text-muted-foreground text-xs">
                {copy.createdAt}: {blueDate(task.created_at, locale)} · {copy.updated}:{" "}
                {blueDate(task.updated_at, locale)}
              </p>
            </CardContent>
          </Card>
        </TabsContent>
      </div>
    </Tabs>
  );
}

export default function BlueTaskDetailPage() {
  return (
    <React.Suspense fallback={null}>
      <BlueTaskDetailInner />
    </React.Suspense>
  );
}

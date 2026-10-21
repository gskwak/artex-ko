"use client";

import * as React from "react";

import Link from "next/link";
import { useSearchParams } from "next/navigation";

import { ArrowLeft, Bot, ExternalLink, RefreshCw, Save } from "lucide-react";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { useActiveLocale } from "@/i18n/locale-store";
import { type BlueCase, blueApi } from "@/lib/blue-api";
import {
  type BlueTask,
  type BlueTaskPriority,
  type BlueTaskStatus,
  blueTaskApi,
  type UpdateBlueTask,
} from "@/lib/blue-task-api";
import { blueTaskPriorityLabel, blueTaskStatusLabel, useBlueTaskCopy } from "@/lib/blue-task-copy";

import { BlueError, BlueLoading, BluePageHeader, blueDate } from "../../_components/blue-ui";

const STATUSES: BlueTaskStatus[] = ["open", "in_progress", "completed", "blocked"];
const PRIORITIES: BlueTaskPriority[] = ["low", "medium", "high", "critical"];

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
  const [title, setTitle] = React.useState("");
  const [description, setDescription] = React.useState("");
  const [caseID, setCaseID] = React.useState("");
  const [priority, setPriority] = React.useState<BlueTaskPriority>("medium");
  const [status, setStatus] = React.useState<BlueTaskStatus>("open");
  const [result, setResult] = React.useState("");

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
    if (!analysisInProgress || !Number.isSafeInteger(id) || id <= 0) return;
    const timer = window.setInterval(async () => {
      try {
        syncTask(await blueTaskApi.get(id));
      } catch {
        // A transient poll failure does not stop the server-side analysis.
      }
    }, 3000);
    return () => window.clearInterval(timer);
  }, [analysisInProgress, id, syncTask]);

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
    if (!task || !caseID || analysisInProgress) return;
    setRunning(true);
    try {
      await blueTaskApi.update(task.id, formBody());
      const response = await blueTaskApi.run(task.id);
      syncTask(response.task);
      toast.success(copy.runQueued);
    } catch (cause) {
      toast.error(cause instanceof Error ? cause.message : copy.runFailed);
    } finally {
      setRunning(false);
    }
  }

  if (!Number.isSafeInteger(id) || id <= 0) {
    return <BlueError message={copy.invalidTask} />;
  }

  const caseName = cases.find((item) => item.id === task?.case_id)?.title;

  return (
    <div className="space-y-5">
      <Button asChild variant="ghost" size="sm" className="-ml-2">
        <Link href="/blue/tasks">
          <ArrowLeft />
          {copy.back}
        </Link>
      </Button>
      <BluePageHeader
        title={task ? `#${task.id} ${task.title}` : copy.title}
        description={copy.description}
        actions={
          <Button variant="outline" size="sm" onClick={() => void load()} disabled={loading}>
            <RefreshCw className={loading ? "animate-spin" : ""} />
            {copy.refresh}
          </Button>
        }
      />
      {error && <BlueError message={error} retry={() => void load()} retryLabel={copy.refresh} />}
      {loading && !task ? <BlueLoading label={copy.loading} /> : null}
      {task && (
        <>
          <div className="flex flex-wrap items-center gap-2">
            <Badge variant={task.status === "blocked" ? "destructive" : "secondary"}>
              {blueTaskStatusLabel(task.status, copy)}
            </Badge>
            <Badge variant="outline">{blueTaskPriorityLabel(task.priority, copy)}</Badge>
            <span className="text-muted-foreground text-xs">
              {copy.createdAt}: {blueDate(task.created_at, locale)} · {copy.updated}:{" "}
              {blueDate(task.updated_at, locale)}
            </span>
          </div>

          <div className="grid gap-5 xl:grid-cols-[minmax(0,2fr)_minmax(18rem,1fr)]">
            <Card>
              <CardHeader>
                <CardTitle>{copy.taskTitle}</CardTitle>
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
                      <select
                        id="blue-task-detail-case"
                        className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm"
                        value={caseID}
                        onChange={(event) => setCaseID(event.target.value)}
                        disabled={analysisInProgress}
                      >
                        <option value="">{copy.noCase}</option>
                        {task.case_id && !cases.some((item) => item.id === task.case_id) && (
                          <option value={task.case_id}>#{task.case_id}</option>
                        )}
                        {cases.map((item) => (
                          <option key={item.id} value={item.id}>
                            #{item.id} {item.title}
                          </option>
                        ))}
                      </select>
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
                  <div className="space-y-1.5">
                    <Label htmlFor="blue-task-detail-result">{copy.result}</Label>
                    <Textarea
                      id="blue-task-detail-result"
                      value={result}
                      onChange={(event) => setResult(event.target.value)}
                      placeholder={copy.emptyResult}
                      rows={10}
                      disabled={analysisInProgress}
                    />
                  </div>
                  <Button type="submit" disabled={saving || running || analysisInProgress || !title.trim()}>
                    <Save />
                    {saving ? copy.saving : copy.save}
                  </Button>
                </form>
              </CardContent>
            </Card>

            <div className="space-y-5">
              <Card>
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

              <Card>
                <CardHeader>
                  <CardTitle>{copy.latestAnalysis}</CardTitle>
                </CardHeader>
                <CardContent className="space-y-3 text-sm">
                  <p className="text-muted-foreground">{copy.analystScope}</p>
                  {analysisInProgress && <p role="status">{copy.analysisInProgress}</p>}
                  {task.completed_at && (
                    <p className="text-muted-foreground text-xs">
                      {copy.completedAt}: {blueDate(task.completed_at, locale)}
                    </p>
                  )}
                  {!caseID && <p className="text-muted-foreground text-xs">{copy.runNeedsCase}</p>}
                  <Button
                    size="sm"
                    onClick={() => void runAnalysis()}
                    disabled={!caseID || running || saving || analysisInProgress || !title.trim()}
                  >
                    <Bot />
                    {running ? copy.running : copy.run}
                  </Button>
                </CardContent>
              </Card>
            </div>
          </div>
        </>
      )}
    </div>
  );
}

export default function BlueTaskDetailPage() {
  return (
    <React.Suspense fallback={null}>
      <BlueTaskDetailInner />
    </React.Suspense>
  );
}

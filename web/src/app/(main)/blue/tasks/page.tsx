"use client";

import * as React from "react";

import Link from "next/link";
import { useRouter } from "next/navigation";

import { ArrowRight, Plus, RefreshCw, Search } from "lucide-react";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { useActiveLocale } from "@/i18n/locale-store";
import { type BlueCase, blueApi } from "@/lib/blue-api";
import { type BlueTask, type BlueTaskPriority, type BlueTaskStatus, blueTaskApi } from "@/lib/blue-task-api";
import { blueTaskPriorityLabel, blueTaskStatusLabel, useBlueTaskCopy } from "@/lib/blue-task-copy";

import { BlueEmpty, BlueError, BlueLoading, BluePageHeader, blueDate } from "../_components/blue-ui";

const PAGE_SIZE = 50;
const STATUSES: BlueTaskStatus[] = ["open", "in_progress", "completed", "blocked"];
const PRIORITIES: BlueTaskPriority[] = ["low", "medium", "high", "critical"];

export default function BlueTasksPage() {
  const router = useRouter();
  const copy = useBlueTaskCopy();
  const locale = useActiveLocale();
  const [tasks, setTasks] = React.useState<BlueTask[]>([]);
  const [cases, setCases] = React.useState<BlueCase[]>([]);
  const [loading, setLoading] = React.useState(true);
  const [loadingMore, setLoadingMore] = React.useState(false);
  const [hasMore, setHasMore] = React.useState(false);
  const [error, setError] = React.useState("");
  const [search, setSearch] = React.useState("");
  const [statusFilter, setStatusFilter] = React.useState<BlueTaskStatus | "all">("all");
  const [createOpen, setCreateOpen] = React.useState(false);
  const [creating, setCreating] = React.useState(false);
  const [title, setTitle] = React.useState("");
  const [description, setDescription] = React.useState("");
  const [caseID, setCaseID] = React.useState("");
  const [priority, setPriority] = React.useState<BlueTaskPriority>("medium");

  const load = React.useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const result = await blueTaskApi.list({
        status: statusFilter === "all" ? undefined : statusFilter,
        limit: PAGE_SIZE + 1,
      });
      setTasks(result.slice(0, PAGE_SIZE));
      setHasMore(result.length > PAGE_SIZE);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : copy.loadFailed);
    } finally {
      setLoading(false);
    }
  }, [copy.loadFailed, statusFilter]);

  React.useEffect(() => {
    void load();
  }, [load]);

  React.useEffect(() => {
    void blueApi
      .cases({ limit: 100 })
      .then(setCases)
      .catch(() => setCases([]));
  }, []);

  async function loadMore() {
    if (loadingMore || !hasMore) return;
    setLoadingMore(true);
    try {
      const result = await blueTaskApi.list({
        status: statusFilter === "all" ? undefined : statusFilter,
        limit: PAGE_SIZE + 1,
        offset: tasks.length,
      });
      setTasks((current) => {
        const known = new Set(current.map((task) => task.id));
        return [...current, ...result.slice(0, PAGE_SIZE).filter((task) => !known.has(task.id))];
      });
      setHasMore(result.length > PAGE_SIZE);
    } catch (cause) {
      toast.error(cause instanceof Error ? cause.message : copy.loadFailed);
    } finally {
      setLoadingMore(false);
    }
  }

  async function createTask(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!title.trim()) return;
    setCreating(true);
    try {
      const task = await blueTaskApi.create({
        title: title.trim(),
        description: description.trim(),
        priority,
        ...(caseID ? { case_id: Number(caseID) } : {}),
      });
      setCreateOpen(false);
      setTitle("");
      setDescription("");
      setCaseID("");
      setPriority("medium");
      toast.success(copy.created);
      router.push(`/blue/tasks/detail?id=${task.id}`);
    } catch (cause) {
      toast.error(cause instanceof Error ? cause.message : copy.saveFailed);
    } finally {
      setCreating(false);
    }
  }

  const query = search.trim().toLocaleLowerCase();
  const visible = tasks.filter((task) =>
    query ? `${task.id} ${task.title} ${task.description} ${task.result}`.toLocaleLowerCase().includes(query) : true,
  );
  const caseNames = new Map(cases.map((item) => [item.id, item.title]));

  return (
    <div className="space-y-5">
      <BluePageHeader
        title={copy.title}
        description={copy.description}
        actions={
          <>
            <Button variant="outline" size="sm" onClick={() => void load()} disabled={loading}>
              <RefreshCw className={loading ? "animate-spin" : ""} />
              {copy.refresh}
            </Button>
            <Dialog open={createOpen} onOpenChange={setCreateOpen}>
              <DialogTrigger asChild>
                <Button size="sm">
                  <Plus />
                  {copy.createTask}
                </Button>
              </DialogTrigger>
              <DialogContent>
                <DialogHeader>
                  <DialogTitle>{copy.createTask}</DialogTitle>
                  <DialogDescription>{copy.createDescription}</DialogDescription>
                </DialogHeader>
                <form className="space-y-4" onSubmit={(event) => void createTask(event)}>
                  <div className="space-y-1.5">
                    <Label htmlFor="blue-task-title">{copy.taskTitle}</Label>
                    <Input
                      id="blue-task-title"
                      value={title}
                      onChange={(event) => setTitle(event.target.value)}
                      maxLength={240}
                      required
                      autoFocus
                    />
                  </div>
                  <div className="space-y-1.5">
                    <Label htmlFor="blue-task-description">{copy.taskDescription}</Label>
                    <Textarea
                      id="blue-task-description"
                      value={description}
                      onChange={(event) => setDescription(event.target.value)}
                      rows={4}
                    />
                  </div>
                  <div className="grid gap-4 sm:grid-cols-2">
                    <div className="space-y-1.5">
                      <Label htmlFor="blue-task-case">{copy.case}</Label>
                      <select
                        id="blue-task-case"
                        className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm"
                        value={caseID}
                        onChange={(event) => setCaseID(event.target.value)}
                      >
                        <option value="">{copy.noCase}</option>
                        {cases.map((item) => (
                          <option key={item.id} value={item.id}>
                            #{item.id} {item.title}
                          </option>
                        ))}
                      </select>
                    </div>
                    <div className="space-y-1.5">
                      <Label htmlFor="blue-task-priority">{copy.priority}</Label>
                      <select
                        id="blue-task-priority"
                        className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm"
                        value={priority}
                        onChange={(event) => setPriority(event.target.value as BlueTaskPriority)}
                      >
                        {PRIORITIES.map((value) => (
                          <option key={value} value={value}>
                            {blueTaskPriorityLabel(value, copy)}
                          </option>
                        ))}
                      </select>
                    </div>
                  </div>
                  <DialogFooter>
                    <Button type="button" variant="outline" onClick={() => setCreateOpen(false)}>
                      {copy.cancel}
                    </Button>
                    <Button type="submit" disabled={creating || !title.trim()}>
                      {creating ? copy.creating : copy.create}
                    </Button>
                  </DialogFooter>
                </form>
              </DialogContent>
            </Dialog>
          </>
        }
      />

      <div className="flex flex-wrap items-center gap-2">
        <div className="relative min-w-48 flex-1">
          <Search className="absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            aria-label={copy.search}
            placeholder={copy.search}
            className="pl-9"
            value={search}
            onChange={(event) => setSearch(event.target.value)}
          />
        </div>
        <select
          aria-label={copy.status}
          className="h-9 rounded-md border border-input bg-background px-3 text-sm"
          value={statusFilter}
          onChange={(event) => setStatusFilter(event.target.value as BlueTaskStatus | "all")}
        >
          <option value="all">{copy.allStatuses}</option>
          {STATUSES.map((value) => (
            <option key={value} value={value}>
              {blueTaskStatusLabel(value, copy)}
            </option>
          ))}
        </select>
      </div>

      {error && <BlueError message={error} retry={() => void load()} retryLabel={copy.refresh} />}
      {loading && tasks.length === 0 ? <BlueLoading label={copy.loading} /> : null}
      {!loading && visible.length === 0 ? <BlueEmpty message={copy.noTasks} /> : null}
      {visible.length > 0 && (
        <div className="grid gap-3 lg:grid-cols-2 2xl:grid-cols-3">
          {visible.map((task) => (
            <Card key={task.id} className="transition-colors hover:border-primary/50">
              <CardContent className="space-y-3 p-4">
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <Link
                      className="line-clamp-2 font-semibold hover:underline"
                      href={`/blue/tasks/detail?id=${task.id}`}
                    >
                      #{task.id} {task.title}
                    </Link>
                    <p className="mt-1 line-clamp-2 text-muted-foreground text-sm">
                      {task.description || task.result || copy.createDescription}
                    </p>
                  </div>
                  <Button asChild variant="ghost" size="icon-sm" aria-label={`${copy.taskTitle} #${task.id}`}>
                    <Link href={`/blue/tasks/detail?id=${task.id}`}>
                      <ArrowRight />
                    </Link>
                  </Button>
                </div>
                <div className="flex flex-wrap items-center gap-2">
                  <Badge variant={task.status === "blocked" ? "destructive" : "secondary"}>
                    {blueTaskStatusLabel(task.status, copy)}
                  </Badge>
                  <Badge variant="outline">{blueTaskPriorityLabel(task.priority, copy)}</Badge>
                  {task.case_id && (
                    <Badge variant="outline">
                      {copy.case} #{task.case_id}
                      {caseNames.get(task.case_id) ? ` · ${caseNames.get(task.case_id)}` : ""}
                    </Badge>
                  )}
                </div>
                <p className="text-muted-foreground text-xs">
                  {copy.updated}: {blueDate(task.updated_at, locale)}
                </p>
              </CardContent>
            </Card>
          ))}
        </div>
      )}
      {hasMore && (
        <div className="flex justify-center">
          <Button variant="outline" onClick={() => void loadMore()} disabled={loadingMore}>
            {loadingMore ? copy.loadingMore : copy.loadMore}
          </Button>
        </div>
      )}
    </div>
  );
}

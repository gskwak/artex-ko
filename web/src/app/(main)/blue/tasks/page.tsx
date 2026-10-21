"use client";

import * as React from "react";

import Link from "next/link";
import { useRouter } from "next/navigation";

import { ArrowDown, ArrowRight, ArrowUp, ArrowUpDown, Plus, RefreshCw, Search, X } from "lucide-react";
import { toast } from "sonner";

import { TablePagination } from "@/components/table-pagination";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from "@/components/ui/sheet";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";
import { useActiveLocale } from "@/i18n/locale-store";
import { type BlueCase, blueApi } from "@/lib/blue-api";
import { type BlueTask, type BlueTaskPriority, type BlueTaskStatus, blueTaskApi } from "@/lib/blue-task-api";
import { blueTaskPriorityLabel, blueTaskStatusLabel, useBlueTaskCopy } from "@/lib/blue-task-copy";

import { BlueError, BlueLoading, blueDate } from "../_components/blue-ui";

const BATCH_SIZE = 50;
const STATUSES: BlueTaskStatus[] = ["open", "in_progress", "completed", "blocked"];
const PRIORITIES: BlueTaskPriority[] = ["low", "medium", "high", "critical"];

type SortField = "id" | "title" | "status" | "priority" | "updated";
type SortDirection = "asc" | "desc";

function SortHead({
  field,
  label,
  activeField,
  direction,
  onSort,
  className,
}: {
  field: SortField;
  label: string;
  activeField: SortField;
  direction: SortDirection;
  onSort: (field: SortField) => void;
  className?: string;
}) {
  const active = field === activeField;
  let ariaSort: React.AriaAttributes["aria-sort"] = "none";
  let icon = <ArrowUpDown className="size-3.5 opacity-40 group-hover/sort:opacity-100" aria-hidden="true" />;
  if (active) {
    ariaSort = direction === "asc" ? "ascending" : "descending";
    icon =
      direction === "asc" ? (
        <ArrowUp className="size-3.5" aria-hidden="true" />
      ) : (
        <ArrowDown className="size-3.5" aria-hidden="true" />
      );
  }
  return (
    <TableHead className={className} aria-sort={ariaSort}>
      <button
        type="button"
        className="group/sort inline-flex w-full items-center gap-1 outline-none focus-visible:underline"
        onClick={() => onSort(field)}
      >
        {label}
        {icon}
      </button>
    </TableHead>
  );
}

export default function BlueTasksPage() {
  const router = useRouter();
  const copy = useBlueTaskCopy();
  const locale = useActiveLocale();
  const requestSequence = React.useRef(0);
  const [tasks, setTasks] = React.useState<BlueTask[]>([]);
  const [cases, setCases] = React.useState<BlueCase[]>([]);
  const [loading, setLoading] = React.useState(true);
  const [loadingMore, setLoadingMore] = React.useState(false);
  const [hasMore, setHasMore] = React.useState(false);
  const [error, setError] = React.useState("");
  const [search, setSearch] = React.useState("");
  const [query, setQuery] = React.useState("");
  const [statusFilter, setStatusFilter] = React.useState<BlueTaskStatus | "all">("all");
  const [caseFilter, setCaseFilter] = React.useState("");
  const [sortField, setSortField] = React.useState<SortField>("updated");
  const [sortDirection, setSortDirection] = React.useState<SortDirection>("desc");
  const [page, setPage] = React.useState(1);
  const [pageSize, setPageSize] = React.useState(20);
  const [createOpen, setCreateOpen] = React.useState(false);
  const [creating, setCreating] = React.useState(false);
  const [title, setTitle] = React.useState("");
  const [description, setDescription] = React.useState("");
  const [caseID, setCaseID] = React.useState("");
  const [priority, setPriority] = React.useState<BlueTaskPriority>("medium");

  React.useEffect(() => {
    const timer = window.setTimeout(() => setQuery(search.trim()), 250);
    return () => window.clearTimeout(timer);
  }, [search]);

  const taskQuery = React.useCallback(
    (offset: number) => ({
      q: query,
      status: statusFilter === "all" ? undefined : statusFilter,
      case_id:
        caseFilter && Number.isSafeInteger(Number(caseFilter)) && Number(caseFilter) > 0
          ? Number(caseFilter)
          : undefined,
      sort: sortField,
      order: sortDirection,
      limit: BATCH_SIZE + 1,
      offset,
    }),
    [caseFilter, query, sortDirection, sortField, statusFilter],
  );

  const load = React.useCallback(async () => {
    const sequence = ++requestSequence.current;
    setLoading(true);
    setError("");
    try {
      const result = await blueTaskApi.list(taskQuery(0));
      if (sequence !== requestSequence.current) return;
      setTasks(result.slice(0, BATCH_SIZE));
      setHasMore(result.length > BATCH_SIZE);
    } catch (cause) {
      if (sequence !== requestSequence.current) return;
      setError(cause instanceof Error ? cause.message : copy.loadFailed);
    } finally {
      if (sequence === requestSequence.current) setLoading(false);
    }
  }, [copy.loadFailed, taskQuery]);

  React.useEffect(() => {
    setTasks([]);
    setPage(1);
    void load();
  }, [load]);

  React.useEffect(() => {
    void blueApi
      .cases({ limit: 100 })
      .then(setCases)
      .catch(() => setCases([]));
  }, []);

  async function loadMore() {
    if (loading || loadingMore || !hasMore) return;
    const sequence = requestSequence.current;
    setLoadingMore(true);
    try {
      const result = await blueTaskApi.list(taskQuery(tasks.length));
      if (sequence !== requestSequence.current) return;
      setTasks((current) => {
        const known = new Set(current.map((task) => task.id));
        return [...current, ...result.slice(0, BATCH_SIZE).filter((task) => !known.has(task.id))];
      });
      setHasMore(result.length > BATCH_SIZE);
    } catch (cause) {
      if (sequence === requestSequence.current) {
        toast.error(cause instanceof Error ? cause.message : copy.loadFailed);
      }
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

  const caseNames = React.useMemo(() => new Map(cases.map((item) => [item.id, item.title])), [cases]);
  const shown = tasks.slice((page - 1) * pageSize, page * pageSize);

  function sortBy(field: SortField) {
    if (field === sortField) {
      setSortDirection((current) => (current === "asc" ? "desc" : "asc"));
    } else {
      setSortField(field);
      setSortDirection(field === "updated" || field === "id" ? "desc" : "asc");
    }
    setPage(1);
  }

  return (
    <div className="space-y-4">
      <h1 className="sr-only">{copy.title}</h1>
      <Card>
        <CardContent className="flex flex-col gap-4 px-0 pt-6">
          <div className="flex flex-wrap items-center gap-2 px-4 lg:px-6">
            <div className="relative w-full sm:max-w-xs">
              <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
              <Input
                aria-label={copy.search}
                placeholder={copy.search}
                className="pr-8 pl-8"
                value={search}
                onChange={(event) => {
                  setSearch(event.target.value);
                  setPage(1);
                }}
              />
              {search && (
                <button
                  type="button"
                  className="absolute top-1/2 right-2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                  aria-label={copy.clearSearch}
                  onClick={() => {
                    setSearch("");
                    setPage(1);
                  }}
                >
                  <X className="size-4" />
                </button>
              )}
            </div>
            <Select value={statusFilter} onValueChange={(value) => setStatusFilter(value as BlueTaskStatus | "all")}>
              <SelectTrigger className="w-36" aria-label={copy.status}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">{copy.allStatuses}</SelectItem>
                {STATUSES.map((value) => (
                  <SelectItem key={value} value={value}>
                    {blueTaskStatusLabel(value, copy)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Input
              type="number"
              min="1"
              step="1"
              list="blue-task-filter-cases"
              value={caseFilter}
              onChange={(event) => {
                const value = event.target.value;
                if (!value || (/^[1-9]\d*$/.test(value) && Number.isSafeInteger(Number(value)))) setCaseFilter(value);
              }}
              className="w-44"
              aria-label={copy.case}
              placeholder={copy.allCases}
            />
            <datalist id="blue-task-filter-cases">
              {cases.map((item) => (
                <option key={item.id} value={item.id} label={item.title} />
              ))}
            </datalist>
            <span className="text-muted-foreground text-xs tabular-nums">
              {tasks.length}
              {hasMore ? "+" : ""}
            </span>
            <div className="ml-auto flex items-center gap-2">
              <Button variant="outline" size="sm" onClick={() => void load()} disabled={loading}>
                <RefreshCw className={loading ? "animate-spin" : ""} />
                {copy.refresh}
              </Button>
              <Sheet open={createOpen} onOpenChange={setCreateOpen}>
                <SheetTrigger asChild>
                  <Button size="sm">
                    <Plus />
                    {copy.createTask}
                  </Button>
                </SheetTrigger>
                <SheetContent className="h-full w-full gap-0 overflow-hidden p-0 data-[side=right]:sm:max-w-xl">
                  <form className="flex min-h-0 flex-1 flex-col" onSubmit={(event) => void createTask(event)}>
                    <SheetHeader className="border-b p-6">
                      <SheetTitle>{copy.createTask}</SheetTitle>
                      <SheetDescription>{copy.createDescription}</SheetDescription>
                    </SheetHeader>
                    <div className="min-h-0 flex-1 space-y-5 overflow-y-auto p-6">
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
                          rows={6}
                        />
                      </div>
                      <div className="grid gap-4 sm:grid-cols-2">
                        <div className="space-y-1.5">
                          <Label htmlFor="blue-task-case">{copy.case}</Label>
                          <Input
                            id="blue-task-case"
                            type="number"
                            min="1"
                            step="1"
                            list="blue-task-create-cases"
                            value={caseID}
                            onChange={(event) => {
                              const value = event.target.value;
                              if (!value || (/^[1-9]\d*$/.test(value) && Number.isSafeInteger(Number(value))))
                                setCaseID(value);
                            }}
                            placeholder={copy.noCase}
                          />
                          <datalist id="blue-task-create-cases">
                            {cases.map((item) => (
                              <option key={item.id} value={item.id} label={item.title} />
                            ))}
                          </datalist>
                        </div>
                        <div className="space-y-1.5">
                          <Label htmlFor="blue-task-priority">{copy.priority}</Label>
                          <Select value={priority} onValueChange={(value) => setPriority(value as BlueTaskPriority)}>
                            <SelectTrigger id="blue-task-priority" className="w-full">
                              <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                              {PRIORITIES.map((value) => (
                                <SelectItem key={value} value={value}>
                                  {blueTaskPriorityLabel(value, copy)}
                                </SelectItem>
                              ))}
                            </SelectContent>
                          </Select>
                        </div>
                      </div>
                    </div>
                    <SheetFooter className="mt-0 flex-row justify-end border-t p-4">
                      <SheetClose asChild>
                        <Button type="button" variant="outline" disabled={creating}>
                          {copy.cancel}
                        </Button>
                      </SheetClose>
                      <Button type="submit" disabled={creating || !title.trim()}>
                        {creating ? copy.creating : copy.create}
                      </Button>
                    </SheetFooter>
                  </form>
                </SheetContent>
              </Sheet>
            </div>
          </div>

          {error && (
            <div className="px-4 lg:px-6">
              <BlueError message={error} retry={() => void load()} retryLabel={copy.refresh} />
            </div>
          )}
          {loading && tasks.length === 0 ? <BlueLoading label={copy.loading} /> : null}
          {!loading && tasks.length === 0 ? (
            <div className="mx-4 flex items-center justify-center rounded-lg border border-dashed py-20 text-muted-foreground text-sm lg:mx-6">
              {tasks.length === 0 && !query && statusFilter === "all" && !caseFilter ? copy.noTasks : copy.noMatch}
            </div>
          ) : null}
          {tasks.length > 0 && (
            <Table className="**:data-[slot='table-cell']:px-4 **:data-[slot='table-head']:px-4">
              <TableHeader className="[&_tr]:border-t">
                <TableRow>
                  <SortHead field="id" label="ID" activeField={sortField} direction={sortDirection} onSort={sortBy} />
                  <SortHead
                    field="title"
                    label={copy.taskTitle}
                    activeField={sortField}
                    direction={sortDirection}
                    onSort={sortBy}
                  />
                  <TableHead>{copy.taskDescription}</TableHead>
                  <TableHead>{copy.case}</TableHead>
                  <SortHead
                    field="status"
                    label={copy.status}
                    activeField={sortField}
                    direction={sortDirection}
                    onSort={sortBy}
                  />
                  <SortHead
                    field="priority"
                    label={copy.priority}
                    activeField={sortField}
                    direction={sortDirection}
                    onSort={sortBy}
                  />
                  <SortHead
                    field="updated"
                    label={copy.updated}
                    activeField={sortField}
                    direction={sortDirection}
                    onSort={sortBy}
                  />
                  <TableHead className="text-right">{copy.actions}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {shown.map((task) => (
                  <TableRow key={task.id}>
                    <TableCell className="font-mono text-muted-foreground text-xs">{task.id}</TableCell>
                    <TableCell className="max-w-52 font-medium">
                      <Link
                        href={`/blue/tasks/detail?id=${task.id}`}
                        className="block truncate hover:underline"
                        title={task.title}
                      >
                        {task.title}
                      </Link>
                    </TableCell>
                    <TableCell
                      className="max-w-64 truncate text-muted-foreground"
                      title={task.description || task.result}
                    >
                      {task.description || task.result || "—"}
                    </TableCell>
                    <TableCell className="max-w-52 truncate">
                      {task.case_id ? `#${task.case_id} ${caseNames.get(task.case_id) ?? ""}` : copy.noCase}
                    </TableCell>
                    <TableCell>
                      <Badge variant={task.status === "blocked" ? "destructive" : "secondary"}>
                        {blueTaskStatusLabel(task.status, copy)}
                      </Badge>
                    </TableCell>
                    <TableCell>
                      <Badge variant="outline">{blueTaskPriorityLabel(task.priority, copy)}</Badge>
                    </TableCell>
                    <TableCell className="text-muted-foreground text-xs">{blueDate(task.updated_at, locale)}</TableCell>
                    <TableCell className="text-right">
                      <Button asChild variant="ghost" size="icon-sm" aria-label={`${copy.taskTitle} #${task.id}`}>
                        <Link href={`/blue/tasks/detail?id=${task.id}`}>
                          <ArrowRight />
                        </Link>
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
          <TablePagination
            page={page}
            pageSize={pageSize}
            total={tasks.length}
            onPageChange={setPage}
            onPageSizeChange={(value) => {
              setPageSize(value);
              setPage(1);
            }}
          />
          {hasMore && (
            <div className="flex justify-center border-t px-4 py-3">
              <Button variant="outline" onClick={() => void loadMore()} disabled={loading || loadingMore}>
                {loadingMore ? copy.loadingMore : copy.loadMore}
              </Button>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

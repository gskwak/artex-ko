"use client";

import * as React from "react";

import Link from "next/link";

import {
  Activity,
  ArrowUpRight,
  Bell,
  ClipboardList,
  Clock3,
  type LucideIcon,
  RefreshCw,
  ShieldCheck,
  ShieldQuestion,
  Target,
} from "lucide-react";
import { Bar, BarChart, CartesianGrid, XAxis, YAxis } from "recharts";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { type ChartConfig, ChartContainer, ChartTooltip, ChartTooltipContent } from "@/components/ui/chart";
import type { Locale } from "@/i18n/config";
import { useActiveLocale } from "@/i18n/locale-store";
import { type BlueAlert, type BlueCase, type BlueEvent, type BlueOverview, blueApi } from "@/lib/blue-api";
import { useBlueCopy } from "@/lib/blue-copy";
import { type BlueTask, blueTaskApi } from "@/lib/blue-task-api";
import { blueTaskPriorityLabel, blueTaskStatusLabel, useBlueTaskCopy } from "@/lib/blue-task-copy";

import {
  BlueAlertStatusBadge,
  BlueCaseStatusBadge,
  BlueError,
  BlueLoading,
  BlueSeverityBadge,
  blueDate,
} from "../_components/blue-ui";

const SAMPLE_LIMIT = 100;
const en = {
  activeTasks: "Active tasks",
  evidenceTrend: "Evidence activity",
  trendHint: "Daily activity from the latest 100 events and alerts, not all-time totals.",
  recentEvents: "Recent events",
  recentTasks: "Recent tasks",
  eventSources: "Event sources",
  operations: "Detection and response",
  taskColumn: "Task",
  statusColumn: "Status",
  priorityColumn: "Priority",
  caseColumn: "Case",
  updatedColumn: "Last activity",
  sevenDays: "7 days",
  thirtyDays: "30 days",
  newAlertRatio: "New alerts",
  openCaseRatio: "Open cases",
  noTasks: "No defense tasks yet.",
  unknownSource: "Unknown source",
  allTime: "All-time total",
  samples: (events: number, alerts: number) => `${events} events · ${alerts} alerts sampled`,
  sample: (count: number) => `Latest ${count} records`,
  ofTotal: (total: number) => `of ${total} total`,
};
const ko: typeof en = {
  activeTasks: "진행 중인 작업",
  evidenceTrend: "증거 활동 추이",
  trendHint: "최근 이벤트와 경보 각각 최대 100건의 일별 추이입니다. 전체 기간 합계가 아닙니다.",
  recentEvents: "최근 이벤트",
  recentTasks: "최근 작업",
  eventSources: "이벤트 출처",
  operations: "탐지와 대응",
  taskColumn: "작업",
  statusColumn: "상태",
  priorityColumn: "우선순위",
  caseColumn: "사건",
  updatedColumn: "최근 활동",
  sevenDays: "7일",
  thirtyDays: "30일",
  newAlertRatio: "새 경보",
  openCaseRatio: "진행 중인 사건",
  noTasks: "아직 방어 작업이 없습니다.",
  unknownSource: "출처 미상",
  allTime: "전체 기간 합계",
  samples: (events: number, alerts: number) => `이벤트 ${events}건 · 경보 ${alerts}건 표본`,
  sample: (count: number) => `최근 ${count}건 기준`,
  ofTotal: (total: number) => `전체 ${total}건 중`,
};
const zh: typeof en = {
  activeTasks: "进行中的任务",
  evidenceTrend: "证据活动趋势",
  trendHint: "最近最多 100 条事件和告警的每日趋势，并非全量总数。",
  recentEvents: "近期事件",
  recentTasks: "近期任务",
  eventSources: "事件来源",
  operations: "检测与响应",
  taskColumn: "任务",
  statusColumn: "状态",
  priorityColumn: "优先级",
  caseColumn: "案件",
  updatedColumn: "最近活动",
  sevenDays: "7 天",
  thirtyDays: "30 天",
  newAlertRatio: "新告警",
  openCaseRatio: "未结案件",
  noTasks: "尚无防御任务。",
  unknownSource: "未知来源",
  allTime: "历史总数",
  samples: (events: number, alerts: number) => `事件 ${events} 条 · 告警 ${alerts} 条样本`,
  sample: (count: number) => `最近 ${count} 条记录`,
  ofTotal: (total: number) => `共 ${total} 条`,
};
const es: typeof en = {
  activeTasks: "Tareas activas",
  evidenceTrend: "Actividad de evidencia",
  trendHint: "Tendencia diaria de hasta los 100 eventos y alertas más recientes; no son totales históricos.",
  recentEvents: "Eventos recientes",
  recentTasks: "Tareas recientes",
  eventSources: "Fuentes de eventos",
  operations: "Detección y respuesta",
  taskColumn: "Tarea",
  statusColumn: "Estado",
  priorityColumn: "Prioridad",
  caseColumn: "Caso",
  updatedColumn: "Última actividad",
  sevenDays: "7 días",
  thirtyDays: "30 días",
  newAlertRatio: "Alertas nuevas",
  openCaseRatio: "Casos abiertos",
  noTasks: "Todavía no hay tareas de defensa.",
  unknownSource: "Origen desconocido",
  allTime: "Total histórico",
  samples: (events: number, alerts: number) => `${events} eventos · ${alerts} alertas muestreadas`,
  sample: (count: number) => `Últimos ${count} registros`,
  ofTotal: (total: number) => `de ${total} en total`,
};
const textByLocale: Record<Locale, typeof en> = { en, ko, zh, es };

function relativeDate(value: string | undefined, locale: Locale): string {
  if (!value) return "—";
  const elapsed = Date.now() - new Date(value).getTime();
  if (!Number.isFinite(elapsed)) return blueDate(value, locale);
  const seconds = Math.max(0, Math.floor(elapsed / 1000));
  const rtf = new Intl.RelativeTimeFormat(locale, { numeric: "auto" });
  if (seconds < 60) return rtf.format(-seconds, "second");
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return rtf.format(-minutes, "minute");
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return rtf.format(-hours, "hour");
  return rtf.format(-Math.floor(hours / 24), "day");
}

function dateKey(date: Date): string {
  return [
    date.getFullYear(),
    String(date.getMonth() + 1).padStart(2, "0"),
    String(date.getDate()).padStart(2, "0"),
  ].join("-");
}

function percentage(part: number, total: number): number {
  return total > 0 ? Math.min(100, Math.round((part / total) * 100)) : 0;
}

function SectionHeading({
  icon: Icon,
  title,
  href,
  linkLabel,
  note,
}: {
  icon: LucideIcon;
  title: string;
  href?: string;
  linkLabel?: string;
  note?: string;
}) {
  return (
    <div className="mb-3 flex items-center justify-between gap-2">
      <div className="flex min-w-0 items-center gap-1.5 font-semibold text-xs">
        <Icon className="size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
        <span className="truncate">{title}</span>
      </div>
      {href && (
        <Link
          href={href}
          className="flex shrink-0 items-center gap-0.5 text-[10px] text-muted-foreground hover:text-foreground"
        >
          {linkLabel}
          <ArrowUpRight className="size-3" aria-hidden="true" />
        </Link>
      )}
      {!href && note && <span className="shrink-0 text-[10px] text-muted-foreground">{note}</span>}
    </div>
  );
}

function StatCard({
  icon: Icon,
  label,
  value,
  detail,
}: {
  icon: LucideIcon;
  label: string;
  value: number;
  detail: string;
}) {
  return (
    <Card className="gap-1">
      <CardHeader className="pb-0">
        <div className="flex items-center gap-1.5 text-[10px] text-muted-foreground">
          <Icon className="size-3" aria-hidden="true" />
          {label}
        </div>
        <div className="font-semibold text-2xl tabular-nums">{value.toLocaleString()}</div>
      </CardHeader>
      <CardContent className="text-[10px] text-muted-foreground">{detail}</CardContent>
    </Card>
  );
}

export default function BlueDashboardPage() {
  const copy = useBlueCopy();
  const taskCopy = useBlueTaskCopy();
  const locale = useActiveLocale();
  const labels = textByLocale[locale];
  const [overview, setOverview] = React.useState<BlueOverview | null>(null);
  const [cases, setCases] = React.useState<BlueCase[]>([]);
  const [alerts, setAlerts] = React.useState<BlueAlert[]>([]);
  const [events, setEvents] = React.useState<BlueEvent[]>([]);
  const [tasks, setTasks] = React.useState<BlueTask[]>([]);
  const [days, setDays] = React.useState<7 | 30>(7);
  const [loading, setLoading] = React.useState(true);
  const [error, setError] = React.useState("");
  const mounted = React.useRef(false);
  const inFlight = React.useRef(false);

  const load = React.useCallback(
    async (showLoading: boolean) => {
      if (inFlight.current) return;
      inFlight.current = true;
      if (showLoading && mounted.current) setLoading(true);
      try {
        const [summary, nextCases, nextAlerts, nextEvents, nextTasks] = await Promise.allSettled([
          blueApi.overview(),
          blueApi.cases({ limit: 6 }),
          blueApi.alerts({ limit: SAMPLE_LIMIT }),
          blueApi.events({ limit: SAMPLE_LIMIT }),
          blueTaskApi.list({ limit: SAMPLE_LIMIT }),
        ]);
        if (!mounted.current) return;
        if (summary.status === "fulfilled") setOverview(summary.value);
        if (nextCases.status === "fulfilled") setCases(nextCases.value);
        if (nextAlerts.status === "fulfilled") setAlerts(nextAlerts.value);
        if (nextEvents.status === "fulfilled") setEvents(nextEvents.value);
        if (nextTasks.status === "fulfilled") setTasks(nextTasks.value);
        const hasFailure = [summary, nextCases, nextAlerts, nextEvents, nextTasks].some(
          (result) => result.status === "rejected",
        );
        setError(hasFailure ? copy.loadFailed : "");
      } catch (cause) {
        if (mounted.current) setError(cause instanceof Error ? cause.message : copy.loadFailed);
      } finally {
        inFlight.current = false;
        if (mounted.current) setLoading(false);
      }
    },
    [copy.loadFailed],
  );

  React.useEffect(() => {
    mounted.current = true;
    void load(true);
    const timer = window.setInterval(() => void load(false), 15_000);
    return () => {
      mounted.current = false;
      window.clearInterval(timer);
    };
  }, [load]);

  const trend = React.useMemo(() => {
    const today = new Date();
    today.setHours(0, 0, 0, 0);
    const buckets = Array.from({ length: days }, (_, index) => {
      const day = new Date(today);
      day.setDate(day.getDate() - (days - index - 1));
      return { date: dateKey(day), events: 0, alerts: 0 };
    });
    const byDate = new Map(buckets.map((bucket) => [bucket.date, bucket]));
    for (const event of events) {
      const date = new Date(event.occurred_at);
      if (!Number.isNaN(date.getTime())) {
        const bucket = byDate.get(dateKey(date));
        if (bucket) bucket.events += 1;
      }
    }
    for (const alert of alerts) {
      const date = new Date(alert.created_at);
      if (!Number.isNaN(date.getTime())) {
        const bucket = byDate.get(dateKey(date));
        if (bucket) bucket.alerts += 1;
      }
    }
    return buckets;
  }, [alerts, days, events]);

  const sourceCounts = React.useMemo(() => {
    const counts = new Map<string, number>();
    for (const event of events) {
      const source = event.source?.trim() || labels.unknownSource;
      counts.set(source, (counts.get(source) ?? 0) + 1);
    }
    return Array.from(counts, ([source, count]) => ({ source, count }))
      .sort((a, b) => b.count - a.count)
      .slice(0, 5);
  }, [events, labels.unknownSource]);
  const sortedTasks = React.useMemo(
    () => [...tasks].sort((a, b) => Date.parse(b.updated_at) - Date.parse(a.updated_at)).slice(0, 6),
    [tasks],
  );
  const activeTaskCount = tasks.filter((task) => task.status === "in_progress").length;
  const chartConfig = React.useMemo(
    () =>
      ({
        events: { label: copy.totalEvents, color: "#2563eb" },
        alerts: { label: copy.alerts, color: "#60a5fa" },
      }) satisfies ChartConfig,
    [copy.alerts, copy.totalEvents],
  );

  return (
    <div className="flex flex-1 flex-col gap-4 pb-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="font-semibold text-lg tracking-tight">{copy.dashboard}</h1>
          <p className="text-muted-foreground text-xs">{copy.dashboardDescription}</p>
        </div>
        <Button variant="outline" size="sm" onClick={() => void load(true)} disabled={loading}>
          <RefreshCw className={loading ? "animate-spin" : ""} aria-hidden="true" />
          {copy.refresh}
        </Button>
      </div>

      {error && <BlueError message={error} retry={() => void load(true)} retryLabel={copy.refresh} />}
      {loading && !overview && <BlueLoading label={copy.loading} />}

      {overview && (
        <>
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5">
            <StatCard
              icon={Target}
              label={labels.activeTasks}
              value={activeTaskCount}
              detail={labels.sample(tasks.length)}
            />
            <StatCard
              icon={ClipboardList}
              label={copy.openCases}
              value={overview.open_case_count}
              detail={labels.ofTotal(overview.case_count)}
            />
            <StatCard icon={Activity} label={copy.totalEvents} value={overview.event_count} detail={labels.allTime} />
            <StatCard
              icon={Bell}
              label={copy.newAlerts}
              value={overview.new_alert_count}
              detail={labels.ofTotal(overview.alert_count)}
            />
            <StatCard
              icon={ShieldQuestion}
              label={copy.pendingActions}
              value={overview.pending_action_count}
              detail={labels.operations}
            />
          </div>

          <Card className="p-4">
            <SectionHeading
              icon={Activity}
              title={labels.evidenceTrend}
              note={labels.samples(events.length, alerts.length)}
            />
            <div className="grid grid-cols-1 gap-6 lg:grid-cols-[220px_1fr]">
              <div className="flex flex-col gap-4">
                <div>
                  <div className="text-[10px] text-muted-foreground">{copy.totalEvents}</div>
                  <div className="mt-0.5 font-bold text-3xl tabular-nums tracking-tight">
                    {overview.event_count.toLocaleString()}
                  </div>
                  <div className="mt-0.5 text-[10px] text-muted-foreground">{labels.trendHint}</div>
                </div>
                {[
                  {
                    label: labels.newAlertRatio,
                    count: overview.new_alert_count,
                    total: overview.alert_count,
                    color: "bg-sky-500",
                  },
                  {
                    label: labels.openCaseRatio,
                    count: overview.open_case_count,
                    total: overview.case_count,
                    color: "bg-blue-600",
                  },
                ].map((row) => (
                  <div key={row.label}>
                    <div className="mb-1 flex items-center justify-between text-[10px]">
                      <span className="text-muted-foreground">{row.label}</span>
                      <span className="font-mono font-semibold tabular-nums">
                        {row.count}/{row.total}
                      </span>
                    </div>
                    <div className="h-1.5 overflow-hidden rounded-full bg-muted">
                      <div
                        className={`h-full rounded-full ${row.color}`}
                        style={{ width: `${percentage(row.count, row.total)}%` }}
                      />
                    </div>
                  </div>
                ))}
              </div>
              <div className="flex min-h-0 flex-col">
                <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
                  <div className="flex gap-3 text-[9px] text-muted-foreground">
                    {(["events", "alerts"] as const).map((key) => (
                      <span key={key} className="flex items-center gap-1">
                        <span
                          className="inline-block size-2 rounded-sm"
                          style={{ background: chartConfig[key].color }}
                        />
                        {chartConfig[key].label}
                      </span>
                    ))}
                  </div>
                  <div className="flex gap-0.5 rounded-md border bg-muted/30 p-0.5">
                    {(
                      [
                        { value: 7, label: labels.sevenDays },
                        { value: 30, label: labels.thirtyDays },
                      ] as const
                    ).map((option) => (
                      <button
                        type="button"
                        key={option.value}
                        onClick={() => setDays(option.value)}
                        className={
                          "rounded px-2.5 py-0.5 font-medium text-[9px] transition-colors" +
                          (days === option.value
                            ? "bg-background text-foreground shadow-sm"
                            : "text-muted-foreground hover:text-foreground")
                        }
                      >
                        {option.label}
                      </button>
                    ))}
                  </div>
                </div>
                {trend.every((day) => day.events === 0 && day.alerts === 0) ? (
                  <div className="flex min-h-[200px] items-center justify-center rounded-lg border bg-muted/10 text-muted-foreground text-xs">
                    {copy.noData}
                  </div>
                ) : (
                  <ChartContainer config={chartConfig} className="h-[200px] w-full">
                    <BarChart data={trend} margin={{ top: 4, right: 4, bottom: 0, left: 0 }} maxBarSize={36}>
                      <CartesianGrid vertical={false} />
                      <XAxis
                        dataKey="date"
                        tickLine={false}
                        axisLine={false}
                        tick={{ fontSize: 10 }}
                        tickMargin={6}
                        interval="preserveStartEnd"
                      />
                      <YAxis
                        tickLine={false}
                        axisLine={false}
                        tick={{ fontSize: 10 }}
                        width={32}
                        allowDecimals={false}
                      />
                      <ChartTooltip cursor={false} content={<ChartTooltipContent indicator="line" />} />
                      <Bar dataKey="events" fill="var(--color-events)" radius={[3, 3, 0, 0]} />
                      <Bar dataKey="alerts" fill="var(--color-alerts)" radius={[3, 3, 0, 0]} />
                    </BarChart>
                  </ChartContainer>
                )}
              </div>
            </div>
          </Card>

          <div className="grid grid-cols-1 gap-3 lg:grid-cols-2">
            <Card className="p-4">
              <SectionHeading
                icon={Activity}
                title={labels.recentEvents}
                href="/blue/events"
                linkLabel={copy.viewAll}
              />
              <div className="divide-y">
                {events.length === 0 ? (
                  <div className="py-4 text-center text-muted-foreground text-xs">{copy.noEvents}</div>
                ) : (
                  events.slice(0, 6).map((event) => (
                    <Link
                      key={event.id}
                      href={`/blue/events?event=${event.id}`}
                      className="flex items-start gap-2 py-2 hover:bg-muted/20"
                    >
                      <span className="mt-0.5 max-w-20 shrink-0 truncate rounded border bg-muted/20 px-1.5 py-0.5 text-[9px] text-muted-foreground">
                        {event.source || labels.unknownSource}
                      </span>
                      <div className="min-w-0 flex-1">
                        <div className="truncate font-medium text-[11px]">{event.event_type || copy.event}</div>
                        <div className="mt-0.5 truncate text-[10px] text-muted-foreground">{event.message}</div>
                      </div>
                      <time
                        dateTime={event.occurred_at}
                        title={blueDate(event.occurred_at, locale)}
                        className="shrink-0 text-[9px] text-muted-foreground tabular-nums"
                      >
                        {relativeDate(event.occurred_at, locale)}
                      </time>
                    </Link>
                  ))
                )}
              </div>
            </Card>
            <Card className="p-4">
              <SectionHeading
                icon={ClipboardList}
                title={copy.recentCases}
                href="/blue/cases"
                linkLabel={copy.viewAll}
              />
              <div className="divide-y">
                {cases.length === 0 ? (
                  <div className="py-4 text-center text-muted-foreground text-xs">{copy.noCases}</div>
                ) : (
                  cases.map((item) => (
                    <Link
                      key={item.id}
                      href={`/blue/cases/detail?id=${item.id}`}
                      className="flex items-start gap-2 py-2 hover:bg-muted/20"
                    >
                      <BlueSeverityBadge severity={item.severity} locale={locale} />
                      <div className="min-w-0 flex-1">
                        <div className="truncate font-semibold text-[11px]">{item.title}</div>
                        <div className="mt-0.5 line-clamp-2 text-[10px] text-muted-foreground leading-snug">
                          {item.summary || item.description || copy.noData}
                        </div>
                      </div>
                      <div className="flex shrink-0 flex-col items-end gap-1">
                        <BlueCaseStatusBadge status={item.status} locale={locale} />
                        <time
                          dateTime={item.updated_at}
                          title={blueDate(item.updated_at, locale)}
                          className="text-[9px] text-muted-foreground"
                        >
                          {relativeDate(item.updated_at, locale)}
                        </time>
                      </div>
                    </Link>
                  ))
                )}
              </div>
            </Card>
          </div>

          <Card className="overflow-hidden p-0">
            <div className="flex items-center justify-between border-b px-4 py-3">
              <div className="flex items-center gap-1.5 font-semibold text-xs">
                <Clock3 className="size-3.5 text-muted-foreground" aria-hidden="true" />
                {labels.recentTasks}
              </div>
              <Link
                href="/blue/tasks"
                className="flex items-center gap-0.5 text-[10px] text-muted-foreground hover:text-foreground"
              >
                {copy.viewAll}
                <ArrowUpRight className="size-3" aria-hidden="true" />
              </Link>
            </div>
            <div className="overflow-x-auto">
              <table className="w-full min-w-[620px] border-collapse text-xs">
                <thead>
                  <tr className="border-b">
                    {[
                      labels.taskColumn,
                      labels.statusColumn,
                      labels.priorityColumn,
                      labels.caseColumn,
                      labels.updatedColumn,
                    ].map((heading) => (
                      <th
                        key={heading}
                        className="px-4 py-2 text-left font-semibold text-[9px] text-muted-foreground uppercase tracking-widest"
                      >
                        {heading}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {sortedTasks.length === 0 ? (
                    <tr>
                      <td colSpan={5} className="px-4 py-6 text-center text-muted-foreground text-xs">
                        {labels.noTasks}
                      </td>
                    </tr>
                  ) : (
                    sortedTasks.map((task) => (
                      <tr key={task.id} className="border-b transition-colors last:border-0 hover:bg-muted/30">
                        <td className="max-w-xs px-4 py-3">
                          <Link href={`/blue/tasks/detail?id=${task.id}`} className="group flex flex-col">
                            <span className="truncate font-medium group-hover:underline">{task.title}</span>
                            <span className="mt-0.5 truncate text-[10px] text-muted-foreground">
                              {task.description || `#${task.id}`}
                            </span>
                          </Link>
                        </td>
                        <td className="px-4 py-3">
                          <Badge variant={task.status === "blocked" ? "destructive" : "secondary"}>
                            {blueTaskStatusLabel(task.status, taskCopy)}
                          </Badge>
                        </td>
                        <td className="px-4 py-3">
                          <Badge variant="outline">{blueTaskPriorityLabel(task.priority, taskCopy)}</Badge>
                        </td>
                        <td className="px-4 py-3">
                          {task.case_id ? (
                            <Link href={`/blue/cases/detail?id=${task.case_id}`} className="hover:underline">
                              #{task.case_id}
                            </Link>
                          ) : (
                            <span className="text-muted-foreground">—</span>
                          )}
                        </td>
                        <td className="px-4 py-3 text-muted-foreground tabular-nums">
                          <time dateTime={task.updated_at} title={blueDate(task.updated_at, locale)}>
                            {relativeDate(task.updated_at, locale)}
                          </time>
                        </td>
                      </tr>
                    ))
                  )}
                </tbody>
              </table>
            </div>
          </Card>

          <div className="grid grid-cols-1 gap-3 lg:grid-cols-3">
            <Card className="p-4">
              <SectionHeading
                icon={Bell}
                title={copy.recentAlerts}
                href="/blue/detection?tab=alerts"
                linkLabel={copy.viewAll}
              />
              <div className="divide-y">
                {alerts.length === 0 ? (
                  <div className="py-4 text-center text-muted-foreground text-xs">{copy.noAlerts}</div>
                ) : (
                  alerts.slice(0, 5).map((item) => (
                    <Link
                      key={item.id}
                      href={`/blue/detection?tab=alerts&alert=${item.id}`}
                      className="flex items-start gap-2 py-2 hover:bg-muted/20"
                    >
                      <BlueSeverityBadge severity={item.severity} locale={locale} />
                      <div className="min-w-0 flex-1">
                        <div className="truncate font-medium text-[11px]">{item.title}</div>
                        <div className="mt-0.5 truncate text-[10px] text-muted-foreground">
                          {copy.eventID}
                          {item.event_id}
                        </div>
                      </div>
                      <BlueAlertStatusBadge status={item.status} locale={locale} />
                    </Link>
                  ))
                )}
              </div>
            </Card>
            <Card className="p-4">
              <SectionHeading icon={Activity} title={labels.eventSources} note={labels.sample(events.length)} />
              {sourceCounts.length === 0 ? (
                <div className="py-6 text-center text-muted-foreground text-xs">{copy.noEvents}</div>
              ) : (
                <div className="flex flex-col gap-2">
                  {sourceCounts.map((entry) => (
                    <div key={entry.source} className="flex items-center gap-2 text-xs">
                      <span
                        className="w-24 shrink-0 truncate text-right text-[10px] text-muted-foreground"
                        title={entry.source}
                      >
                        {entry.source}
                      </span>
                      <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-muted">
                        <div
                          className="h-full rounded-full bg-blue-500"
                          style={{ width: `${percentage(entry.count, sourceCounts[0].count)}%` }}
                        />
                      </div>
                      <span className="w-6 text-right tabular-nums">{entry.count}</span>
                    </div>
                  ))}
                </div>
              )}
            </Card>
            <Card className="p-4">
              <SectionHeading icon={ShieldCheck} title={labels.operations} />
              <div className="flex flex-col gap-2">
                {[
                  { label: copy.detectionRules, count: overview.rule_count, href: "/blue/detection?tab=rules" },
                  { label: copy.newAlerts, count: overview.new_alert_count, href: "/blue/detection?tab=alerts" },
                  { label: copy.pendingActions, count: overview.pending_action_count, href: "/blue/actions" },
                ].map((entry) => (
                  <Link
                    key={entry.label}
                    href={entry.href}
                    className="flex items-center justify-between rounded-lg border bg-muted/20 px-3 py-2 hover:bg-muted/40"
                  >
                    <span className="text-[10px] text-muted-foreground">{entry.label}</span>
                    <span className="font-mono font-semibold text-xs tabular-nums">{entry.count.toLocaleString()}</span>
                  </Link>
                ))}
              </div>
            </Card>
          </div>
        </>
      )}
    </div>
  );
}

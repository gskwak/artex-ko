"use client";

import * as React from "react";

import Link from "next/link";

import { Activity, ArrowRight, Bell, ClipboardList, RefreshCw, ShieldCheck, ShieldQuestion } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { useActiveLocale } from "@/i18n/locale-store";
import { type BlueAlert, type BlueCase, type BlueOverview, blueApi } from "@/lib/blue-api";
import { useBlueCopy } from "@/lib/blue-copy";

import {
  BlueAlertStatusBadge,
  BlueCaseStatusBadge,
  BlueEmpty,
  BlueError,
  BlueLoading,
  BluePageHeader,
  BlueSeverityBadge,
  blueDate,
} from "../_components/blue-ui";

export default function BlueDashboardPage() {
  const copy = useBlueCopy();
  const locale = useActiveLocale();
  const [overview, setOverview] = React.useState<BlueOverview | null>(null);
  const [cases, setCases] = React.useState<BlueCase[]>([]);
  const [alerts, setAlerts] = React.useState<BlueAlert[]>([]);
  const [loading, setLoading] = React.useState(true);
  const [error, setError] = React.useState("");

  const load = React.useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const [summary, recentCases, recentAlerts] = await Promise.all([
        blueApi.overview(),
        blueApi.cases({ limit: 6 }),
        blueApi.alerts({ limit: 6 }),
      ]);
      setOverview(summary);
      setCases(recentCases);
      setAlerts(recentAlerts);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : copy.loadFailed);
    } finally {
      setLoading(false);
    }
  }, [copy.loadFailed]);

  React.useEffect(() => {
    void load();
  }, [load]);

  const metrics = overview
    ? [
        { label: copy.openCases, value: overview.open_case_count, icon: ClipboardList, href: "/blue/cases" },
        { label: copy.newAlerts, value: overview.new_alert_count, icon: Bell, href: "/blue/detection?tab=alerts" },
        { label: copy.totalEvents, value: overview.event_count, icon: Activity, href: "/blue/events" },
        {
          label: copy.detectionRules,
          value: overview.rule_count,
          icon: ShieldCheck,
          href: "/blue/detection?tab=rules",
        },
        {
          label: copy.pendingActions,
          value: overview.pending_action_count,
          icon: ShieldQuestion,
          href: "/blue/actions",
        },
      ]
    : [];

  return (
    <div className="space-y-6">
      <BluePageHeader
        title={copy.dashboard}
        description={copy.dashboardDescription}
        actions={
          <Button variant="outline" size="sm" onClick={() => void load()} disabled={loading}>
            <RefreshCw className={loading ? "animate-spin" : ""} />
            {copy.refresh}
          </Button>
        }
      />
      {error && <BlueError message={error} retry={() => void load()} retryLabel={copy.refresh} />}
      {loading && !overview ? <BlueLoading label={copy.loading} /> : null}
      {overview && (
        <>
          <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
            {metrics.map(({ label, value, icon: Icon, href }) => (
              <Link
                key={label}
                href={href}
                className="rounded-xl focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              >
                <Card className="h-full transition-colors hover:border-primary/50">
                  <CardContent className="flex items-center justify-between gap-2 p-4">
                    <div>
                      <p className="text-xs text-muted-foreground">{label}</p>
                      <p className="mt-1 text-2xl font-semibold tabular-nums">{value.toLocaleString()}</p>
                    </div>
                    <Icon className="size-5 text-primary" aria-hidden="true" />
                  </CardContent>
                </Card>
              </Link>
            ))}
          </div>
          <div className="grid gap-4 xl:grid-cols-2">
            <Card>
              <CardHeader className="flex flex-row items-center justify-between gap-2 pb-2">
                <CardTitle className="text-base">{copy.recentCases}</CardTitle>
                <Button asChild variant="ghost" size="sm">
                  <Link href="/blue/cases">
                    {copy.viewAll}
                    <ArrowRight />
                  </Link>
                </Button>
              </CardHeader>
              <CardContent className="space-y-2">
                {cases.length === 0 ? (
                  <BlueEmpty message={copy.noCases} />
                ) : (
                  cases.map((item) => (
                    <Link
                      key={item.id}
                      href={`/blue/cases/detail?id=${item.id}`}
                      className="flex min-w-0 items-center justify-between gap-3 rounded-lg border p-3 transition-colors hover:bg-muted/40"
                    >
                      <div className="min-w-0">
                        <p className="truncate text-sm font-medium">
                          #{item.id} {item.title}
                        </p>
                        <p className="text-xs text-muted-foreground">{blueDate(item.updated_at, locale)}</p>
                      </div>
                      <div className="flex shrink-0 items-center gap-2">
                        <BlueSeverityBadge severity={item.severity} locale={locale} />
                        <BlueCaseStatusBadge status={item.status} locale={locale} />
                      </div>
                    </Link>
                  ))
                )}
              </CardContent>
            </Card>
            <Card>
              <CardHeader className="flex flex-row items-center justify-between gap-2 pb-2">
                <CardTitle className="text-base">{copy.recentAlerts}</CardTitle>
                <Button asChild variant="ghost" size="sm">
                  <Link href="/blue/detection?tab=alerts">
                    {copy.viewAll}
                    <ArrowRight />
                  </Link>
                </Button>
              </CardHeader>
              <CardContent className="space-y-2">
                {alerts.length === 0 ? (
                  <BlueEmpty message={copy.noAlerts} />
                ) : (
                  alerts.map((item) => (
                    <Link
                      key={item.id}
                      href={`/blue/detection?tab=alerts&alert=${item.id}`}
                      className="flex min-w-0 items-center justify-between gap-3 rounded-lg border p-3 transition-colors hover:bg-muted/40"
                    >
                      <div className="min-w-0">
                        <p className="truncate text-sm font-medium">
                          #{item.id} {item.title}
                        </p>
                        <p className="text-xs text-muted-foreground">
                          {blueDate(item.created_at, locale)} · {copy.eventID}
                          {item.event_id}
                        </p>
                      </div>
                      <div className="flex shrink-0 items-center gap-2">
                        <BlueSeverityBadge severity={item.severity} locale={locale} />
                        <BlueAlertStatusBadge status={item.status} locale={locale} />
                      </div>
                    </Link>
                  ))
                )}
              </CardContent>
            </Card>
          </div>
        </>
      )}
    </div>
  );
}

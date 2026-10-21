"use client";

import type { ReactNode } from "react";

import { AlertCircle, Inbox } from "lucide-react";

import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import type { BlueAlertStatus, BlueCaseStatus, BlueSeverity } from "@/lib/blue-api";
import { blueAlertStatusLabel, blueCaseStatusLabel, blueSeverityLabel } from "@/lib/blue-copy";
import { cn } from "@/lib/utils";

export function BluePageHeader({
  title,
  description,
  actions,
}: {
  title: string;
  description?: string;
  actions?: ReactNode;
}) {
  return (
    <div className="mb-6 flex flex-wrap items-start justify-between gap-4">
      <div className="min-w-0 space-y-1">
        <h1 className="text-2xl font-semibold tracking-tight">{title}</h1>
        {description && <p className="max-w-2xl text-sm text-muted-foreground">{description}</p>}
      </div>
      {actions && <div className="flex flex-wrap items-center gap-2">{actions}</div>}
    </div>
  );
}

export function BlueError({
  message,
  retry,
  retryLabel,
}: {
  message: string;
  retry?: () => void;
  retryLabel?: string;
}) {
  return (
    <Alert variant="destructive">
      <AlertCircle className="size-4" />
      <AlertDescription className="flex flex-wrap items-center justify-between gap-2">
        <span>{message}</span>
        {retry && (
          <Button size="sm" variant="outline" onClick={retry}>
            {retryLabel ?? "Retry"}
          </Button>
        )}
      </AlertDescription>
    </Alert>
  );
}

export function BlueEmpty({ message }: { message: string }) {
  return (
    <Card>
      <CardContent className="flex min-h-40 flex-col items-center justify-center gap-3 p-6 text-center text-muted-foreground">
        <Inbox className="size-7 opacity-60" aria-hidden="true" />
        <p className="max-w-md text-sm">{message}</p>
      </CardContent>
    </Card>
  );
}

export function BlueLoading({ label }: { label: string }) {
  return (
    <p className="py-10 text-center text-sm text-muted-foreground" role="status">
      {label}
    </p>
  );
}

export function BlueSeverityBadge({ severity, locale }: { severity: BlueSeverity; locale: string }) {
  const styles: Record<BlueSeverity, string> = {
    info: "border-sky-500/30 bg-sky-500/10 text-sky-700 dark:text-sky-300",
    low: "border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300",
    medium: "border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300",
    high: "border-orange-500/30 bg-orange-500/10 text-orange-700 dark:text-orange-300",
    critical: "border-red-500/30 bg-red-500/10 text-red-700 dark:text-red-300",
  };
  return (
    <Badge variant="outline" className={cn("font-medium", styles[severity])}>
      {blueSeverityLabel(severity, locale)}
    </Badge>
  );
}

export function BlueCaseStatusBadge({ status, locale }: { status: BlueCaseStatus; locale: string }) {
  return <Badge variant="secondary">{blueCaseStatusLabel(status, locale)}</Badge>;
}

export function BlueAlertStatusBadge({ status, locale }: { status: BlueAlertStatus; locale: string }) {
  return <Badge variant={status === "new" ? "destructive" : "secondary"}>{blueAlertStatusLabel(status, locale)}</Badge>;
}

export function blueDate(value?: string, locale = "ko"): string {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString(locale === "ko" ? "ko-KR" : "en-US");
}

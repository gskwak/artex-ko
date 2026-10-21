"use client";

import * as React from "react";

import { RefreshCw, RotateCcw, Rss, Trash2 } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { useActiveLocale } from "@/i18n/locale-store";
import { type BlueEventSource, type BlueSourceSyncResult, blueApi } from "@/lib/blue-api";
import { useBlueSourceCopy } from "@/lib/blue-source-copy";

import { BlueError, blueDate } from "./blue-ui";

interface BlueSourcesPanelProps {
  onEventsChanged: () => void;
}

export function BlueSourcesPanel({ onEventsChanged }: BlueSourcesPanelProps) {
  const copy = useBlueSourceCopy();
  const locale = useActiveLocale();
  const [sources, setSources] = React.useState<BlueEventSource[]>([]);
  const [loading, setLoading] = React.useState(true);
  const [loadError, setLoadError] = React.useState("");
  const [actionError, setActionError] = React.useState("");
  const [reload, setReload] = React.useState(0);
  const [name, setName] = React.useState("");
  const [url, setUrl] = React.useState("");
  const [interval, setIntervalValue] = React.useState("30");
  const [adding, setAdding] = React.useState(false);
  const [busyID, setBusyID] = React.useState<number | null>(null);
  const [busyKind, setBusyKind] = React.useState<"sync" | "toggle" | "reset" | "delete" | null>(null);

  // biome-ignore lint/correctness/useExhaustiveDependencies: reload is an explicit refetch trigger.
  React.useEffect(() => {
    let active = true;
    setLoading(true);
    setLoadError("");
    blueApi
      .eventSources()
      .then((items) => {
        if (active) setSources(items);
      })
      .catch((cause: unknown) => {
        if (active) setLoadError(cause instanceof Error ? cause.message : copy.sourceError);
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [copy.sourceError, reload]);

  React.useEffect(() => {
    const timer = window.setInterval(() => setReload((value) => value + 1), 30_000);
    return () => window.clearInterval(timer);
  }, []);

  const intervalSeconds = Number(interval);
  const validInterval = Number.isInteger(intervalSeconds) && intervalSeconds >= 10 && intervalSeconds <= 3600;
  const canAdd = name.trim().length > 0 && url.trim().length > 0 && validInterval && !adding;

  function reportSync(result: BlueSourceSyncResult) {
    const count = `${copy.processed}: ${result.processed.toLocaleString(locale)}`;
    if (result.detection_errors > 0 || (result.warnings?.length ?? 0) > 0) {
      toast.warning(`${copy.syncWarning} ${count}`);
    } else {
      toast.success(`${copy.syncComplete} ${count}`);
    }
  }

  async function addSource(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!canAdd) return;
    setAdding(true);
    setActionError("");
    try {
      await blueApi.createEventSource({
        name: name.trim(),
        url: url.trim(),
        poll_interval_seconds: intervalSeconds,
      });
      setName("");
      setUrl("");
      toast.success(copy.created);
      setReload((value) => value + 1);
    } catch (cause) {
      setActionError(cause instanceof Error ? cause.message : copy.sourceError);
    } finally {
      setAdding(false);
    }
  }

  async function syncSource(source: BlueEventSource) {
    if (busyID !== null) return;
    setBusyID(source.id);
    setBusyKind("sync");
    setActionError("");
    try {
      const result = await blueApi.syncEventSource(source.id);
      reportSync(result);
      setReload((value) => value + 1);
      onEventsChanged();
    } catch (cause) {
      setActionError(cause instanceof Error ? cause.message : copy.sourceError);
      setReload((value) => value + 1);
    } finally {
      setBusyID(null);
      setBusyKind(null);
    }
  }

  async function toggleSource(source: BlueEventSource) {
    if (busyID !== null) return;
    setBusyID(source.id);
    setBusyKind("toggle");
    setActionError("");
    try {
      await blueApi.updateEventSource(source.id, { enabled: !source.enabled });
      toast.success(source.enabled ? copy.disabled : copy.enabled);
      setReload((value) => value + 1);
    } catch (cause) {
      setActionError(cause instanceof Error ? cause.message : copy.sourceError);
    } finally {
      setBusyID(null);
      setBusyKind(null);
    }
  }

  async function resetSource(source: BlueEventSource) {
    if (busyID !== null || !window.confirm(copy.resetConfirm)) return;
    setBusyID(source.id);
    setBusyKind("reset");
    setActionError("");
    try {
      await blueApi.updateEventSource(source.id, { reset_cursor: true });
      const result = await blueApi.syncEventSource(source.id);
      reportSync(result);
      setReload((value) => value + 1);
      onEventsChanged();
    } catch (cause) {
      setActionError(cause instanceof Error ? cause.message : copy.sourceError);
      setReload((value) => value + 1);
    } finally {
      setBusyID(null);
      setBusyKind(null);
    }
  }

  async function removeSource(source: BlueEventSource) {
    if (busyID !== null || !window.confirm(copy.removeConfirm)) return;
    setBusyID(source.id);
    setBusyKind("delete");
    setActionError("");
    try {
      await blueApi.deleteEventSource(source.id);
      toast.success(copy.deleted);
      setReload((value) => value + 1);
    } catch (cause) {
      setActionError(cause instanceof Error ? cause.message : copy.sourceError);
    } finally {
      setBusyID(null);
      setBusyKind(null);
    }
  }

  return (
    <Card>
      <CardHeader className="pb-2">
        <div className="flex items-center justify-between gap-2">
          <CardTitle className="flex items-center gap-2 text-base">
            <Rss aria-hidden="true" className="size-4" />
            {copy.title}
          </CardTitle>
          <Button size="sm" variant="outline" onClick={() => setReload((value) => value + 1)} disabled={loading}>
            <RefreshCw aria-hidden="true" className={loading ? "animate-spin" : ""} />
            {copy.refresh}
          </Button>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        <p className="text-sm text-muted-foreground">{copy.description}</p>
        <form
          className="grid gap-3 lg:grid-cols-[minmax(8rem,1fr)_minmax(12rem,2fr)_8rem_auto] lg:items-end"
          onSubmit={(event) => void addSource(event)}
        >
          <div className="space-y-1.5">
            <label htmlFor="blue-source-name" className="text-xs font-medium">
              {copy.name}
            </label>
            <Input
              id="blue-source-name"
              value={name}
              onChange={(event) => setName(event.target.value)}
              maxLength={200}
              disabled={adding}
            />
          </div>
          <div className="space-y-1.5">
            <label htmlFor="blue-source-url" className="text-xs font-medium">
              {copy.url}
            </label>
            <Input
              id="blue-source-url"
              type="url"
              value={url}
              onChange={(event) => setUrl(event.target.value)}
              placeholder="https://sensor.example/events.jsonl"
              disabled={adding}
            />
          </div>
          <div className="space-y-1.5">
            <label htmlFor="blue-source-interval" className="text-xs font-medium">
              {copy.interval}
            </label>
            <Input
              id="blue-source-interval"
              type="number"
              min={10}
              max={3600}
              step={1}
              value={interval}
              onChange={(event) => setIntervalValue(event.target.value)}
              disabled={adding}
            />
          </div>
          <Button type="submit" disabled={!canAdd}>
            {adding ? copy.adding : copy.add}
          </Button>
        </form>
        <p className="text-xs text-muted-foreground">{copy.intervalHint}</p>
        <p className="text-xs text-amber-700 dark:text-amber-300">{copy.trustHint}</p>
        {actionError && <BlueError message={actionError} />}
        {loadError && (
          <BlueError message={loadError} retry={() => setReload((value) => value + 1)} retryLabel={copy.refresh} />
        )}
        {loading && sources.length === 0 && <p className="text-sm text-muted-foreground">{copy.loading}</p>}
        {!loading && !loadError && sources.length === 0 && (
          <p className="text-sm text-muted-foreground">{copy.empty}</p>
        )}
        {sources.length > 0 && (
          <div className="space-y-2">
            {sources.map((source) => (
              <div key={source.id} className="space-y-2 rounded-lg border p-3">
                <div className="flex flex-wrap items-start justify-between gap-3">
                  <div className="min-w-0 space-y-1">
                    <p className="font-medium">
                      {source.name} <span className="text-xs text-muted-foreground">#{source.id}</span>
                    </p>
                    <p className="break-all font-mono text-xs text-muted-foreground">{source.url}</p>
                    <p className="text-xs text-muted-foreground">
                      {source.enabled ? copy.enabled : copy.disabled} · {copy.interval}: {source.poll_interval_seconds}s
                      · {copy.lastSync}:{" "}
                      {source.last_synced_at ? blueDate(source.last_synced_at, locale) : copy.neverSynced}
                    </p>
                  </div>
                  <div className="flex flex-wrap gap-2">
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => void syncSource(source)}
                      disabled={busyID !== null}
                    >
                      <RefreshCw
                        aria-hidden="true"
                        className={busyID === source.id && busyKind === "sync" ? "animate-spin" : ""}
                      />
                      {busyID === source.id && busyKind === "sync" ? copy.syncing : copy.syncNow}
                    </Button>
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => void toggleSource(source)}
                      disabled={busyID !== null}
                    >
                      {source.enabled ? copy.disable : copy.enable}
                    </Button>
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => void resetSource(source)}
                      disabled={busyID !== null}
                    >
                      <RotateCcw aria-hidden="true" className="size-4" />
                      {copy.resetCursor}
                    </Button>
                    <Button
                      size="sm"
                      variant="ghost"
                      onClick={() => void removeSource(source)}
                      disabled={busyID !== null}
                      aria-label={`${copy.remove}: ${source.name}`}
                    >
                      <Trash2 aria-hidden="true" className="size-4" />
                      {copy.remove}
                    </Button>
                  </div>
                </div>
                {source.last_error && (
                  <p
                    role="alert"
                    className="rounded-md border border-destructive/40 bg-destructive/5 p-2 text-xs text-destructive"
                  >
                    {copy.lastError}: {source.last_error}
                  </p>
                )}
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  );
}

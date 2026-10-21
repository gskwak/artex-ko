"use client";

import * as React from "react";

import Link from "next/link";

import { ArrowRight, Plus, RefreshCw, Search } from "lucide-react";
import { toast } from "sonner";

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
import { type BlueCase, type BlueCaseStatus, type BlueSeverity, blueApi } from "@/lib/blue-api";
import { blueCaseStatusLabel, blueSeverityLabel, useBlueCopy } from "@/lib/blue-copy";

import {
  BlueCaseStatusBadge,
  BlueEmpty,
  BlueError,
  BlueLoading,
  BluePageHeader,
  BlueSeverityBadge,
  blueDate,
} from "../_components/blue-ui";
import { BlueInvestigationTabs } from "../_components/investigation-tabs";

const CASE_STATUSES: BlueCaseStatus[] = ["open", "investigating", "contained", "resolved", "closed"];
const SEVERITIES: BlueSeverity[] = ["info", "low", "medium", "high", "critical"];
const PAGE_SIZE = 100;

export default function BlueCasesPage() {
  const copy = useBlueCopy();
  const locale = useActiveLocale();
  const [cases, setCases] = React.useState<BlueCase[]>([]);
  const [loading, setLoading] = React.useState(true);
  const [loadingMore, setLoadingMore] = React.useState(false);
  const [hasMore, setHasMore] = React.useState(false);
  const [nextOffset, setNextOffset] = React.useState(0);
  const [error, setError] = React.useState("");
  const [search, setSearch] = React.useState("");
  const [query, setQuery] = React.useState("");
  const [status, setStatus] = React.useState<BlueCaseStatus | "all">("all");
  const [severity, setSeverity] = React.useState<BlueSeverity | "all">("all");
  const [createOpen, setCreateOpen] = React.useState(false);
  const [creating, setCreating] = React.useState(false);
  const [title, setTitle] = React.useState("");
  const [description, setDescription] = React.useState("");
  const [newSeverity, setNewSeverity] = React.useState<BlueSeverity>("medium");
  const loadVersion = React.useRef(0);

  React.useEffect(() => {
    const timer = window.setTimeout(() => setQuery(search.trim()), 250);
    return () => window.clearTimeout(timer);
  }, [search]);

  const load = React.useCallback(async () => {
    const version = ++loadVersion.current;
    setLoading(true);
    setLoadingMore(false);
    setCases([]);
    setHasMore(false);
    setNextOffset(0);
    setError("");
    try {
      const page = await blueApi.cases({
        q: query,
        status: status === "all" ? undefined : status,
        severity: severity === "all" ? undefined : severity,
        limit: PAGE_SIZE + 1,
      });
      if (version !== loadVersion.current) return;
      setCases(page.slice(0, PAGE_SIZE));
      setHasMore(page.length > PAGE_SIZE);
      setNextOffset(Math.min(page.length, PAGE_SIZE));
    } catch (cause) {
      if (version === loadVersion.current) setError(cause instanceof Error ? cause.message : copy.loadFailed);
    } finally {
      if (version === loadVersion.current) setLoading(false);
    }
  }, [copy.loadFailed, query, severity, status]);

  React.useEffect(() => {
    void load();
  }, [load]);

  async function loadMore() {
    if (loadingMore || !hasMore) return;
    const version = loadVersion.current;
    setLoadingMore(true);
    try {
      const page = await blueApi.cases({
        q: query,
        status: status === "all" ? undefined : status,
        severity: severity === "all" ? undefined : severity,
        limit: PAGE_SIZE + 1,
        offset: nextOffset,
      });
      if (version !== loadVersion.current) return;
      setCases((current) => {
        const known = new Set(current.map((item) => item.id));
        return [...current, ...page.slice(0, PAGE_SIZE).filter((item) => !known.has(item.id))];
      });
      setNextOffset(nextOffset + Math.min(page.length, PAGE_SIZE));
      setHasMore(page.length > PAGE_SIZE);
    } catch (cause) {
      if (version === loadVersion.current) toast.error(cause instanceof Error ? cause.message : copy.loadFailed);
    } finally {
      if (version === loadVersion.current) setLoadingMore(false);
    }
  }

  async function createCase(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!title.trim()) return;
    setCreating(true);
    try {
      await blueApi.createCase({
        title: title.trim(),
        description: description.trim(),
        severity: newSeverity,
      });
      void load();
      setTitle("");
      setDescription("");
      setNewSeverity("medium");
      setCreateOpen(false);
      toast.success(copy.saved);
    } catch (cause) {
      toast.error(cause instanceof Error ? cause.message : copy.saveFailed);
    } finally {
      setCreating(false);
    }
  }

  return (
    <div className="space-y-5">
      <BluePageHeader
        title={copy.cases}
        description={copy.casesDescription}
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
                  {copy.createCase}
                </Button>
              </DialogTrigger>
              <DialogContent>
                <DialogHeader>
                  <DialogTitle>{copy.createCase}</DialogTitle>
                  <DialogDescription>{copy.casesDescription}</DialogDescription>
                </DialogHeader>
                <form onSubmit={(event) => void createCase(event)} className="space-y-4">
                  <div className="space-y-1.5">
                    <Label htmlFor="blue-case-title">{copy.caseTitle}</Label>
                    <Input
                      id="blue-case-title"
                      value={title}
                      onChange={(event) => setTitle(event.target.value)}
                      maxLength={240}
                      required
                      autoFocus
                    />
                  </div>
                  <div className="space-y-1.5">
                    <Label htmlFor="blue-case-description">{copy.description}</Label>
                    <Textarea
                      id="blue-case-description"
                      value={description}
                      onChange={(event) => setDescription(event.target.value)}
                      rows={4}
                    />
                  </div>
                  <div className="space-y-1.5">
                    <Label htmlFor="blue-case-severity">{copy.severity}</Label>
                    <select
                      id="blue-case-severity"
                      className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm"
                      value={newSeverity}
                      onChange={(event) => setNewSeverity(event.target.value as BlueSeverity)}
                    >
                      {SEVERITIES.map((value) => (
                        <option key={value} value={value}>
                          {blueSeverityLabel(value, locale)}
                        </option>
                      ))}
                    </select>
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

      <BlueInvestigationTabs active="cases" />

      <div className="flex flex-wrap items-center gap-2">
        <div className="relative min-w-48 flex-1">
          <Search className="absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            className="pl-9"
            aria-label={copy.searchCases}
            placeholder={copy.searchCases}
            value={search}
            onChange={(event) => setSearch(event.target.value)}
          />
        </div>
        <select
          aria-label={copy.filterStatus}
          className="h-9 rounded-md border border-input bg-background px-3 text-sm"
          value={status}
          onChange={(event) => setStatus(event.target.value as BlueCaseStatus | "all")}
        >
          <option value="all">{copy.allStatuses}</option>
          {CASE_STATUSES.map((value) => (
            <option key={value} value={value}>
              {blueCaseStatusLabel(value, locale)}
            </option>
          ))}
        </select>
        <select
          aria-label={copy.severity}
          className="h-9 rounded-md border border-input bg-background px-3 text-sm"
          value={severity}
          onChange={(event) => setSeverity(event.target.value as BlueSeverity | "all")}
        >
          <option value="all">{copy.allSeverities}</option>
          {SEVERITIES.map((value) => (
            <option key={value} value={value}>
              {blueSeverityLabel(value, locale)}
            </option>
          ))}
        </select>
      </div>

      {error && <BlueError message={error} retry={() => void load()} retryLabel={copy.refresh} />}
      {loading && cases.length === 0 ? <BlueLoading label={copy.loading} /> : null}
      {!loading && !error && cases.length === 0 ? <BlueEmpty message={copy.noCases} /> : null}
      {cases.length > 0 && (
        <div className="grid gap-3 lg:grid-cols-2 2xl:grid-cols-3">
          {cases.map((item) => (
            <Card key={item.id} className="transition-colors hover:border-primary/50">
              <CardContent className="space-y-3 p-4">
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <Link
                      className="line-clamp-2 font-semibold hover:underline"
                      href={`/blue/cases/detail?id=${item.id}`}
                    >
                      #{item.id} {item.title}
                    </Link>
                    <p className="mt-1 line-clamp-2 text-sm text-muted-foreground">
                      {item.summary || item.description || copy.noData}
                    </p>
                  </div>
                  <Button asChild variant="ghost" size="icon-sm" aria-label={`${copy.openCase} #${item.id}`}>
                    <Link href={`/blue/cases/detail?id=${item.id}`}>
                      <ArrowRight />
                    </Link>
                  </Button>
                </div>
                <div className="flex flex-wrap items-center gap-2">
                  <BlueSeverityBadge severity={item.severity} locale={locale} />
                  <BlueCaseStatusBadge status={item.status} locale={locale} />
                </div>
                <p className="text-xs text-muted-foreground">
                  {copy.updated}: {blueDate(item.updated_at, locale)}
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

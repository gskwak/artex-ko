"use client";

import * as React from "react";

import Link from "next/link";

import { ArrowUpRight, ChevronLeft, ChevronRight, RefreshCw, Search } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { useActiveLocale } from "@/i18n/locale-store";
import { type BlueEvent, blueApi } from "@/lib/blue-api";
import { useBlueTrafficCopy } from "@/lib/blue-traffic-copy";
import { trafficFields } from "@/lib/blue-traffic-fields";

import { BlueError, BlueLoading, BluePageHeader, BlueSeverityBadge, blueDate } from "../_components/blue-ui";
import { BlueInvestigationTabs } from "../_components/investigation-tabs";

const PAGE_SIZE = 50;
const PROTOCOLS = ["TCP", "UDP", "ICMP", "HTTP", "DNS", "TLS"] as const;

export default function BlueTrafficPage() {
  const copy = useBlueTrafficCopy();
  const locale = useActiveLocale();
  const [searchInput, setSearchInput] = React.useState("");
  const [sourceInput, setSourceInput] = React.useState("");
  const [query, setQuery] = React.useState("");
  const [source, setSource] = React.useState("");
  const [protocol, setProtocol] = React.useState("");
  const [page, setPage] = React.useState(0);
  const [reload, setReload] = React.useState(0);
  const [items, setItems] = React.useState<BlueEvent[]>([]);
  const [total, setTotal] = React.useState(0);
  const [loading, setLoading] = React.useState(true);
  const [error, setError] = React.useState("");
  const [selected, setSelected] = React.useState<BlueEvent | null>(null);

  // biome-ignore lint/correctness/useExhaustiveDependencies: reload is an explicit manual refetch trigger.
  React.useEffect(() => {
    let active = true;
    setLoading(true);
    setError("");
    blueApi
      .traffic({ limit: PAGE_SIZE, offset: page * PAGE_SIZE, q: query, source, protocol })
      .then((result) => {
        if (!active) return;
        if (page > 0 && result.total <= page * PAGE_SIZE) {
          setPage(Math.max(0, Math.ceil(result.total / PAGE_SIZE) - 1));
          return;
        }
        setItems(result.items);
        setTotal(result.total);
      })
      .catch((cause: unknown) => {
        if (active) setError(cause instanceof Error ? cause.message : copy.loadFailed);
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [copy.loadFailed, page, protocol, query, reload, source]);

  const selectedFields = selected ? trafficFields(selected) : null;
  const pages = Math.max(1, Math.ceil(total / PAGE_SIZE));
  const first = total === 0 ? 0 : page * PAGE_SIZE + 1;
  const last = Math.min(total, page * PAGE_SIZE + items.length);

  return (
    <div className="space-y-5 pb-6">
      <BluePageHeader
        title={copy.title}
        description={copy.subtitle}
        actions={
          <Button variant="outline" size="sm" onClick={() => setReload((value) => value + 1)} disabled={loading}>
            <RefreshCw aria-hidden="true" className={loading ? "animate-spin" : ""} />
            {copy.refresh}
          </Button>
        }
      />
      <BlueInvestigationTabs active="traffic" />

      <div className="flex flex-wrap items-center justify-between gap-2 rounded-lg border bg-muted/20 px-3 py-2 text-xs text-muted-foreground">
        <span>{copy.trafficHint}</span>
        <Button asChild variant="outline" size="sm">
          <Link href="/blue/events">
            {copy.importEvents}
            <ArrowUpRight aria-hidden="true" className="size-3.5" />
          </Link>
        </Button>
      </div>

      <form
        className="flex flex-wrap items-center gap-2"
        onSubmit={(event) => {
          event.preventDefault();
          setPage(0);
          setQuery(searchInput.trim());
          setSource(sourceInput.trim());
        }}
      >
        <Input
          aria-label={copy.search}
          placeholder={copy.search}
          value={searchInput}
          onChange={(event) => setSearchInput(event.target.value)}
          className="min-w-48 flex-1"
        />
        <Input
          aria-label={copy.sourceSearch}
          placeholder={copy.sourceSearch}
          value={sourceInput}
          onChange={(event) => setSourceInput(event.target.value)}
          className="w-44"
        />
        <select
          aria-label={copy.protocol}
          value={protocol}
          onChange={(event) => {
            setProtocol(event.target.value);
            setPage(0);
          }}
          className="h-9 rounded-md border border-input bg-background px-3 text-sm"
        >
          <option value="">{copy.allProtocols}</option>
          {PROTOCOLS.map((value) => (
            <option key={value} value={value}>
              {value}
            </option>
          ))}
        </select>
        <Button type="submit" size="sm">
          <Search aria-hidden="true" className="size-4" />
          {copy.search}
        </Button>
      </form>

      {error && <BlueError message={error} retry={() => setReload((value) => value + 1)} retryLabel={copy.refresh} />}
      {loading && items.length === 0 ? <BlueLoading label={copy.loading} /> : null}

      <Card className="gap-0 overflow-hidden p-0">
        <div className="overflow-x-auto">
          <Table className="min-w-[920px]">
            <TableHeader>
              <TableRow>
                <TableHead>{copy.time}</TableHead>
                <TableHead>{copy.source}</TableHead>
                <TableHead>{copy.origin}</TableHead>
                <TableHead>{copy.destination}</TableHead>
                <TableHead>{copy.protocol}</TableHead>
                <TableHead>{copy.eventType}</TableHead>
                <TableHead>{copy.severity}</TableHead>
                <TableHead>{copy.case}</TableHead>
                <TableHead>
                  <span className="sr-only">{copy.details}</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {!loading && !error && items.length === 0 && (
                <TableRow>
                  <TableCell colSpan={9} className="h-28 text-center text-muted-foreground">
                    {copy.empty}
                  </TableCell>
                </TableRow>
              )}
              {items.map((item) => {
                const fields = trafficFields(item);
                return (
                  <TableRow key={item.id}>
                    <TableCell className="whitespace-nowrap text-xs">{blueDate(item.occurred_at, locale)}</TableCell>
                    <TableCell className="max-w-32 truncate" title={item.source}>
                      {item.source}
                    </TableCell>
                    <TableCell className="max-w-40 truncate font-mono text-xs" title={fields.sourceEndpoint}>
                      {fields.sourceEndpoint || "—"}
                    </TableCell>
                    <TableCell className="max-w-40 truncate font-mono text-xs" title={fields.destinationEndpoint}>
                      {fields.destinationEndpoint || "—"}
                    </TableCell>
                    <TableCell className="font-mono text-xs">{fields.protocol || "—"}</TableCell>
                    <TableCell className="max-w-52">
                      <div className="truncate" title={item.event_type}>
                        {item.event_type}
                      </div>
                      <div
                        className="truncate text-xs text-muted-foreground"
                        title={fields.url || fields.host || item.message}
                      >
                        {fields.url || fields.host || item.message}
                      </div>
                    </TableCell>
                    <TableCell>
                      <BlueSeverityBadge severity={item.severity} locale={locale} />
                    </TableCell>
                    <TableCell>
                      {item.case_id ? (
                        <Link href={`/blue/cases/detail?id=${item.case_id}`} className="hover:underline">
                          #{item.case_id}
                        </Link>
                      ) : (
                        <span className="text-muted-foreground">—</span>
                      )}
                    </TableCell>
                    <TableCell>
                      <Button variant="ghost" size="sm" onClick={() => setSelected(item)}>
                        {copy.details}
                      </Button>
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </div>
        <div className="flex flex-wrap items-center justify-between gap-2 border-t px-4 py-3 text-xs text-muted-foreground">
          <span className="tabular-nums">
            {copy.pagination} {first}–{last} / {total}
          </span>
          <div className="flex items-center gap-2">
            <Button
              variant="outline"
              size="icon-sm"
              aria-label={copy.previous}
              disabled={page === 0 || loading}
              onClick={() => setPage((value) => value - 1)}
            >
              <ChevronLeft aria-hidden="true" />
            </Button>
            <span className="tabular-nums">
              {copy.pageOf} {page + 1} / {pages}
            </span>
            <Button
              variant="outline"
              size="icon-sm"
              aria-label={copy.next}
              disabled={page + 1 >= pages || loading}
              onClick={() => setPage((value) => value + 1)}
            >
              <ChevronRight aria-hidden="true" />
            </Button>
          </div>
        </div>
      </Card>

      <Sheet open={selected !== null} onOpenChange={(open) => !open && setSelected(null)}>
        <SheetContent className="w-full! max-w-none! gap-0 p-0 sm:w-[40rem]! sm:max-w-[40rem]!">
          {selected && selectedFields && (
            <>
              <SheetHeader className="border-b px-5 py-4">
                <SheetTitle>
                  {copy.details} #{selected.id}
                </SheetTitle>
                <SheetDescription>{selected.message}</SheetDescription>
              </SheetHeader>
              <div className="min-h-0 flex-1 space-y-5 overflow-auto p-5 text-sm">
                <dl className="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-2">
                  {[
                    [copy.time, blueDate(selected.occurred_at, locale)],
                    [copy.source, selected.source],
                    [copy.origin, selectedFields.sourceEndpoint || "—"],
                    [copy.destination, selectedFields.destinationEndpoint || "—"],
                    [copy.protocol, selectedFields.protocol || "—"],
                    ...(selectedFields.method ? [[copy.method, selectedFields.method]] : []),
                    ...(selectedFields.host ? [[copy.host, selectedFields.host]] : []),
                    ...(selectedFields.url ? [[copy.url, selectedFields.url]] : []),
                    ...(selectedFields.status ? [[copy.status, selectedFields.status]] : []),
                    ...(selectedFields.action ? [[copy.action, selectedFields.action]] : []),
                    [copy.eventType, selected.event_type],
                    [copy.case, selected.case_id ? `#${selected.case_id}` : copy.noCase],
                  ].map(([label, value]) => (
                    <React.Fragment key={label}>
                      <dt className="text-muted-foreground">{label}</dt>
                      <dd className="min-w-0 break-all font-medium">{value}</dd>
                    </React.Fragment>
                  ))}
                </dl>
                <div className="flex flex-wrap gap-2">
                  <Button asChild variant="outline" size="sm">
                    <Link href={`/blue/events?event=${selected.id}`}>{copy.viewEvent}</Link>
                  </Button>
                  {selected.case_id && (
                    <Button asChild variant="outline" size="sm">
                      <Link href={`/blue/cases/detail?id=${selected.case_id}`}>
                        {copy.case} #{selected.case_id}
                      </Link>
                    </Button>
                  )}
                </div>
                <div>
                  <h2 className="mb-2 font-medium">{copy.raw}</h2>
                  <pre className="max-h-[50vh] overflow-auto rounded-md border bg-muted/30 p-3 font-mono text-xs whitespace-pre-wrap break-all">
                    {JSON.stringify(selected.raw, null, 2)}
                  </pre>
                </div>
              </div>
            </>
          )}
        </SheetContent>
      </Sheet>
    </div>
  );
}

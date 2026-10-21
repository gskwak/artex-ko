"use client";

import * as React from "react";

import Link from "next/link";

import { Archive, Plus, RefreshCw, Search } from "lucide-react";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { useActiveLocale } from "@/i18n/locale-store";
import { type BlueAsset, type BlueAssetActivity, type BlueAssetKind, blueAssetsApi } from "@/lib/blue-assets-api";

import { BlueEmpty, BlueError, BlueLoading, BluePageHeader, blueDate } from "../_components/blue-ui";

const PAGE_SIZE = 100;
const KINDS: BlueAssetKind[] = ["host", "ip", "domain", "service", "application", "other"];
const labels = {
  en: {
    title: "Blue Team assets",
    description: "Identify systems involved in defensive investigations. Red Team assets remain separate.",
    add: "Add asset",
    edit: "Edit asset",
    refresh: "Refresh",
    search: "Search ID, identifier or name",
    all: "All",
    active: "Active",
    archived: "Archived",
    kind: "Type",
    identifier: "Identifier",
    label: "Display name",
    notes: "Notes",
    status: "Status",
    updated: "Updated",
    save: "Save",
    cancel: "Cancel",
    archive: "Archive",
    restore: "Restore",
    loading: "Loading assets…",
    saving: "Saving…",
    more: "Load more",
    view: "View",
    empty: "No assets match this view. Add a system to link it with evidence and cases.",
    select: "Select an asset to view its investigation history.",
    activity: "Related investigation records",
    cases: "Cases",
    events: "Events",
    actions: "Response actions",
    noActivity: "No linked records yet.",
    archiveHelp: "Archived assets keep existing evidence links but cannot receive new ones.",
    saved: "Asset saved.",
    failed: "Could not save the asset.",
    loadFailed: "Could not load assets.",
    kindNames: {
      host: "Host",
      ip: "IP address",
      domain: "Domain",
      service: "Service",
      application: "Application",
      other: "Other",
    },
  },
  ko: {
    title: "블루팀 자산",
    description: "방어 조사에 사용되는 시스템을 식별합니다. 레드팀 자산 목록과는 별개입니다.",
    add: "자산 추가",
    edit: "자산 수정",
    refresh: "새로고침",
    search: "ID, 식별자 또는 이름 검색",
    all: "전체",
    active: "사용 중",
    archived: "보관됨",
    kind: "종류",
    identifier: "식별자",
    label: "표시 이름",
    notes: "메모",
    status: "상태",
    updated: "수정 시각",
    save: "저장",
    cancel: "취소",
    archive: "보관",
    restore: "복원",
    loading: "자산을 불러오는 중…",
    saving: "저장 중…",
    more: "더 보기",
    view: "보기",
    empty: "표시할 자산이 없습니다. 자산을 추가해 증거와 사건에 연결하세요.",
    select: "자산을 선택하면 관련 조사 이력을 확인할 수 있습니다.",
    activity: "관련 조사 기록",
    cases: "사건",
    events: "이벤트",
    actions: "대응 조치",
    noActivity: "연결된 기록이 없습니다.",
    archiveHelp: "보관한 자산의 기존 증거 연결은 유지되며 새 연결은 할 수 없습니다.",
    saved: "자산을 저장했습니다.",
    failed: "자산을 저장하지 못했습니다.",
    loadFailed: "자산을 불러오지 못했습니다.",
    kindNames: {
      host: "호스트",
      ip: "IP 주소",
      domain: "도메인",
      service: "서비스",
      application: "애플리케이션",
      other: "기타",
    },
  },
  zh: {
    title: "蓝队资产",
    description: "识别防御调查中的系统。红队资产清单保持独立。",
    add: "添加资产",
    edit: "编辑资产",
    refresh: "刷新",
    search: "搜索 ID、标识符或名称",
    all: "全部",
    active: "使用中",
    archived: "已归档",
    kind: "类型",
    identifier: "标识符",
    label: "显示名称",
    notes: "备注",
    status: "状态",
    updated: "更新时间",
    save: "保存",
    cancel: "取消",
    archive: "归档",
    restore: "恢复",
    loading: "正在加载资产…",
    saving: "保存中…",
    more: "加载更多",
    view: "查看",
    empty: "没有匹配的资产。添加系统后可将其关联到证据和案件。",
    select: "选择资产以查看相关调查记录。",
    activity: "相关调查记录",
    cases: "案件",
    events: "事件",
    actions: "响应操作",
    noActivity: "尚无关联记录。",
    archiveHelp: "归档资产保留现有证据关联，但不能添加新关联。",
    saved: "资产已保存。",
    failed: "无法保存资产。",
    loadFailed: "无法加载资产。",
    kindNames: { host: "主机", ip: "IP 地址", domain: "域名", service: "服务", application: "应用程序", other: "其他" },
  },
  es: {
    title: "Activos del equipo azul",
    description:
      "Identifique los sistemas de las investigaciones defensivas. Los activos del equipo rojo están separados.",
    add: "Añadir activo",
    edit: "Editar activo",
    refresh: "Actualizar",
    search: "Buscar ID, identificador o nombre",
    all: "Todos",
    active: "Activo",
    archived: "Archivado",
    kind: "Tipo",
    identifier: "Identificador",
    label: "Nombre visible",
    notes: "Notas",
    status: "Estado",
    updated: "Actualizado",
    save: "Guardar",
    cancel: "Cancelar",
    archive: "Archivar",
    restore: "Restaurar",
    loading: "Cargando activos…",
    saving: "Guardando…",
    more: "Cargar más",
    view: "Ver",
    empty: "No hay activos en esta vista. Añada un sistema para vincularlo con pruebas y casos.",
    select: "Seleccione un activo para ver su historial de investigación.",
    activity: "Registros de investigación relacionados",
    cases: "Casos",
    events: "Eventos",
    actions: "Acciones de respuesta",
    noActivity: "No hay registros vinculados.",
    archiveHelp: "Los activos archivados conservan los vínculos existentes, pero no admiten nuevos.",
    saved: "Activo guardado.",
    failed: "No se pudo guardar el activo.",
    loadFailed: "No se pudieron cargar los activos.",
    kindNames: {
      host: "Equipo",
      ip: "Dirección IP",
      domain: "Dominio",
      service: "Servicio",
      application: "Aplicación",
      other: "Otro",
    },
  },
} as const;

type AssetForm = { kind: BlueAssetKind; identifier: string; label: string; notes: string };
const blankForm: AssetForm = { kind: "host", identifier: "", label: "", notes: "" };

export default function BlueAssetsPage() {
  const locale = useActiveLocale();
  const t = labels[locale];
  const [assets, setAssets] = React.useState<BlueAsset[]>([]);
  const [loading, setLoading] = React.useState(true);
  const [loadingMore, setLoadingMore] = React.useState(false);
  const [hasMore, setHasMore] = React.useState(false);
  const [offset, setOffset] = React.useState(0);
  const [error, setError] = React.useState("");
  const [search, setSearch] = React.useState("");
  const [query, setQuery] = React.useState("");
  const [status, setStatus] = React.useState<"all" | "active" | "archived">("all");
  const [selected, setSelected] = React.useState<BlueAsset | null>(null);
  const [activity, setActivity] = React.useState<BlueAssetActivity | null>(null);
  const [detailLoading, setDetailLoading] = React.useState(false);
  const [formOpen, setFormOpen] = React.useState(false);
  const [editing, setEditing] = React.useState<BlueAsset | null>(null);
  const [form, setForm] = React.useState<AssetForm>(blankForm);
  const [saving, setSaving] = React.useState(false);
  const [changingStatus, setChangingStatus] = React.useState(false);
  const listVersion = React.useRef(0);
  const detailVersion = React.useRef(0);

  React.useEffect(() => {
    const timer = window.setTimeout(() => setQuery(search.trim()), 250);
    return () => window.clearTimeout(timer);
  }, [search]);

  const load = React.useCallback(async () => {
    const version = ++listVersion.current;
    setLoading(true);
    setError("");
    try {
      const page = await blueAssetsApi.list({
        q: query,
        status: status === "all" ? undefined : status,
        limit: PAGE_SIZE + 1,
      });
      if (version !== listVersion.current) return;
      setAssets(page.slice(0, PAGE_SIZE));
      setOffset(Math.min(page.length, PAGE_SIZE));
      setHasMore(page.length > PAGE_SIZE);
    } catch (cause) {
      if (version === listVersion.current) setError(cause instanceof Error ? cause.message : t.loadFailed);
    } finally {
      if (version === listVersion.current) setLoading(false);
    }
  }, [query, status, t.loadFailed]);

  React.useEffect(() => {
    void load();
  }, [load]);

  async function loadMore() {
    if (loadingMore || !hasMore) return;
    const version = listVersion.current;
    setLoadingMore(true);
    try {
      const page = await blueAssetsApi.list({
        q: query,
        status: status === "all" ? undefined : status,
        limit: PAGE_SIZE + 1,
        offset,
      });
      if (version !== listVersion.current) return;
      setAssets((current) => {
        const known = new Set(current.map((item) => item.id));
        return [...current, ...page.slice(0, PAGE_SIZE).filter((item) => !known.has(item.id))];
      });
      setOffset(offset + Math.min(page.length, PAGE_SIZE));
      setHasMore(page.length > PAGE_SIZE);
    } catch (cause) {
      if (version === listVersion.current) toast.error(cause instanceof Error ? cause.message : t.loadFailed);
    } finally {
      if (version === listVersion.current) setLoadingMore(false);
    }
  }

  const selectAsset = React.useCallback(
    async (id: number) => {
      const version = ++detailVersion.current;
      setDetailLoading(true);
      setActivity(null);
      try {
        const [asset, history] = await Promise.all([blueAssetsApi.get(id), blueAssetsApi.activity(id)]);
        if (version !== detailVersion.current) return;
        setSelected(asset);
        setActivity(history);
      } catch (cause) {
        if (version === detailVersion.current) toast.error(cause instanceof Error ? cause.message : t.loadFailed);
      } finally {
        if (version === detailVersion.current) setDetailLoading(false);
      }
    },
    [t.loadFailed],
  );

  React.useEffect(() => {
    const raw = new URLSearchParams(window.location.search).get("asset");
    if (!raw || !/^\d+$/.test(raw)) return;
    const id = Number(raw);
    if (Number.isSafeInteger(id) && id > 0) void selectAsset(id);
  }, [selectAsset]);

  function openForm(asset?: BlueAsset) {
    setEditing(asset ?? null);
    setForm(
      asset ? { kind: asset.kind, identifier: asset.identifier, label: asset.label, notes: asset.notes } : blankForm,
    );
    setFormOpen(true);
  }

  async function saveAsset(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!form.identifier.trim() || !form.label.trim()) return;
    setSaving(true);
    try {
      const payload = {
        kind: form.kind,
        identifier: form.identifier.trim(),
        label: form.label.trim(),
        notes: form.notes.trim(),
      };
      const asset = editing ? await blueAssetsApi.patch(editing.id, payload) : await blueAssetsApi.create(payload);
      setFormOpen(false);
      void load();
      void selectAsset(asset.id);
      toast.success(t.saved);
    } catch (cause) {
      toast.error(cause instanceof Error ? cause.message : t.failed);
    } finally {
      setSaving(false);
    }
  }

  async function changeStatus(asset: BlueAsset) {
    setChangingStatus(true);
    try {
      const updated = await blueAssetsApi.patch(asset.id, {
        status: asset.status === "active" ? "archived" : "active",
      });
      setSelected(updated);
      void load();
      toast.success(t.saved);
    } catch (cause) {
      toast.error(cause instanceof Error ? cause.message : t.failed);
    } finally {
      setChangingStatus(false);
    }
  }

  return (
    <div className="space-y-5 pb-6">
      <BluePageHeader
        title={t.title}
        description={t.description}
        actions={
          <>
            <Button size="sm" variant="outline" onClick={() => void load()} disabled={loading}>
              <RefreshCw className={loading ? "animate-spin" : ""} />
              {t.refresh}
            </Button>
            <Button size="sm" onClick={() => openForm()}>
              <Plus />
              {t.add}
            </Button>
          </>
        }
      />

      <Card>
        <CardContent className="flex flex-wrap items-center gap-3 pt-6">
          <div className="relative min-w-56 flex-1">
            <Search
              className="pointer-events-none absolute top-2.5 left-3 size-4 text-muted-foreground"
              aria-hidden="true"
            />
            <Input
              aria-label={t.search}
              placeholder={t.search}
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              className="pl-9"
            />
          </div>
          <Label htmlFor="blue-asset-status" className="sr-only">
            {t.status}
          </Label>
          <select
            id="blue-asset-status"
            value={status}
            onChange={(event) => setStatus(event.target.value as typeof status)}
            className="h-9 rounded-md border border-input bg-background px-3 text-sm"
          >
            <option value="all">{t.all}</option>
            <option value="active">{t.active}</option>
            <option value="archived">{t.archived}</option>
          </select>
        </CardContent>
      </Card>

      {error && <BlueError message={error} retry={() => void load()} retryLabel={t.refresh} />}
      {loading ? (
        <BlueLoading label={t.loading} />
      ) : assets.length === 0 ? (
        <BlueEmpty message={t.empty} />
      ) : (
        <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_minmax(20rem,0.7fr)]">
          <div className="space-y-3">
            <Card>
              <CardContent className="divide-y p-0">
                {assets.map((asset) => (
                  <div key={asset.id} className="flex flex-wrap items-center justify-between gap-3 px-4 py-3">
                    <div className="min-w-0">
                      <div className="font-medium break-all">{asset.label || asset.identifier}</div>
                      <div className="text-xs text-muted-foreground break-all">
                        #{asset.id} · {asset.identifier} · {t.kindNames[asset.kind]}
                      </div>
                    </div>
                    <div className="flex items-center gap-2">
                      <Badge variant={asset.status === "active" ? "secondary" : "outline"}>{t[asset.status]}</Badge>
                      <Button
                        size="sm"
                        variant={selected?.id === asset.id ? "secondary" : "outline"}
                        onClick={() => void selectAsset(asset.id)}
                      >
                        {t.view}
                      </Button>
                    </div>
                  </div>
                ))}
              </CardContent>
            </Card>
            {hasMore && (
              <Button variant="outline" onClick={() => void loadMore()} disabled={loadingMore}>
                {t.more}
              </Button>
            )}
          </div>

          <Card className="h-fit">
            {detailLoading ? (
              <BlueLoading label={t.loading} />
            ) : selected ? (
              <>
                <CardHeader>
                  <div className="flex flex-wrap items-start justify-between gap-2">
                    <div>
                      <CardTitle className="break-all">{selected.label || selected.identifier}</CardTitle>
                      <p className="mt-1 text-xs text-muted-foreground break-all">
                        #{selected.id} · {selected.identifier}
                      </p>
                    </div>
                    <Badge variant={selected.status === "active" ? "secondary" : "outline"}>{t[selected.status]}</Badge>
                  </div>
                </CardHeader>
                <CardContent className="space-y-5 text-sm">
                  <dl className="grid grid-cols-[7rem_1fr] gap-2">
                    <dt className="text-muted-foreground">{t.kind}</dt>
                    <dd>{t.kindNames[selected.kind]}</dd>
                    <dt className="text-muted-foreground">{t.notes}</dt>
                    <dd className="whitespace-pre-wrap break-words">{selected.notes || "—"}</dd>
                    <dt className="text-muted-foreground">{t.updated}</dt>
                    <dd>{blueDate(selected.updated_at, locale)}</dd>
                  </dl>
                  <div className="flex flex-wrap gap-2">
                    <Button size="sm" variant="outline" onClick={() => openForm(selected)}>
                      {t.edit}
                    </Button>
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => void changeStatus(selected)}
                      disabled={changingStatus}
                    >
                      <Archive className="size-4" />
                      {selected.status === "active" ? t.archive : t.restore}
                    </Button>
                  </div>
                  <p className="text-xs text-muted-foreground">{t.archiveHelp}</p>
                  <div className="space-y-3 border-t pt-4">
                    <h2 className="font-semibold">{t.activity}</h2>
                    {activity && activity.cases.length + activity.events.length + activity.actions.length === 0 && (
                      <p className="text-muted-foreground">{t.noActivity}</p>
                    )}
                    {activity && activity.cases.length > 0 && (
                      <section className="space-y-1">
                        <h3 className="font-medium">
                          {t.cases} · {activity.case_count ?? activity.cases.length}
                        </h3>
                        {activity.cases.map((item) => (
                          <Link
                            key={item.id}
                            href={`/blue/cases/detail?id=${item.id}`}
                            className="block text-primary hover:underline"
                          >
                            #{item.id} {item.title}
                          </Link>
                        ))}
                      </section>
                    )}
                    {activity && activity.events.length > 0 && (
                      <section className="space-y-1">
                        <h3 className="font-medium">
                          {t.events} · {activity.event_count ?? activity.events.length}
                        </h3>
                        {activity.events.map((item) => (
                          <Link
                            key={item.id}
                            href={`/blue/events?event=${item.id}`}
                            className="block text-primary hover:underline"
                          >
                            #{item.id} {item.event_type}
                          </Link>
                        ))}
                      </section>
                    )}
                    {activity && activity.actions.length > 0 && (
                      <section className="space-y-1">
                        <h3 className="font-medium">
                          {t.actions} · {activity.action_count ?? activity.actions.length}
                        </h3>
                        {activity.actions.map((item) => (
                          <Link key={item.id} href="/blue/actions" className="block text-primary hover:underline">
                            #{item.id} {item.kind}
                          </Link>
                        ))}
                      </section>
                    )}
                  </div>
                </CardContent>
              </>
            ) : (
              <CardContent className="py-10 text-center text-sm text-muted-foreground">{t.select}</CardContent>
            )}
          </Card>
        </div>
      )}

      <Dialog open={formOpen} onOpenChange={setFormOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{editing ? t.edit : t.add}</DialogTitle>
            <DialogDescription>{t.description}</DialogDescription>
          </DialogHeader>
          <form className="space-y-4" onSubmit={(event) => void saveAsset(event)}>
            <div className="space-y-1.5">
              <Label htmlFor="blue-asset-kind">{t.kind}</Label>
              <select
                id="blue-asset-kind"
                value={form.kind}
                onChange={(event) => setForm({ ...form, kind: event.target.value as BlueAssetKind })}
                className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm"
              >
                {KINDS.map((kind) => (
                  <option key={kind} value={kind}>
                    {t.kindNames[kind]}
                  </option>
                ))}
              </select>
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="blue-asset-identifier">{t.identifier}</Label>
              <Input
                id="blue-asset-identifier"
                required
                maxLength={500}
                value={form.identifier}
                onChange={(event) => setForm({ ...form, identifier: event.target.value })}
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="blue-asset-label">{t.label}</Label>
              <Input
                id="blue-asset-label"
                required
                maxLength={200}
                value={form.label}
                onChange={(event) => setForm({ ...form, label: event.target.value })}
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="blue-asset-notes">{t.notes}</Label>
              <Textarea
                id="blue-asset-notes"
                maxLength={10000}
                rows={4}
                value={form.notes}
                onChange={(event) => setForm({ ...form, notes: event.target.value })}
              />
            </div>
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setFormOpen(false)}>
                {t.cancel}
              </Button>
              <Button type="submit" disabled={saving || !form.identifier.trim() || !form.label.trim()}>
                {saving ? t.saving : t.save}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  );
}

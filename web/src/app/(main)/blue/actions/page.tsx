"use client";

import * as React from "react";

import Link from "next/link";

import { Check, RefreshCw, X } from "lucide-react";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";
import { useActiveLocale } from "@/i18n/locale-store";
import { type BlueOverview, blueRequest } from "@/lib/blue-api";

import { BlueEmpty, BlueError, BlueLoading, BluePageHeader, blueDate } from "../_components/blue-ui";
import { BlueInvestigationTabs } from "../_components/investigation-tabs";

type ActionStatus = "pending" | "approved" | "rejected" | "completed" | "failed";
type StatusFilter = "all" | ActionStatus;
type RecordMode = "approve" | "reject" | "complete" | "fail";
const PAGE_SIZE = 200;

const STATUS_BY_RECORD_MODE: Record<RecordMode, ActionStatus> = {
  approve: "approved",
  reject: "rejected",
  complete: "completed",
  fail: "failed",
};

const ACTION_KIND_LABELS: Record<string, Record<string, string>> = {
  en: {
    isolate_host: "Isolate host",
    block_ip: "Block IP",
    reset_credential: "Reset credential",
    rotate_secret: "Rotate secret",
    patch_system: "Patch system",
    custom: "Custom",
  },
  ko: {
    isolate_host: "호스트 격리",
    block_ip: "IP 차단",
    reset_credential: "자격 증명 재설정",
    rotate_secret: "비밀값 교체",
    patch_system: "시스템 패치",
    custom: "사용자 지정",
  },
  zh: {
    isolate_host: "隔离主机",
    block_ip: "封禁 IP",
    reset_credential: "重置凭据",
    rotate_secret: "轮换密钥",
    patch_system: "系统补丁",
    custom: "自定义",
  },
  es: {
    isolate_host: "Aislar equipo",
    block_ip: "Bloquear IP",
    reset_credential: "Restablecer credenciales",
    rotate_secret: "Rotar secreto",
    patch_system: "Aplicar parche",
    custom: "Personalizada",
  },
};

function actionKindLabel(kind: string, locale: string): string {
  return ACTION_KIND_LABELS[locale]?.[kind] ?? kind.replaceAll("_", " ");
}

interface BlueAction {
  id: number;
  case_id: number;
  kind: string;
  target: string;
  rationale: string;
  status: ActionStatus;
  decision_note: string;
  params: Record<string, unknown> | null;
  requested_by: string;
  decided_by: string | null;
  created_at: string;
  decided_at: string | null;
  completed_by?: string | null;
  completed_at?: string | null;
  completion_note?: string | null;
  completion_evidence?: string | null;
}

const EN = {
  title: "Response approvals",
  subtitle: "Review proposed containment or remediation steps and record an auditable decision.",
  boundary:
    "Approval records a decision only. Completion records an operator-reported outcome. ARTEX does not execute external response actions from this page.",
  refresh: "Refresh",
  loadMore: "Load more proposals",
  filter: "Status",
  all: "All",
  pending: "Pending",
  approved: "Approved",
  rejected: "Rejected",
  completed: "Completed",
  failed: "Failed",
  empty: "No response proposals in this view. Proposals are created inside a case.",
  case: "Case",
  action: "Action",
  target: "Target",
  rationale: "Rationale",
  status: "Status",
  requested: "Requested",
  decision: "Decision",
  approve: "Approve",
  reject: "Reject",
  complete: "Record completion",
  fail: "Record failure",
  approveTitle: "Approve proposal",
  rejectTitle: "Reject proposal",
  completeTitle: "Record completed action",
  failTitle: "Record failed action",
  decisionDesc: "Record your decision and its reason. This does not perform the proposed action.",
  completionDesc:
    "Record what an operator actually did and the observed outcome. This form does not perform the action.",
  note: "Decision note",
  completionNote: "Outcome note",
  notePlaceholder: "Explain the basis for your decision",
  completionNotePlaceholder: "Describe the action performed and its result",
  noteRequired: "Add a decision note so the approval record is reviewable.",
  completionNoteRequired: "Describe the observed outcome before recording it.",
  completionEvidence: "Evidence reference (optional)",
  evidencePlaceholder: "Ticket, log, change record, or evidence URL",
  cancel: "Cancel",
  confirmApprove: "Record approval",
  confirmReject: "Record rejection",
  confirmComplete: "Record completion",
  confirmFail: "Record failure",
  approvedNotice: "Approval recorded. Perform and verify the action outside ARTEX before recording completion.",
  rejectedNotice: "The proposed action was rejected.",
  completedNotice: "An operator recorded that the action was completed; review its evidence.",
  failedNotice: "An operator recorded that the action did not complete successfully.",
  parameters: "Proposed parameters",
  requestedBy: "Requested by",
  decidedBy: "Decided by",
  loadError: "Could not load response proposals",
  decisionError: "Could not record decision",
  decisionSaved: "Decision recorded",
  loading: "Loading response proposals…",
} as const;

type Copy = { [K in keyof typeof EN]: string };

const KO: Copy = {
  title: "대응 승인",
  subtitle: "격리·복구 제안을 검토하고 감사 가능한 결정을 기록합니다.",
  boundary:
    "승인은 결정만 기록하고, 완료는 담당자가 보고한 결과를 기록합니다. 이 화면에서 ARTEX가 외부 시스템에 대응 조치를 실행하지는 않습니다.",
  refresh: "새로고침",
  loadMore: "제안 더 보기",
  filter: "상태",
  all: "전체",
  pending: "대기 중",
  approved: "승인됨",
  rejected: "거절됨",
  completed: "완료 기록됨",
  failed: "실패 기록됨",
  empty: "표시할 대응 제안이 없습니다. 사건 상세 화면에서 제안을 만들 수 있습니다.",
  case: "사건",
  action: "조치",
  target: "대상",
  rationale: "근거",
  status: "상태",
  requested: "요청 시각",
  decision: "결정",
  approve: "승인",
  reject: "거절",
  complete: "완료 기록",
  fail: "실패 기록",
  approveTitle: "대응 제안 승인",
  rejectTitle: "대응 제안 거절",
  completeTitle: "완료 결과 기록",
  failTitle: "실패 결과 기록",
  decisionDesc: "결정과 근거를 기록합니다. 제안된 조치를 실제로 실행하지는 않습니다.",
  completionDesc: "담당자가 실제 수행한 내용과 관찰된 결과를 기록합니다. 이 양식이 조치를 실행하지는 않습니다.",
  note: "결정 메모",
  completionNote: "결과 메모",
  notePlaceholder: "결정 근거를 적어주세요",
  completionNotePlaceholder: "수행한 조치와 그 결과를 적어주세요",
  noteRequired: "승인 기록을 검토할 수 있도록 결정 메모를 입력하세요.",
  completionNoteRequired: "관찰한 결과를 입력한 뒤 기록하세요.",
  completionEvidence: "증거 참조 (선택 사항)",
  evidencePlaceholder: "티켓, 로그, 변경 기록 또는 증거 URL",
  cancel: "취소",
  confirmApprove: "승인 기록",
  confirmReject: "거절 기록",
  confirmComplete: "완료 기록",
  confirmFail: "실패 기록",
  approvedNotice: "승인을 기록했습니다. ARTEX 밖에서 조치를 수행하고 검증한 뒤 완료 결과를 기록하세요.",
  rejectedNotice: "제안된 조치가 거절됐습니다.",
  completedNotice: "담당자가 조치 완료를 기록했습니다. 증거를 검토하세요.",
  failedNotice: "담당자가 조치 미완료 또는 실패를 기록했습니다.",
  parameters: "제안된 매개변수",
  requestedBy: "요청자",
  decidedBy: "결정자",
  loadError: "대응 제안을 불러오지 못했습니다",
  decisionError: "결정을 기록하지 못했습니다",
  decisionSaved: "결정을 기록했습니다",
  loading: "대응 제안을 불러오는 중…",
};

const ZH: Copy = {
  title: "响应审批",
  subtitle: "审查隔离或修复建议，并记录可审计的决定。",
  boundary: "审批仅记录决定；完成状态记录操作人员报告的结果。ARTEX 不会从此页面执行外部响应操作。",
  refresh: "刷新",
  loadMore: "加载更多建议",
  filter: "状态",
  all: "全部",
  pending: "待审批",
  approved: "已批准",
  rejected: "已拒绝",
  completed: "已记录完成",
  failed: "已记录失败",
  empty: "此视图中没有响应建议。可在事件案件中创建建议。",
  case: "案件",
  action: "操作",
  target: "目标",
  rationale: "依据",
  status: "状态",
  requested: "请求时间",
  decision: "决定",
  approve: "批准",
  reject: "拒绝",
  complete: "记录完成",
  fail: "记录失败",
  approveTitle: "批准建议",
  rejectTitle: "拒绝建议",
  completeTitle: "记录已完成的操作",
  failTitle: "记录失败的操作",
  decisionDesc: "记录您的决定及理由。此操作不会执行建议的响应措施。",
  completionDesc: "记录操作人员实际采取的措施和观察到的结果。此表单不会执行该操作。",
  note: "决定备注",
  completionNote: "结果备注",
  notePlaceholder: "说明做出此决定的依据",
  completionNotePlaceholder: "描述已采取的措施及其结果",
  noteRequired: "请填写决定备注，以便审查审批记录。",
  completionNoteRequired: "请描述观察到的结果后再记录。",
  completionEvidence: "证据引用（可选）",
  evidencePlaceholder: "工单、日志、变更记录或证据链接",
  cancel: "取消",
  confirmApprove: "记录批准",
  confirmReject: "记录拒绝",
  confirmComplete: "记录完成",
  confirmFail: "记录失败",
  approvedNotice: "批准已记录。请在 ARTEX 外执行并验证该操作，然后记录完成结果。",
  rejectedNotice: "建议的操作已被拒绝。",
  completedNotice: "操作人员已记录操作完成；请审查其证据。",
  failedNotice: "操作人员已记录操作未能成功完成。",
  parameters: "建议的参数",
  requestedBy: "请求人",
  decidedBy: "决定人",
  loadError: "无法加载响应建议",
  decisionError: "无法记录决定",
  decisionSaved: "决定已记录",
  loading: "正在加载响应建议…",
};

const ES: Copy = {
  title: "Aprobaciones de respuesta",
  subtitle: "Revisa las medidas propuestas de contención o remediación y registra una decisión auditable.",
  boundary:
    "La aprobación solo registra una decisión. La finalización registra el resultado informado por un operador. ARTEX no ejecuta acciones de respuesta externas desde esta página.",
  refresh: "Actualizar",
  loadMore: "Cargar más propuestas",
  filter: "Estado",
  all: "Todos",
  pending: "Pendiente",
  approved: "Aprobada",
  rejected: "Rechazada",
  completed: "Finalización registrada",
  failed: "Fallo registrado",
  empty: "No hay propuestas de respuesta en esta vista. Las propuestas se crean dentro de un caso.",
  case: "Caso",
  action: "Acción",
  target: "Objetivo",
  rationale: "Justificación",
  status: "Estado",
  requested: "Solicitada",
  decision: "Decisión",
  approve: "Aprobar",
  reject: "Rechazar",
  complete: "Registrar finalización",
  fail: "Registrar fallo",
  approveTitle: "Aprobar propuesta",
  rejectTitle: "Rechazar propuesta",
  completeTitle: "Registrar acción completada",
  failTitle: "Registrar acción fallida",
  decisionDesc: "Registra tu decisión y su motivo. Esto no ejecuta la acción propuesta.",
  completionDesc:
    "Registra lo que hizo realmente un operador y el resultado observado. Este formulario no ejecuta la acción.",
  note: "Nota de decisión",
  completionNote: "Nota de resultado",
  notePlaceholder: "Explica el fundamento de tu decisión",
  completionNotePlaceholder: "Describe la acción realizada y su resultado",
  noteRequired: "Añade una nota para que se pueda revisar el registro de aprobación.",
  completionNoteRequired: "Describe el resultado observado antes de registrarlo.",
  completionEvidence: "Referencia de evidencia (opcional)",
  evidencePlaceholder: "Ticket, registro, cambio o URL de evidencia",
  cancel: "Cancelar",
  confirmApprove: "Registrar aprobación",
  confirmReject: "Registrar rechazo",
  confirmComplete: "Registrar finalización",
  confirmFail: "Registrar fallo",
  approvedNotice:
    "Aprobación registrada. Ejecuta y verifica la acción fuera de ARTEX antes de registrar su finalización.",
  rejectedNotice: "La acción propuesta fue rechazada.",
  completedNotice: "Un operador registró que la acción se completó; revisa la evidencia.",
  failedNotice: "Un operador registró que la acción no se completó correctamente.",
  parameters: "Parámetros propuestos",
  requestedBy: "Solicitada por",
  decidedBy: "Decidida por",
  loadError: "No se pudieron cargar las propuestas de respuesta",
  decisionError: "No se pudo registrar la decisión",
  decisionSaved: "Decisión registrada",
  loading: "Cargando propuestas de respuesta…",
};

const COPY_BY_LOCALE: Record<string, Copy> = { en: EN, ko: KO, zh: ZH, es: ES };

function statusBadge(status: ActionStatus, label: string) {
  if (status === "approved" || status === "completed") return <Badge variant="secondary">{label}</Badge>;
  if (status === "rejected" || status === "failed") return <Badge variant="destructive">{label}</Badge>;
  return (
    <Badge variant="outline" className="border-amber-500/60 text-amber-700 dark:text-amber-300">
      {label}
    </Badge>
  );
}

export default function BlueActionsPage() {
  const locale = useActiveLocale();
  const t = COPY_BY_LOCALE[locale] ?? EN;
  const statusNotice: Record<ActionStatus, string> = {
    pending: "",
    approved: t.approvedNotice,
    rejected: t.rejectedNotice,
    completed: t.completedNotice,
    failed: t.failedNotice,
  };
  const dialogTitles: Record<RecordMode, string> = {
    approve: t.approveTitle,
    reject: t.rejectTitle,
    complete: t.completeTitle,
    fail: t.failTitle,
  };
  const confirmLabels: Record<RecordMode, string> = {
    approve: t.confirmApprove,
    reject: t.confirmReject,
    complete: t.confirmComplete,
    fail: t.confirmFail,
  };
  const [actions, setActions] = React.useState<BlueAction[]>([]);
  const [loading, setLoading] = React.useState(true);
  const [loadingMore, setLoadingMore] = React.useState(false);
  const [hasMore, setHasMore] = React.useState(false);
  const [error, setError] = React.useState("");
  const [pendingCount, setPendingCount] = React.useState<number | null>(null);
  const nextOffset = React.useRef(0);
  const requestVersion = React.useRef(0);
  const [filter, setFilter] = React.useState<StatusFilter>("pending");
  const [selected, setSelected] = React.useState<BlueAction | null>(null);
  const [recordMode, setRecordMode] = React.useState<RecordMode>("approve");
  const [note, setNote] = React.useState("");
  const [evidence, setEvidence] = React.useState("");
  const [saving, setSaving] = React.useState(false);

  const load = React.useCallback(async () => {
    const version = ++requestVersion.current;
    setLoading(true);
    setLoadingMore(false);
    setError("");
    setActions([]);
    setHasMore(false);
    nextOffset.current = 0;
    try {
      const statusQuery = filter === "all" ? "" : `&status=${filter}`;
      const rows = await blueRequest<BlueAction[]>(`/actions?limit=${PAGE_SIZE}${statusQuery}`);
      if (version !== requestVersion.current) return;
      const page = Array.isArray(rows) ? rows : [];
      setActions(page);
      nextOffset.current = page.length;
      setHasMore(page.length === PAGE_SIZE);
      void blueRequest<BlueOverview>("/overview")
        .then((overview) => {
          if (version === requestVersion.current) setPendingCount(overview.pending_action_count);
        })
        .catch(() => {
          if (version === requestVersion.current) setPendingCount(null);
        });
    } catch (error) {
      if (version === requestVersion.current) {
        setPendingCount(null);
        setError(error instanceof Error ? `${t.loadError}: ${error.message}` : t.loadError);
      }
    } finally {
      if (version === requestVersion.current) setLoading(false);
    }
  }, [filter, t.loadError]);

  async function loadMore() {
    const version = requestVersion.current;
    setLoadingMore(true);
    try {
      const statusQuery = filter === "all" ? "" : `&status=${filter}`;
      const rows = await blueRequest<BlueAction[]>(
        `/actions?limit=${PAGE_SIZE}&offset=${nextOffset.current}${statusQuery}`,
      );
      if (version !== requestVersion.current) return;
      const page = Array.isArray(rows) ? rows : [];
      nextOffset.current += page.length;
      setActions((current) => {
        const seen = new Set(current.map((action) => action.id));
        return [...current, ...page.filter((action) => !seen.has(action.id))];
      });
      setHasMore(page.length === PAGE_SIZE);
    } catch (error) {
      if (version === requestVersion.current) {
        toast.error(error instanceof Error ? `${t.loadError}: ${error.message}` : t.loadError);
      }
    } finally {
      if (version === requestVersion.current) setLoadingMore(false);
    }
  }

  React.useEffect(() => {
    void load();
  }, [load]);

  const visible = actions;

  function openDecision(action: BlueAction, mode: RecordMode) {
    setSelected(action);
    setRecordMode(mode);
    setNote("");
    setEvidence("");
  }

  async function decide() {
    if (!selected) return;
    if (!note.trim()) {
      toast.error(recordMode === "approve" || recordMode === "reject" ? t.noteRequired : t.completionNoteRequired);
      return;
    }
    setSaving(true);
    try {
      if (recordMode === "approve" || recordMode === "reject") {
        await blueRequest<BlueAction>(`/actions/${selected.id}/decision`, {
          method: "POST",
          body: JSON.stringify({ approved: recordMode === "approve", note: note.trim() }),
        });
      } else {
        await blueRequest<BlueAction>(`/actions/${selected.id}/complete`, {
          method: "POST",
          body: JSON.stringify({ success: recordMode === "complete", note: note.trim(), evidence: evidence.trim() }),
        });
      }
      setFilter(STATUS_BY_RECORD_MODE[recordMode]);
      setSelected(null);
      toast.success(t.decisionSaved);
    } catch (error) {
      toast.error(error instanceof Error ? `${t.decisionError}: ${error.message}` : t.decisionError);
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="space-y-5 pb-6">
      <BluePageHeader
        title={t.title}
        description={t.subtitle}
        actions={
          <>
            <Button variant="outline" size="sm" onClick={() => void load()} disabled={loading}>
              <RefreshCw aria-hidden="true" className={loading ? "animate-spin" : ""} />
              {t.refresh}
            </Button>
            <BlueInvestigationTabs active="actions" />
          </>
        }
      />

      <div role="note" className="rounded-lg border bg-muted/20 px-3 py-2 text-xs text-muted-foreground">
        {t.boundary}
      </div>

      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-sm text-muted-foreground">
          {t.pending}: <span className="font-medium text-foreground">{pendingCount ?? "—"}</span>
        </p>
        <div className="flex items-center gap-2">
          <Label htmlFor="blue-action-filter" className="text-sm text-muted-foreground">
            {t.filter}
          </Label>
          <select
            id="blue-action-filter"
            className="h-9 rounded-md border border-input bg-background px-3 text-sm"
            value={filter}
            onChange={(event) => setFilter(event.target.value as StatusFilter)}
          >
            <option value="pending">{t.pending}</option>
            <option value="all">{t.all}</option>
            <option value="approved">{t.approved}</option>
            <option value="rejected">{t.rejected}</option>
            <option value="completed">{t.completed}</option>
            <option value="failed">{t.failed}</option>
          </select>
        </div>
      </div>

      {error && <BlueError message={error} retry={() => void load()} retryLabel={t.refresh} />}
      {loading && <BlueLoading label={t.loading} />}
      {!loading && !error && visible.length === 0 && <BlueEmpty message={t.empty} />}
      {visible.length > 0 && (
        <Card className="gap-0 overflow-hidden p-0">
          <div className="overflow-x-auto">
            <Table className="min-w-[900px]">
              <TableHeader>
                <TableRow>
                  <TableHead>{t.case}</TableHead>
                  <TableHead>{t.action}</TableHead>
                  <TableHead>{t.target}</TableHead>
                  <TableHead>{t.status}</TableHead>
                  <TableHead>{t.requested}</TableHead>
                  <TableHead className="text-right">{t.decision}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {visible.map((action) => (
                  <TableRow key={action.id}>
                    <TableCell>
                      <Link
                        className="text-primary underline-offset-2 hover:underline"
                        href={`/blue/cases/detail?id=${action.case_id}`}
                      >
                        #{action.case_id}
                      </Link>
                    </TableCell>
                    <TableCell className="min-w-52">
                      <div className="font-medium">{actionKindLabel(action.kind, locale)}</div>
                      {action.rationale && (
                        <p className="mt-1 max-w-80 text-xs text-muted-foreground">{action.rationale}</p>
                      )}
                      {action.requested_by && (
                        <p className="mt-1 text-xs text-muted-foreground">
                          {t.requestedBy}: {action.requested_by}
                        </p>
                      )}
                    </TableCell>
                    <TableCell className="max-w-56 break-all font-mono text-xs">{action.target || "—"}</TableCell>
                    <TableCell>
                      {statusBadge(action.status, t[action.status])}
                      {action.status !== "pending" && (
                        <p className="mt-1 max-w-56 text-xs text-muted-foreground">{statusNotice[action.status]}</p>
                      )}
                      {action.status !== "pending" && action.decision_note && (
                        <p className="mt-1 max-w-56 text-xs">{action.decision_note}</p>
                      )}
                      {action.decided_at && (
                        <p className="mt-1 max-w-56 text-xs text-muted-foreground">
                          {t.decidedBy}: {action.decided_by || "—"} · {blueDate(action.decided_at, locale)}
                        </p>
                      )}
                    </TableCell>
                    <TableCell className="whitespace-nowrap text-xs">{blueDate(action.created_at, locale)}</TableCell>
                    <TableCell>
                      {action.status === "pending" ? (
                        <div className="flex justify-end gap-1.5">
                          <Button size="sm" variant="outline" onClick={() => openDecision(action, "reject")}>
                            <X className="size-3.5" /> {t.reject}
                          </Button>
                          <Button size="sm" onClick={() => openDecision(action, "approve")}>
                            <Check className="size-3.5" /> {t.approve}
                          </Button>
                        </div>
                      ) : action.status === "approved" ? (
                        <div className="flex justify-end gap-1.5">
                          <Button size="sm" variant="outline" onClick={() => openDecision(action, "fail")}>
                            {t.fail}
                          </Button>
                          <Button size="sm" onClick={() => openDecision(action, "complete")}>
                            {t.complete}
                          </Button>
                        </div>
                      ) : (
                        <div className="max-w-64 text-right text-xs text-muted-foreground">
                          {action.status === "rejected" && <span>—</span>}
                          {action.completion_note && <p className="mt-1">{action.completion_note}</p>}
                          {action.completion_evidence && <p className="mt-1 break-all">{action.completion_evidence}</p>}
                          {action.completed_at && (
                            <p>
                              {action.completed_by || "—"} · {blueDate(action.completed_at, locale)}
                            </p>
                          )}
                        </div>
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
          {hasMore && !loading && (
            <div className="flex justify-center border-t px-4 py-3">
              <Button variant="outline" onClick={() => void loadMore()} disabled={loadingMore}>
                {t.loadMore}
                {loadingMore ? "…" : ""}
              </Button>
            </div>
          )}
        </Card>
      )}

      <Dialog open={selected !== null} onOpenChange={(open) => !open && setSelected(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{dialogTitles[recordMode]}</DialogTitle>
            <DialogDescription>
              {recordMode === "approve" || recordMode === "reject" ? t.decisionDesc : t.completionDesc}
            </DialogDescription>
          </DialogHeader>
          {selected && (
            <div className="space-y-3 py-2 text-sm">
              <p>
                <span className="font-medium">{t.action}:</span> {actionKindLabel(selected.kind, locale)}
              </p>
              <p>
                <span className="font-medium">{t.target}:</span> {selected.target || "—"}
              </p>
              <p>
                <span className="font-medium">{t.rationale}:</span> {selected.rationale || "—"}
              </p>
              {selected.params && Object.keys(selected.params).length > 0 && (
                <details className="rounded-md border px-3 py-2">
                  <summary className="cursor-pointer font-medium">{t.parameters}</summary>
                  <pre className="mt-2 max-h-40 overflow-auto whitespace-pre-wrap break-all text-xs">
                    {JSON.stringify(selected.params, null, 2)}
                  </pre>
                </details>
              )}
              <div className="space-y-1.5">
                <Label htmlFor="blue-action-note">
                  {recordMode === "approve" || recordMode === "reject" ? t.note : t.completionNote}
                </Label>
                <Textarea
                  id="blue-action-note"
                  rows={3}
                  value={note}
                  onChange={(event) => setNote(event.target.value)}
                  placeholder={
                    recordMode === "approve" || recordMode === "reject"
                      ? t.notePlaceholder
                      : t.completionNotePlaceholder
                  }
                />
              </div>
              {(recordMode === "complete" || recordMode === "fail") && (
                <div className="space-y-1.5">
                  <Label htmlFor="blue-action-evidence">{t.completionEvidence}</Label>
                  <Textarea
                    id="blue-action-evidence"
                    rows={2}
                    value={evidence}
                    onChange={(event) => setEvidence(event.target.value)}
                    placeholder={t.evidencePlaceholder}
                  />
                </div>
              )}
            </div>
          )}
          <DialogFooter>
            <Button variant="outline" onClick={() => setSelected(null)} disabled={saving}>
              {t.cancel}
            </Button>
            <Button
              variant={recordMode === "reject" || recordMode === "fail" ? "destructive" : "default"}
              onClick={() => void decide()}
              disabled={saving || !note.trim()}
            >
              {confirmLabels[recordMode]}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

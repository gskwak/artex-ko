"use client";

import * as React from "react";

import { FlaskConical, Play, Plus, RefreshCw, ShieldCheck } from "lucide-react";
import { useLocale } from "next-intl";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
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
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";
import { blueRequest } from "@/lib/blue-api";
import { blueSeverityLabel } from "@/lib/blue-copy";

type Severity = "info" | "low" | "medium" | "high" | "critical";
type Operator = "equals" | "contains" | "startswith" | "endswith" | "exists" | "gt" | "gte" | "lt" | "lte";
type RuleFormat = "basic" | "json" | "sigma";

interface BlueRule {
  id: number;
  name: string;
  description: string;
  field: string;
  operator: Operator;
  value: string;
  severity: Severity;
  enabled: boolean;
  definition?: unknown;
  created_at: string;
  updated_at: string;
}

interface RuleMatch {
  event_id: number;
  source?: string;
  event_type?: string;
  message?: string;
  occurred_at?: string;
}

interface RuleRunResult {
  matched_count: number;
  scanned_count: number;
  matches: RuleMatch[];
  created_alerts?: number;
}

interface RuleForm {
  name: string;
  description: string;
  format: RuleFormat;
  field: string;
  operator: Operator;
  value: string;
  severity: Severity;
  enabled: boolean;
  definitionText: string;
  sigmaText: string;
}

const EMPTY_FORM: RuleForm = {
  name: "",
  description: "",
  format: "basic",
  field: "event_type",
  operator: "equals",
  value: "",
  severity: "medium",
  enabled: true,
  definitionText: "",
  sigmaText: "",
};

const FIELD_SUGGESTIONS = ["event_type", "source", "message", "asset", "severity", "raw.user", "raw.src_ip"];
const OPERATORS: Operator[] = ["equals", "contains", "startswith", "endswith", "exists", "gt", "gte", "lt", "lte"];
const OPERATOR_LABELS: Record<string, Record<Operator, string>> = {
  en: {
    equals: "Equals",
    contains: "Contains",
    startswith: "Starts with",
    endswith: "Ends with",
    exists: "Exists",
    gt: "Greater than",
    gte: "Greater or equal",
    lt: "Less than",
    lte: "Less or equal",
  },
  ko: {
    equals: "같음",
    contains: "포함",
    startswith: "시작 문자열",
    endswith: "끝 문자열",
    exists: "존재함",
    gt: "초과",
    gte: "이상",
    lt: "미만",
    lte: "이하",
  },
  zh: {
    equals: "等于",
    contains: "包含",
    startswith: "以…开头",
    endswith: "以…结尾",
    exists: "存在",
    gt: "大于",
    gte: "大于或等于",
    lt: "小于",
    lte: "小于或等于",
  },
  es: {
    equals: "Igual a",
    contains: "Contiene",
    startswith: "Empieza por",
    endswith: "Termina en",
    exists: "Existe",
    gt: "Mayor que",
    gte: "Mayor o igual",
    lt: "Menor que",
    lte: "Menor o igual",
  },
};
const SEVERITIES: Severity[] = ["info", "low", "medium", "high", "critical"];
const PAGE_SIZE = 200;

const EN = {
  title: "Detection rules",
  subtitle:
    "Test rules against stored events before creating alerts. Rules analyze imported evidence; they do not block traffic.",
  newRule: "New rule",
  refresh: "Refresh",
  loadMore: "Load more rules",
  noRules: "No detection rules yet. Create one to turn imported events into reviewable alerts.",
  loadError: "Could not load detection rules",
  saveError: "Could not save rule",
  saved: "Rule saved",
  testError: "Rule test failed",
  runError: "Rule run failed",
  runDone: "Rule run completed",
  statusError: "Could not change rule status",
  name: "Name",
  description: "Description",
  field: "Event field",
  operator: "Comparison",
  value: "Value",
  severity: "Alert severity",
  state: "State",
  actions: "Actions",
  scanLimit: "Maximum events to scan",
  scanned: "Events scanned",
  enabled: "Enabled",
  disabled: "Disabled",
  edit: "Edit",
  test: "Test",
  run: "Run",
  testing: "Testing…",
  running: "Running…",
  cancel: "Cancel",
  save: "Save rule",
  create: "Create rule",
  editorTitle: "Detection rule",
  editorDescription: "Choose a field comparison, a structured definition, or a supported Sigma YAML rule.",
  format: "Rule format",
  basicFormat: "Single field",
  jsonFormat: "Advanced JSON",
  sigmaFormat: "Sigma YAML import",
  advanced: "Advanced detection definition (JSON)",
  advancedHint:
    "This definition is evaluated instead of the basic field comparison. It supports structured selections and conditions, not arbitrary code.",
  advancedExample:
    '{"detection":{"selection":{"event_type":"auth.failure","message|contains":"failed"},"condition":"selection"}}',
  invalidJSON: "Advanced definition must be a JSON object.",
  requiredJSON: "Enter a detection definition or choose single field.",
  sigma: "Sigma rule (YAML)",
  sigmaHint:
    "A supported subset of Sigma is converted to the structured definition when saved. Correlation, timeframe, and keywords-only rules are not supported.",
  sigmaExample:
    "title: Failed login\nlogsource:\n  category: authentication\ndetection:\n  selection:\n    event_type: auth.failure\n  condition: selection",
  requiredSigma: "Paste a Sigma YAML rule.",
  requiredName: "Enter a rule name.",
  requiredValue: "Enter a comparison value, or use the exists operator.",
  results: "Latest result",
  testResult: "Test only — no alerts were created.",
  runResult: "Run — new alerts created:",
  matches: "Matching events",
  noMatches: "No stored events matched this rule.",
  eventId: "Event ID",
  eventType: "Event type",
  source: "Source",
  message: "Message",
  sampleLimited: "Showing the returned sample; the match count may be higher.",
  pendingRun: "Run evaluates stored events and creates deduplicated alerts for matches.",
  genericError: "An unexpected error occurred.",
} as const;

type Copy = { [K in keyof typeof EN]: string };

const KO: Copy = {
  title: "탐지 규칙",
  subtitle: "저장된 이벤트에 규칙을 시험한 뒤 경보를 만듭니다. 이 규칙은 증거를 분석하며 트래픽을 차단하지 않습니다.",
  newRule: "새 규칙",
  refresh: "새로고침",
  loadMore: "규칙 더 보기",
  noRules: "탐지 규칙이 없습니다. 규칙을 만들어 가져온 이벤트에서 검토 가능한 경보를 생성하세요.",
  loadError: "탐지 규칙을 불러오지 못했습니다",
  saveError: "규칙을 저장하지 못했습니다",
  saved: "규칙을 저장했습니다",
  testError: "규칙 시험에 실패했습니다",
  runError: "규칙 실행에 실패했습니다",
  runDone: "규칙 실행을 마쳤습니다",
  statusError: "규칙 상태를 바꾸지 못했습니다",
  name: "이름",
  description: "설명",
  field: "이벤트 필드",
  operator: "비교 방식",
  value: "값",
  severity: "경보 심각도",
  state: "상태",
  actions: "작업",
  scanLimit: "최대 검사 이벤트 수",
  scanned: "검사한 이벤트",
  enabled: "사용 중",
  disabled: "사용 안 함",
  edit: "편집",
  test: "시험",
  run: "실행",
  testing: "시험 중…",
  running: "실행 중…",
  cancel: "취소",
  save: "규칙 저장",
  create: "규칙 만들기",
  editorTitle: "탐지 규칙",
  editorDescription: "단일 필드 비교, 구조화된 정의, 지원 범위의 Sigma YAML 중에서 선택하세요.",
  format: "규칙 형식",
  basicFormat: "단일 필드",
  jsonFormat: "고급 JSON",
  sigmaFormat: "Sigma YAML 가져오기",
  advanced: "고급 탐지 정의 (JSON)",
  advancedHint:
    "기본 필드 비교 대신 이 정의를 평가합니다. 구조화된 선택과 조건을 지원하며 임의 코드는 실행하지 않습니다.",
  advancedExample:
    '{"detection":{"selection":{"event_type":"auth.failure","message|contains":"failed"},"condition":"selection"}}',
  invalidJSON: "고급 정의는 JSON 객체여야 합니다.",
  requiredJSON: "탐지 정의를 입력하거나 단일 필드를 선택하세요.",
  sigma: "Sigma 규칙 (YAML)",
  sigmaHint:
    "저장할 때 지원되는 Sigma 일부 문법을 구조화된 정의로 변환합니다. correlation, timeframe, 키워드만 있는 규칙은 지원하지 않습니다.",
  sigmaExample:
    "title: Failed login\nlogsource:\n  category: authentication\ndetection:\n  selection:\n    event_type: auth.failure\n  condition: selection",
  requiredSigma: "Sigma YAML 규칙을 붙여넣으세요.",
  requiredName: "규칙 이름을 입력하세요.",
  requiredValue: "비교할 값을 입력하거나 'exists' 방식을 선택하세요.",
  results: "최근 결과",
  testResult: "시험만 수행했습니다. 경보는 생성하지 않았습니다.",
  runResult: "실행으로 새로 생성된 경보:",
  matches: "일치한 이벤트",
  noMatches: "저장된 이벤트 중 일치하는 항목이 없습니다.",
  eventId: "이벤트 ID",
  eventType: "이벤트 유형",
  source: "출처",
  message: "메시지",
  sampleLimited: "반환된 일부 이벤트를 표시합니다. 전체 일치 건수는 더 많을 수 있습니다.",
  pendingRun: "실행하면 저장된 이벤트를 평가하고 일치 건에 대해 중복을 제거한 경보를 생성합니다.",
  genericError: "예상치 못한 오류가 발생했습니다.",
};

const ZH: Copy = {
  title: "检测规则",
  subtitle: "先用已存储的事件测试规则，再创建告警。规则分析导入的证据，不会阻断流量。",
  newRule: "新建规则",
  refresh: "刷新",
  loadMore: "加载更多规则",
  noRules: "尚无检测规则。创建规则，将导入的事件转为可审查的告警。",
  loadError: "无法加载检测规则",
  saveError: "无法保存规则",
  saved: "规则已保存",
  testError: "规则测试失败",
  runError: "规则运行失败",
  runDone: "规则运行完成",
  statusError: "无法更改规则状态",
  name: "名称",
  description: "描述",
  field: "事件字段",
  operator: "比较方式",
  value: "值",
  severity: "告警严重程度",
  state: "状态",
  actions: "操作",
  scanLimit: "最多扫描事件数",
  scanned: "已扫描事件",
  enabled: "已启用",
  disabled: "已停用",
  edit: "编辑",
  test: "测试",
  run: "运行",
  testing: "测试中…",
  running: "运行中…",
  cancel: "取消",
  save: "保存规则",
  create: "创建规则",
  editorTitle: "检测规则",
  editorDescription: "选择单字段比较、结构化定义或受支持的 Sigma YAML 规则。",
  format: "规则格式",
  basicFormat: "单字段",
  jsonFormat: "高级 JSON",
  sigmaFormat: "导入 Sigma YAML",
  advanced: "高级检测定义 (JSON)",
  advancedHint: "此定义将代替基本字段比较进行评估。支持结构化选择和条件，不执行任意代码。",
  advancedExample:
    '{"detection":{"selection":{"event_type":"auth.failure","message|contains":"failed"},"condition":"selection"}}',
  invalidJSON: "高级定义必须是 JSON 对象。",
  requiredJSON: "请输入检测定义，或选择单字段格式。",
  sigma: "Sigma 规则 (YAML)",
  sigmaHint: "保存时会将受支持的 Sigma 语法转换为结构化定义。不支持关联规则、时间窗口及纯关键词规则。",
  sigmaExample:
    "title: Failed login\nlogsource:\n  category: authentication\ndetection:\n  selection:\n    event_type: auth.failure\n  condition: selection",
  requiredSigma: "请粘贴 Sigma YAML 规则。",
  requiredName: "请输入规则名称。",
  requiredValue: "请输入比较值，或使用 exists 运算符。",
  results: "最近结果",
  testResult: "仅测试，未创建告警。",
  runResult: "运行后新建的告警：",
  matches: "匹配事件",
  noMatches: "没有已存储的事件匹配此规则。",
  eventId: "事件 ID",
  eventType: "事件类型",
  source: "来源",
  message: "消息",
  sampleLimited: "仅显示返回的样本；匹配总数可能更多。",
  pendingRun: "运行规则会评估已存储的事件，并为匹配项创建去重告警。",
  genericError: "发生意外错误。",
};

const ES: Copy = {
  title: "Reglas de detección",
  subtitle:
    "Prueba las reglas con los eventos almacenados antes de crear alertas. Las reglas analizan la evidencia importada; no bloquean el tráfico.",
  newRule: "Nueva regla",
  refresh: "Actualizar",
  loadMore: "Cargar más reglas",
  noRules:
    "Todavía no hay reglas de detección. Crea una para generar alertas revisables a partir de los eventos importados.",
  loadError: "No se pudieron cargar las reglas de detección",
  saveError: "No se pudo guardar la regla",
  saved: "Regla guardada",
  testError: "Falló la prueba de la regla",
  runError: "Falló la ejecución de la regla",
  runDone: "Ejecución de la regla completada",
  statusError: "No se pudo cambiar el estado de la regla",
  name: "Nombre",
  description: "Descripción",
  field: "Campo del evento",
  operator: "Comparación",
  value: "Valor",
  severity: "Gravedad de la alerta",
  state: "Estado",
  actions: "Acciones",
  scanLimit: "Máximo de eventos a examinar",
  scanned: "Eventos examinados",
  enabled: "Activada",
  disabled: "Desactivada",
  edit: "Editar",
  test: "Probar",
  run: "Ejecutar",
  testing: "Probando…",
  running: "Ejecutando…",
  cancel: "Cancelar",
  save: "Guardar regla",
  create: "Crear regla",
  editorTitle: "Regla de detección",
  editorDescription: "Elige una comparación de campo, una definición estructurada o una regla Sigma YAML compatible.",
  format: "Formato de la regla",
  basicFormat: "Un campo",
  jsonFormat: "JSON avanzado",
  sigmaFormat: "Importar Sigma YAML",
  advanced: "Definición de detección avanzada (JSON)",
  advancedHint:
    "Esta definición se evalúa en lugar de la comparación básica. Admite selecciones y condiciones estructuradas, pero no código arbitrario.",
  advancedExample:
    '{"detection":{"selection":{"event_type":"auth.failure","message|contains":"failed"},"condition":"selection"}}',
  invalidJSON: "La definición avanzada debe ser un objeto JSON.",
  requiredJSON: "Introduce una definición de detección o elige un solo campo.",
  sigma: "Regla Sigma (YAML)",
  sigmaHint:
    "Al guardar, la sintaxis Sigma compatible se convierte en una definición estructurada. No se admiten reglas de correlación, intervalos de tiempo ni reglas solo de palabras clave.",
  sigmaExample:
    "title: Failed login\nlogsource:\n  category: authentication\ndetection:\n  selection:\n    event_type: auth.failure\n  condition: selection",
  requiredSigma: "Pega una regla Sigma YAML.",
  requiredName: "Introduce el nombre de la regla.",
  requiredValue: "Introduce un valor de comparación o usa el operador exists.",
  results: "Resultado más reciente",
  testResult: "Solo prueba: no se crearon alertas.",
  runResult: "Ejecución: nuevas alertas creadas:",
  matches: "Eventos coincidentes",
  noMatches: "Ningún evento almacenado coincide con esta regla.",
  eventId: "ID del evento",
  eventType: "Tipo de evento",
  source: "Origen",
  message: "Mensaje",
  sampleLimited: "Se muestra la muestra devuelta; el total de coincidencias puede ser mayor.",
  pendingRun: "La ejecución evalúa eventos almacenados y crea alertas sin duplicados para las coincidencias.",
  genericError: "Se produjo un error inesperado.",
};

const COPY_BY_LOCALE: Record<string, Copy> = { en: EN, ko: KO, zh: ZH, es: ES };

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error ? `${fallback}: ${error.message}` : fallback;
}

function formatDefinition(definition: unknown): string {
  if (definition == null) return "";
  if (typeof definition === "string") {
    try {
      return JSON.stringify(JSON.parse(definition), null, 2);
    } catch {
      return definition;
    }
  }
  if (typeof definition === "object" && Object.keys(definition).length === 0) return "";
  return JSON.stringify(definition, null, 2);
}

export default function BlueRulesPage() {
  const locale = useLocale();
  const t = COPY_BY_LOCALE[locale] ?? EN;
  const [rules, setRules] = React.useState<BlueRule[]>([]);
  const [loading, setLoading] = React.useState(true);
  const [loadingMore, setLoadingMore] = React.useState(false);
  const [hasMore, setHasMore] = React.useState(false);
  const nextOffset = React.useRef(0);
  const requestVersion = React.useRef(0);
  const [editorOpen, setEditorOpen] = React.useState(false);
  const [editingId, setEditingId] = React.useState<number | null>(null);
  const [form, setForm] = React.useState<RuleForm>(EMPTY_FORM);
  const [saving, setSaving] = React.useState(false);
  const [workingId, setWorkingId] = React.useState<number | null>(null);
  const [scanLimit, setScanLimit] = React.useState(5000);
  const [result, setResult] = React.useState<{ ruleId: number; mode: "test" | "run"; data: RuleRunResult } | null>(
    null,
  );

  const load = React.useCallback(async () => {
    const version = ++requestVersion.current;
    setLoading(true);
    setLoadingMore(false);
    try {
      const rows = await blueRequest<BlueRule[]>(`/rules?limit=${PAGE_SIZE}`);
      if (version !== requestVersion.current) return;
      const page = Array.isArray(rows) ? rows : [];
      setRules(page);
      nextOffset.current = page.length;
      setHasMore(page.length === PAGE_SIZE);
    } catch (error) {
      if (version === requestVersion.current) toast.error(errorMessage(error, t.loadError));
    } finally {
      if (version === requestVersion.current) setLoading(false);
    }
  }, [t.loadError]);

  async function loadMore() {
    const version = requestVersion.current;
    setLoadingMore(true);
    try {
      const rows = await blueRequest<BlueRule[]>(`/rules?limit=${PAGE_SIZE}&offset=${nextOffset.current}`);
      if (version !== requestVersion.current) return;
      const page = Array.isArray(rows) ? rows : [];
      nextOffset.current += page.length;
      setRules((current) => {
        const seen = new Set(current.map((rule) => rule.id));
        return [...current, ...page.filter((rule) => !seen.has(rule.id))];
      });
      setHasMore(page.length === PAGE_SIZE);
    } catch (error) {
      if (version === requestVersion.current) toast.error(errorMessage(error, t.loadError));
    } finally {
      if (version === requestVersion.current) setLoadingMore(false);
    }
  }

  React.useEffect(() => {
    void load();
  }, [load]);

  function openCreate() {
    setEditingId(null);
    setForm({ ...EMPTY_FORM });
    setEditorOpen(true);
  }

  function openEdit(rule: BlueRule) {
    setEditingId(rule.id);
    setForm({
      name: rule.name,
      description: rule.description ?? "",
      format: formatDefinition(rule.definition) ? "json" : "basic",
      field: rule.field || "event_type",
      operator: rule.operator || "equals",
      value: rule.value ?? "",
      severity: rule.severity,
      enabled: rule.enabled,
      definitionText: formatDefinition(rule.definition),
      sigmaText: "",
    });
    setEditorOpen(true);
  }

  async function save() {
    if (!form.name.trim()) {
      toast.error(t.requiredName);
      return;
    }
    let definition: Record<string, unknown> | null = null;
    if (form.format === "json") {
      if (!form.definitionText.trim()) {
        toast.error(t.requiredJSON);
        return;
      }
      try {
        const parsed: unknown = JSON.parse(form.definitionText);
        if (parsed === null || typeof parsed !== "object" || Array.isArray(parsed)) throw new Error(t.invalidJSON);
        definition = parsed as Record<string, unknown>;
      } catch {
        toast.error(t.invalidJSON);
        return;
      }
    } else if (form.format === "sigma" && !form.sigmaText.trim()) {
      toast.error(t.requiredSigma);
      return;
    } else if (form.format === "basic" && form.operator !== "exists" && !form.value.trim()) {
      toast.error(t.requiredValue);
      return;
    }
    const payload = {
      name: form.name.trim(),
      description: form.description.trim(),
      field: form.field.trim() || "event_type",
      operator: form.operator,
      value: form.value,
      severity: form.severity,
      enabled: form.enabled,
      ...(form.format === "sigma" ? { sigma_yaml: form.sigmaText.trim() } : { definition }),
    };
    setSaving(true);
    try {
      if (editingId === null) {
        await blueRequest<BlueRule>("/rules", { method: "POST", body: JSON.stringify(payload) });
      } else {
        await blueRequest<BlueRule>(`/rules/${editingId}`, { method: "PATCH", body: JSON.stringify(payload) });
      }
      toast.success(t.saved);
      setEditorOpen(false);
      await load();
    } catch (error) {
      toast.error(errorMessage(error, t.saveError));
    } finally {
      setSaving(false);
    }
  }

  async function changeEnabled(rule: BlueRule, enabled: boolean) {
    setWorkingId(rule.id);
    try {
      await blueRequest<BlueRule>(`/rules/${rule.id}`, {
        method: "PATCH",
        body: JSON.stringify({ enabled }),
      });
      setRules((current) => current.map((item) => (item.id === rule.id ? { ...item, enabled } : item)));
    } catch (error) {
      toast.error(errorMessage(error, t.statusError));
    } finally {
      setWorkingId(null);
    }
  }

  async function evaluate(rule: BlueRule, mode: "test" | "run") {
    setWorkingId(rule.id);
    try {
      const data = await blueRequest<RuleRunResult>(`/rules/${rule.id}/${mode}`, {
        method: "POST",
        body: JSON.stringify({ limit: scanLimit }),
      });
      setResult({ ruleId: rule.id, mode, data });
      if (mode === "run") toast.success(`${t.runDone}: ${data.created_alerts ?? 0}`);
    } catch (error) {
      toast.error(errorMessage(error, mode === "test" ? t.testError : t.runError));
    } finally {
      setWorkingId(null);
    }
  }

  return (
    <div className="space-y-6 p-4 md:p-6">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 className="flex items-center gap-2 text-2xl font-semibold tracking-tight">
            <ShieldCheck className="size-6 text-primary" /> {t.title}
          </h1>
          <p className="mt-1 max-w-3xl text-sm text-muted-foreground">{t.subtitle}</p>
        </div>
        <div className="flex flex-wrap items-end gap-2">
          <div className="space-y-1">
            <Label htmlFor="blue-rule-scan-limit" className="text-xs text-muted-foreground">
              {t.scanLimit}
            </Label>
            <Input
              id="blue-rule-scan-limit"
              className="w-28"
              type="number"
              min={1}
              max={50000}
              value={scanLimit}
              onChange={(event) => setScanLimit(Math.max(1, Math.min(50000, Number(event.target.value) || 1)))}
            />
          </div>
          <Button variant="outline" onClick={() => void load()} disabled={loading}>
            <RefreshCw className="size-4" /> {t.refresh}
          </Button>
          <Button onClick={openCreate}>
            <Plus className="size-4" /> {t.newRule}
          </Button>
        </div>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t.title}</CardTitle>
          <CardDescription>{t.pendingRun}</CardDescription>
        </CardHeader>
        <CardContent>
          {loading ? (
            <p className="py-8 text-center text-sm text-muted-foreground">{t.refresh}…</p>
          ) : rules.length === 0 ? (
            <p className="py-8 text-center text-sm text-muted-foreground">{t.noRules}</p>
          ) : (
            <div className="overflow-x-auto">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t.name}</TableHead>
                    <TableHead>{t.field}</TableHead>
                    <TableHead>{t.severity}</TableHead>
                    <TableHead>{t.state}</TableHead>
                    <TableHead className="text-right">{t.actions}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {rules.map((rule) => (
                    <TableRow key={rule.id}>
                      <TableCell className="min-w-52">
                        <div className="font-medium">{rule.name}</div>
                        {rule.description && (
                          <div className="max-w-96 truncate text-xs text-muted-foreground">{rule.description}</div>
                        )}
                      </TableCell>
                      <TableCell className="font-mono text-xs">
                        {rule.definition &&
                        typeof rule.definition === "object" &&
                        Object.keys(rule.definition).length > 0
                          ? "JSON"
                          : `${rule.field} ${OPERATOR_LABELS[locale]?.[rule.operator] ?? rule.operator} ${rule.value}`}
                      </TableCell>
                      <TableCell>
                        <Badge variant="outline">{blueSeverityLabel(rule.severity, locale)}</Badge>
                      </TableCell>
                      <TableCell>
                        <div className="flex items-center gap-2">
                          <Switch
                            checked={rule.enabled}
                            disabled={workingId === rule.id}
                            aria-label={`${rule.name}: ${t.state}`}
                            onCheckedChange={(enabled) => void changeEnabled(rule, enabled)}
                          />
                          <span className="text-xs text-muted-foreground">{rule.enabled ? t.enabled : t.disabled}</span>
                        </div>
                      </TableCell>
                      <TableCell>
                        <div className="flex flex-wrap justify-end gap-1.5">
                          <Button
                            size="sm"
                            variant="ghost"
                            onClick={() => openEdit(rule)}
                            disabled={workingId === rule.id}
                          >
                            {t.edit}
                          </Button>
                          <Button
                            size="sm"
                            variant="outline"
                            onClick={() => void evaluate(rule, "test")}
                            disabled={workingId === rule.id}
                          >
                            <FlaskConical className="size-3.5" /> {workingId === rule.id ? t.testing : t.test}
                          </Button>
                          <Button
                            size="sm"
                            onClick={() => void evaluate(rule, "run")}
                            disabled={workingId === rule.id || !rule.enabled}
                          >
                            <Play className="size-3.5" /> {workingId === rule.id ? t.running : t.run}
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
          {hasMore && !loading && (
            <div className="mt-4 text-center">
              <Button variant="outline" onClick={() => void loadMore()} disabled={loadingMore}>
                {t.loadMore}
                {loadingMore ? "…" : ""}
              </Button>
            </div>
          )}
        </CardContent>
      </Card>

      {result && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">
              {t.results}: {rules.find((rule) => rule.id === result.ruleId)?.name ?? `#${result.ruleId}`}
            </CardTitle>
            <CardDescription>
              {result.mode === "test" ? t.testResult : `${t.runResult} ${result.data.created_alerts ?? 0}`} {t.scanned}:{" "}
              {result.data.scanned_count}. {t.matches}: {result.data.matched_count}
            </CardDescription>
          </CardHeader>
          <CardContent>
            {result.data.matches.length === 0 ? (
              <p className="text-sm text-muted-foreground">{t.noMatches}</p>
            ) : (
              <div className="space-y-2">
                <div className="overflow-x-auto">
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>{t.eventId}</TableHead>
                        <TableHead>{t.source}</TableHead>
                        <TableHead>{t.eventType}</TableHead>
                        <TableHead>{t.message}</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {result.data.matches.map((match) => (
                        <TableRow key={match.event_id}>
                          <TableCell className="font-mono">#{match.event_id}</TableCell>
                          <TableCell>{match.source ?? "—"}</TableCell>
                          <TableCell className="font-mono text-xs">{match.event_type ?? "—"}</TableCell>
                          <TableCell className="max-w-xl truncate">{match.message ?? "—"}</TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </div>
                {result.data.matches.length < result.data.matched_count && (
                  <p className="text-xs text-muted-foreground">{t.sampleLimited}</p>
                )}
              </div>
            )}
          </CardContent>
        </Card>
      )}

      <Dialog open={editorOpen} onOpenChange={setEditorOpen}>
        <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>{t.editorTitle}</DialogTitle>
            <DialogDescription>{t.editorDescription}</DialogDescription>
          </DialogHeader>
          <div className="grid gap-4 py-2">
            <div className="grid gap-1.5">
              <Label htmlFor="blue-rule-name">{t.name}</Label>
              <Input
                id="blue-rule-name"
                value={form.name}
                onChange={(event) => setForm({ ...form, name: event.target.value })}
                maxLength={200}
              />
            </div>
            <div className="grid gap-1.5">
              <Label htmlFor="blue-rule-description">{t.description}</Label>
              <Textarea
                id="blue-rule-description"
                value={form.description}
                onChange={(event) => setForm({ ...form, description: event.target.value })}
                rows={2}
              />
            </div>
            <div className="grid gap-1.5">
              <Label htmlFor="blue-rule-format">{t.format}</Label>
              <Select value={form.format} onValueChange={(value) => setForm({ ...form, format: value as RuleFormat })}>
                <SelectTrigger id="blue-rule-format">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="basic">{t.basicFormat}</SelectItem>
                  <SelectItem value="json">{t.jsonFormat}</SelectItem>
                  <SelectItem value="sigma">{t.sigmaFormat}</SelectItem>
                </SelectContent>
              </Select>
            </div>
            {form.format === "basic" && (
              <div className="grid gap-3 sm:grid-cols-2">
                <div className="grid gap-1.5">
                  <Label htmlFor="blue-rule-field">{t.field}</Label>
                  <Input
                    id="blue-rule-field"
                    list="blue-rule-fields"
                    value={form.field}
                    onChange={(event) => setForm({ ...form, field: event.target.value })}
                  />
                  <datalist id="blue-rule-fields">
                    {FIELD_SUGGESTIONS.map((field) => (
                      <option key={field} value={field} />
                    ))}
                  </datalist>
                </div>
                <div className="grid gap-1.5">
                  <Label htmlFor="blue-rule-operator">{t.operator}</Label>
                  <Select
                    value={form.operator}
                    onValueChange={(value) => setForm({ ...form, operator: value as Operator })}
                  >
                    <SelectTrigger id="blue-rule-operator">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {OPERATORS.map((operator) => (
                        <SelectItem key={operator} value={operator}>
                          {OPERATOR_LABELS[locale]?.[operator] ?? operator}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              </div>
            )}
            <div className="grid gap-3 sm:grid-cols-2">
              {form.format === "basic" && (
                <div className="grid gap-1.5">
                  <Label htmlFor="blue-rule-value">{t.value}</Label>
                  <Input
                    id="blue-rule-value"
                    value={form.value}
                    disabled={form.operator === "exists"}
                    onChange={(event) => setForm({ ...form, value: event.target.value })}
                  />
                </div>
              )}
              <div className="grid gap-1.5">
                <Label htmlFor="blue-rule-severity">{t.severity}</Label>
                <Select
                  value={form.severity}
                  onValueChange={(value) => setForm({ ...form, severity: value as Severity })}
                >
                  <SelectTrigger id="blue-rule-severity">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {SEVERITIES.map((severity) => (
                      <SelectItem key={severity} value={severity}>
                        {blueSeverityLabel(severity, locale)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </div>
            {form.format === "json" && (
              <div className="grid gap-1.5">
                <Label htmlFor="blue-rule-definition">{t.advanced}</Label>
                <Textarea
                  id="blue-rule-definition"
                  className="font-mono text-xs"
                  rows={7}
                  placeholder={t.advancedExample}
                  value={form.definitionText}
                  onChange={(event) => setForm({ ...form, definitionText: event.target.value })}
                />
                <p className="text-xs text-muted-foreground">{t.advancedHint}</p>
              </div>
            )}
            {form.format === "sigma" && (
              <div className="grid gap-1.5">
                <Label htmlFor="blue-rule-sigma">{t.sigma}</Label>
                <Textarea
                  id="blue-rule-sigma"
                  className="font-mono text-xs"
                  rows={10}
                  placeholder={t.sigmaExample}
                  value={form.sigmaText}
                  onChange={(event) => setForm({ ...form, sigmaText: event.target.value })}
                />
                <p className="text-xs text-muted-foreground">{t.sigmaHint}</p>
              </div>
            )}
            <div className="flex items-center gap-2">
              <Switch
                id="blue-rule-enabled"
                checked={form.enabled}
                onCheckedChange={(enabled) => setForm({ ...form, enabled })}
              />
              <Label htmlFor="blue-rule-enabled">{t.enabled}</Label>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setEditorOpen(false)}>
              {t.cancel}
            </Button>
            <Button onClick={() => void save()} disabled={saving}>
              {saving ? `${t.save}…` : editingId === null ? t.create : t.save}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

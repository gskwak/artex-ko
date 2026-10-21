import type { BlueAction, BlueAlert, BlueAnalysisRun, BlueCase, BlueEvent, BlueObservation } from "@/lib/blue-api";
import type { BlueTask } from "@/lib/blue-task-api";

export interface ReportRows<T> {
  items: T[];
  truncated: boolean;
}

export interface BlueCaseReportSnapshot {
  caseRecord: BlueCase;
  capturedAt: string;
  events: ReportRows<BlueEvent>;
  alerts: ReportRows<BlueAlert>;
  observations: ReportRows<BlueObservation>;
  analyses: ReportRows<BlueAnalysisRun>;
  actions: ReportRows<BlueAction>;
}

type ReportCopy = {
  generated: string;
  scope: string;
  case: string;
  task: string;
  status: string;
  severity: string;
  priority: string;
  created: string;
  updated: string;
  completed: string;
  description: string;
  summary: string;
  result: string;
  caseEvidence: string;
  events: string;
  alerts: string;
  observations: string;
  analyses: string;
  actions: string;
  source: string;
  type: string;
  asset: string;
  occurred: string;
  message: string;
  rule: string;
  reason: string;
  author: string;
  detail: string;
  references: string;
  assessment: string;
  target: string;
  decision: string;
  completion: string;
  run: string;
  noRecords: string;
  noCase: string;
  unsaved: string;
  sourceNote: string;
  truncated: string;
  responseNote: string;
};

const en: ReportCopy = {
  generated: "Exported at",
  scope: "Scope",
  case: "Case",
  task: "Task",
  status: "Status",
  severity: "Severity",
  priority: "Priority",
  created: "Created",
  updated: "Updated",
  completed: "Completed",
  description: "Description",
  summary: "Summary",
  result: "Result and handoff notes",
  caseEvidence: "Linked case evidence",
  events: "Events",
  alerts: "Alerts",
  observations: "Observations",
  analyses: "Analysis runs",
  actions: "Response proposals and decisions",
  source: "Source",
  type: "Type",
  asset: "Asset",
  occurred: "Occurred",
  message: "Message",
  rule: "Rule",
  reason: "Reason",
  author: "Author",
  detail: "Detail",
  references: "Evidence references",
  assessment: "Assessment",
  target: "Target",
  decision: "Decision note",
  completion: "Completion evidence",
  run: "Analysis run",
  noRecords: "No saved records",
  noCase: "No case is linked to this task.",
  unsaved: "This report contains saved records only. Unsaved edits are excluded.",
  sourceNote:
    "Record IDs refer to ARTEX blue-team data. Original raw event payloads are available in the app and are not copied into this report. The data was retrieved in pages, so changes during export may affect the snapshot.",
  truncated:
    "The export limit was reached; additional records may exist. Review them in ARTEX before treating this report as complete.",
  responseNote:
    "ARTEX records proposed, approved, and operator-reported response actions. This report does not establish that ARTEX executed an external action.",
};

const ko: ReportCopy = {
  ...en,
  generated: "내보낸 시각",
  scope: "범위",
  case: "사건",
  task: "작업",
  status: "상태",
  severity: "심각도",
  priority: "우선순위",
  created: "생성",
  updated: "수정",
  completed: "완료",
  description: "설명",
  summary: "요약",
  result: "결과 및 인계 메모",
  caseEvidence: "연결된 사건의 증거",
  events: "이벤트",
  alerts: "경보",
  observations: "분석 기록",
  analyses: "분석 실행",
  actions: "대응 제안과 결정",
  source: "출처",
  type: "유형",
  asset: "자산",
  occurred: "발생",
  message: "내용",
  rule: "규칙",
  reason: "사유",
  author: "작성자",
  detail: "상세",
  references: "증거 참조",
  assessment: "평가",
  target: "대상",
  decision: "결정 메모",
  completion: "완료 증거",
  run: "분석 실행",
  noRecords: "저장된 기록 없음",
  noCase: "이 작업에 연결된 사건이 없습니다.",
  unsaved: "이 보고서에는 저장된 기록만 포함됩니다. 저장하지 않은 수정 내용은 제외됩니다.",
  sourceNote:
    "기록 ID는 ARTEX 블루팀 데이터를 가리킵니다. 이벤트 원본 데이터는 앱에서 확인할 수 있으며 이 보고서에는 복사하지 않습니다. 페이지별로 조회하므로 내보내는 동안 데이터가 바뀌면 결과에 영향을 줄 수 있습니다.",
  truncated:
    "내보내기 상한에 도달했습니다. 추가 기록이 있을 수 있으므로 보고서를 완전한 기록으로 취급하기 전에 ARTEX에서 확인하세요.",
  responseNote:
    "ARTEX는 대응 제안과 승인, 담당자가 보고한 완료 결과를 기록합니다. 이 보고서는 ARTEX가 외부 조치를 실행했다는 증거가 아닙니다.",
};

const zh: ReportCopy = {
  ...en,
  generated: "导出时间",
  scope: "范围",
  case: "案件",
  task: "任务",
  status: "状态",
  severity: "严重程度",
  priority: "优先级",
  created: "创建",
  updated: "更新",
  completed: "完成",
  description: "描述",
  summary: "摘要",
  result: "结果与交接记录",
  caseEvidence: "关联案件证据",
  events: "事件",
  alerts: "告警",
  observations: "分析记录",
  analyses: "分析运行",
  actions: "响应提案与决定",
  source: "来源",
  type: "类型",
  asset: "资产",
  occurred: "发生时间",
  message: "内容",
  rule: "规则",
  reason: "原因",
  author: "作者",
  detail: "详情",
  references: "证据引用",
  assessment: "评估",
  target: "目标",
  decision: "决定记录",
  completion: "完成证据",
  run: "分析运行",
  noRecords: "没有已保存记录",
  noCase: "此任务未关联案件。",
  unsaved: "本报告仅包含已保存记录，不包含未保存的修改。",
  sourceNote:
    "记录 ID 对应 ARTEX 蓝队数据。原始事件内容可在应用中查看，本报告未复制。导出使用分页读取，期间的数据变更可能影响快照。",
  truncated: "已达到导出上限，可能仍有其他记录。请在 ARTEX 中核对后再将本报告视为完整记录。",
  responseNote: "ARTEX 记录响应提案、批准和操作员报告的结果。本报告不能证明 ARTEX 执行了外部操作。",
};

const es: ReportCopy = {
  ...en,
  generated: "Exportado el",
  scope: "Alcance",
  case: "Caso",
  task: "Tarea",
  status: "Estado",
  severity: "Gravedad",
  priority: "Prioridad",
  created: "Creado",
  updated: "Actualizado",
  completed: "Completado",
  description: "Descripción",
  summary: "Resumen",
  result: "Resultado y notas de entrega",
  caseEvidence: "Pruebas del caso vinculado",
  events: "Eventos",
  alerts: "Alertas",
  observations: "Observaciones",
  analyses: "Ejecuciones de análisis",
  actions: "Propuestas y decisiones de respuesta",
  source: "Fuente",
  type: "Tipo",
  asset: "Activo",
  occurred: "Ocurrió",
  message: "Contenido",
  rule: "Regla",
  reason: "Motivo",
  author: "Autor",
  detail: "Detalle",
  references: "Referencias de pruebas",
  assessment: "Evaluación",
  target: "Objetivo",
  decision: "Nota de decisión",
  completion: "Prueba de finalización",
  run: "Ejecución de análisis",
  noRecords: "Sin registros guardados",
  noCase: "Esta tarea no tiene un caso vinculado.",
  unsaved: "Este informe solo incluye registros guardados. Se excluyen los cambios sin guardar.",
  sourceNote:
    "Los identificadores remiten a datos del equipo azul de ARTEX. Los eventos originales se pueden consultar en la aplicación y no se copian aquí. La exportación se lee por páginas; los cambios durante ese proceso pueden afectar el resultado.",
  truncated:
    "Se alcanzó el límite de exportación y puede haber más registros. Revísalos en ARTEX antes de considerar completo este informe.",
  responseNote:
    "ARTEX registra propuestas, aprobaciones y resultados comunicados por operadores. Este informe no demuestra que ARTEX haya ejecutado una acción externa.",
};

const copies: Record<string, ReportCopy> = { en, ko, zh, es };

function copyFor(locale: string): ReportCopy {
  return copies[locale] ?? en;
}

function plain(value: unknown): string {
  const text = String(value ?? "")
    .replace(/\s+/g, " ")
    .trim();
  return text ? text.replace(/[\\`*_{}[\]|<>]/g, "\\$&") : "—";
}

function quoted(value: unknown): string {
  const text = String(value ?? "").trim();
  if (!text) return "> —";
  return text
    .replace(/\r\n?/g, "\n")
    .split("\n")
    .map((line) => `> ${plain(line)}`)
    .join("\n");
}

function line(label: string, value: unknown): string {
  return `- **${label}:** ${plain(value)}`;
}

function section(level: number, title: string): string {
  return `${"#".repeat(level)} ${title}`;
}

function countTitle(title: string, rows: ReportRows<unknown>): string {
  return `${title} (${rows.items.length}${rows.truncated ? "+" : ""})`;
}

function referenceList(observation: BlueObservation): string {
  const eventIDs = new Set([
    ...(observation.event_id ? [observation.event_id] : []),
    ...(observation.evidence_event_ids ?? []),
  ]);
  const alertIDs = new Set([
    ...(observation.alert_id ? [observation.alert_id] : []),
    ...(observation.evidence_alert_ids ?? []),
  ]);
  const refs = [...eventIDs].map((id) => `event #${id}`).concat([...alertIDs].map((id) => `alert #${id}`));
  return refs.join(", ") || "—";
}

function appendRows<T>(
  lines: string[],
  rows: ReportRows<T>,
  heading: string,
  level: number,
  render: (item: T) => string[],
  copy: ReportCopy,
): void {
  lines.push(section(level, countTitle(heading, rows)), "");
  if (rows.items.length === 0) lines.push(copy.noRecords, "");
  for (const item of rows.items) lines.push(...render(item), "");
  if (rows.truncated) lines.push(`**${copy.truncated}**`, "");
}

function caseSections(snapshot: BlueCaseReportSnapshot, copy: ReportCopy, level: number): string[] {
  const item = snapshot.caseRecord;
  const lines = [
    section(level, `${copy.case} #${item.id}: ${plain(item.title)}`),
    "",
    line(copy.generated, snapshot.capturedAt),
    line(copy.status, item.status),
    line(copy.severity, item.severity),
    line(copy.created, item.created_at),
    line(copy.updated, item.updated_at),
    "",
    `**${copy.description}**`,
    quoted(item.description),
    "",
    `**${copy.summary}**`,
    quoted(item.summary),
    "",
    `**${copy.scope}:** ${copy.sourceNote}`,
    "",
  ];

  appendRows(
    lines,
    snapshot.events,
    copy.events,
    level + 1,
    (event) => [
      section(level + 2, `${copy.events} #${event.id}`),
      line(copy.occurred, event.occurred_at),
      line(copy.source, event.source),
      line(copy.type, event.event_type),
      line(copy.severity, event.severity),
      line(copy.asset, event.asset),
      `**${copy.message}**`,
      quoted(event.message),
    ],
    copy,
  );
  appendRows(
    lines,
    snapshot.alerts,
    copy.alerts,
    level + 1,
    (alert) => [
      section(level + 2, `${copy.alerts} #${alert.id}: ${plain(alert.title)}`),
      line(copy.status, alert.status),
      line(copy.severity, alert.severity),
      line(copy.rule, `#${alert.rule_id}`),
      line(copy.references, `event #${alert.event_id}`),
      `**${copy.reason}**`,
      quoted(alert.reason),
    ],
    copy,
  );
  appendRows(
    lines,
    snapshot.observations,
    copy.observations,
    level + 1,
    (observation) => [
      section(level + 2, `${copy.observations} #${observation.id} (${plain(observation.kind)})`),
      line(copy.created, observation.created_at),
      line(copy.author, observation.author),
      line(copy.references, referenceList(observation)),
      ...(observation.assessment
        ? [
            line(
              copy.assessment,
              [observation.assessment.verdict, observation.assessment.confidence, observation.assessment.severity]
                .filter(Boolean)
                .join(" / "),
            ),
          ]
        : []),
      `**${copy.summary}**`,
      quoted(observation.summary),
      `**${copy.detail}**`,
      quoted(observation.detail),
    ],
    copy,
  );
  appendRows(
    lines,
    snapshot.analyses,
    copy.analyses,
    level + 1,
    (run) => [
      section(level + 2, `${copy.run} #${run.id}`),
      line(copy.status, run.status),
      line(copy.created, run.created_at),
      line(copy.completed, run.finished_at),
      line(copy.references, run.observation_id ? `observation #${run.observation_id}` : "—"),
      ...(run.error ? [line(copy.reason, run.error)] : []),
    ],
    copy,
  );
  appendRows(
    lines,
    snapshot.actions,
    copy.actions,
    level + 1,
    (action) => [
      section(level + 2, `${copy.actions} #${action.id}`),
      line(copy.type, action.kind),
      line(copy.status, action.status),
      line(copy.target, action.target),
      line(copy.created, action.created_at),
      line(copy.author, action.requested_by),
      `**${copy.reason}**`,
      quoted(action.rationale),
      `**${copy.decision}**`,
      quoted(action.decision_note),
      `**${copy.completion}**`,
      quoted(action.completion_evidence),
    ],
    copy,
  );
  lines.push(copy.responseNote, "");
  return lines;
}

export function formatBlueCaseReport(snapshot: BlueCaseReportSnapshot, locale: string): string {
  const copy = copyFor(locale);
  return [...caseSections(snapshot, copy, 1), copy.unsaved, ""].join("\n");
}

export function formatBlueTaskReport(
  task: BlueTask,
  caseSnapshot: BlueCaseReportSnapshot | null,
  capturedAt: string,
  locale: string,
): string {
  const copy = copyFor(locale);
  const lines = [
    section(1, `${copy.task} #${task.id}: ${plain(task.title)}`),
    "",
    line(copy.generated, capturedAt),
    line(copy.status, task.status),
    line(copy.priority, task.priority),
    line(copy.created, task.created_at),
    line(copy.updated, task.updated_at),
    line(copy.completed, task.completed_at),
    line(copy.case, task.case_id ? `#${task.case_id}` : "—"),
    line(copy.run, task.analysis_run_id ? `#${task.analysis_run_id}` : "—"),
    "",
    `**${copy.description}**`,
    quoted(task.description),
    "",
    `**${copy.result}**`,
    quoted(task.result),
    "",
  ];
  if (caseSnapshot) {
    lines.push(section(2, copy.caseEvidence), "", ...caseSections(caseSnapshot, copy, 3));
  } else {
    lines.push(copy.noCase, "");
  }
  lines.push(copy.unsaved, "");
  return lines.join("\n");
}

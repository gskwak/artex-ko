import {
  type BlueAction,
  type BlueAlert,
  type BlueAnalysisRun,
  type BlueEvent,
  type BlueObservation,
  blueApi,
  blueRequest,
} from "@/lib/blue-api";
import {
  type BlueCaseReportSnapshot,
  formatBlueCaseReport,
  formatBlueTaskReport,
  type ReportRows,
} from "@/lib/blue-report-format";
import { blueTaskApi } from "@/lib/blue-task-api";

const PAGE_SIZE = 1000;
const MAX_ROWS_PER_SECTION = 10_000;

async function collectRows<T>(page: (limit: number, offset: number) => Promise<T[]>): Promise<ReportRows<T>> {
  const items: T[] = [];
  while (items.length < MAX_ROWS_PER_SECTION) {
    const limit = Math.min(PAGE_SIZE, MAX_ROWS_PER_SECTION - items.length);
    const next = await page(limit, items.length);
    items.push(...next);
    if (next.length < limit) return { items, truncated: false };
  }
  const beyondLimit = await page(1, MAX_ROWS_PER_SECTION);
  return { items, truncated: beyondLimit.length > 0 };
}

export async function captureBlueCaseReport(caseID: number): Promise<BlueCaseReportSnapshot> {
  const caseRecord = await blueApi.getCase(caseID);
  const capturedAt = new Date().toISOString();
  const [events, alerts, observations, analyses, actions] = await Promise.all([
    collectRows<BlueEvent>((limit, offset) => blueApi.events({ case_id: caseID, limit, offset })),
    collectRows<BlueAlert>((limit, offset) => blueApi.alerts({ case_id: caseID, limit, offset })),
    collectRows<BlueObservation>((limit, offset) => blueApi.observations(caseID, { limit, offset })),
    collectRows<BlueAnalysisRun>(async (limit, offset) => {
      const result = await blueRequest<{ runs: BlueAnalysisRun[] }>(
        `/cases/${caseID}/analyses?limit=${limit}&offset=${offset}`,
      );
      return result.runs ?? [];
    }),
    collectRows<BlueAction>((limit, offset) =>
      blueRequest<BlueAction[]>(`/cases/${caseID}/actions?limit=${limit}&offset=${offset}`),
    ),
  ]);
  return { caseRecord, capturedAt, events, alerts, observations, analyses, actions };
}

function downloadMarkdown(filename: string, body: string): void {
  const url = URL.createObjectURL(new Blob([body], { type: "text/markdown;charset=utf-8" }));
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = filename;
  document.body.append(anchor);
  anchor.click();
  anchor.remove();
  window.setTimeout(() => URL.revokeObjectURL(url), 60_000);
}

function reportDate(): string {
  return new Date().toISOString().slice(0, 10);
}

export async function downloadBlueCaseReport(caseID: number, locale: string): Promise<void> {
  const snapshot = await captureBlueCaseReport(caseID);
  downloadMarkdown(`artex-blue-case-${caseID}-${reportDate()}.md`, formatBlueCaseReport(snapshot, locale));
}

export async function downloadBlueTaskReport(taskID: number, locale: string): Promise<void> {
  const task = await blueTaskApi.get(taskID);
  const caseSnapshot = task.case_id ? await captureBlueCaseReport(task.case_id) : null;
  const capturedAt = new Date().toISOString();
  downloadMarkdown(
    `artex-blue-task-${taskID}-${reportDate()}.md`,
    formatBlueTaskReport(task, caseSnapshot, capturedAt, locale),
  );
}

export function blueReportButtonCopy(locale: string): { download: string; downloading: string; failed: string } {
  switch (locale) {
    case "ko":
      return { download: "보고서 다운로드", downloading: "보고서 생성 중…", failed: "보고서를 만들지 못했습니다." };
    case "zh":
      return { download: "下载报告", downloading: "正在生成报告…", failed: "无法生成报告。" };
    case "es":
      return {
        download: "Descargar informe",
        downloading: "Generando informe…",
        failed: "No se pudo generar el informe.",
      };
    default:
      return {
        download: "Download report",
        downloading: "Generating report…",
        failed: "Could not generate the report.",
      };
  }
}

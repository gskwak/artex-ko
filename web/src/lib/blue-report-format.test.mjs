import { formatBlueCaseReport, formatBlueTaskReport } from "./blue-report-format.ts";
import assert from "node:assert/strict";
import test from "node:test";

const caseRecord = {
  id: 7,
  title: "조사 대상",
  description: "동일 자산의 요청을 검토",
  status: "investigating",
  severity: "high",
  summary: "로그 기준 판단 보류",
  created_at: "2026-10-21T00:00:00Z",
  updated_at: "2026-10-21T01:00:00Z",
};

const snapshot = {
  caseRecord,
  capturedAt: "2026-10-21T02:00:00Z",
  events: {
    items: [
      {
        id: 11,
        source: "lab-access-log",
        event_type: "http_request",
        severity: "medium",
        asset: "lab-target",
        message: "GET /login returned 401",
        occurred_at: "2026-10-21T00:15:00Z",
        raw: { secret: "raw-only-value" },
      },
    ],
    truncated: false,
  },
  alerts: {
    items: [
      {
        id: 13,
        rule_id: 3,
        event_id: 11,
        title: "반복 로그인",
        severity: "medium",
        status: "investigating",
        reason: "반복 요청",
      },
    ],
    truncated: false,
  },
  observations: {
    items: [
      {
        id: 17,
        kind: "analysis",
        author: "blue_analyst",
        created_at: "2026-10-21T01:00:00Z",
        evidence_event_ids: [11],
        evidence_alert_ids: [13],
        summary: "추가 검증 필요",
        detail: "이벤트 11과 경보 13만 확인함",
        assessment: { verdict: "inconclusive", confidence: "low", severity: "unknown" },
      },
    ],
    truncated: false,
  },
  analyses: { items: [{ id: 19, status: "completed", observation_id: 17 }], truncated: false },
  actions: {
    items: [
      {
        id: 23,
        kind: "block_ip",
        target: "192.0.2.10",
        status: "approved",
        rationale: "검토 제안",
        decision_note: "승인만 기록",
        completion_evidence: "",
      },
    ],
    truncated: false,
  },
};

test("case report cites stored evidence and distinguishes an approved proposal from execution", () => {
  const report = formatBlueCaseReport(snapshot, "ko");
  assert.match(report, /사건 #7/);
  assert.match(report, /이벤트 #11/);
  assert.match(report, /경보 #13/);
  assert.match(report, /event #11, alert #13/);
  assert.match(report, /분석 실행 #19/);
  assert.match(report, /승인만 기록/);
  assert.match(report, /외부 조치를 실행했다는 증거가 아닙니다/);
  assert.doesNotMatch(report, /raw-only-value/);
});

test("report discloses truncated evidence instead of implying completeness", () => {
  const report = formatBlueCaseReport({ ...snapshot, events: { ...snapshot.events, truncated: true } }, "en");
  assert.match(report, /Events \(1\+\)/);
  assert.match(report, /additional records may exist/);
});

test("task report includes linked case evidence but labels saved, editable result", () => {
  const task = {
    id: 29,
    title: "Review HTTP evidence",
    description: "Check if this is malicious",
    result: "Needs owner approval",
    status: "completed",
    priority: "high",
    case_id: 7,
    analysis_run_id: 19,
    created_at: "2026-10-21T00:00:00Z",
    updated_at: "2026-10-21T02:00:00Z",
  };
  const report = formatBlueTaskReport(task, snapshot, "2026-10-21T03:00:00Z", "en");
  assert.match(report, /Task #29/);
  assert.match(report, /Result and handoff notes/);
  assert.match(report, /Needs owner approval/);
  assert.match(report, /Linked case evidence/);
  assert.match(report, /Case #7/);
  assert.match(report, /Unsaved edits are excluded/);
});

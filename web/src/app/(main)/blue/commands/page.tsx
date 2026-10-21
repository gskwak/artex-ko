"use client";

import Link from "next/link";

import { ArrowUpRight, ShieldCheck } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { useActiveLocale } from "@/i18n/locale-store";

import { BluePageHeader } from "../_components/blue-ui";

const copy = {
  en: {
    title: "Blue Team tool executions",
    description: "Blue Team investigations do not execute shell or network tools.",
    detail:
      "Blue Team chat and case analysis read stored evidence and propose findings. They cannot run commands or perform response actions. Review analysis results in the linked task or case; approved response actions are recorded in Investigation.",
    tasks: "View analysis tasks",
    cases: "View cases",
  },
  ko: {
    title: "블루팀 도구 실행",
    description: "블루팀 조사는 셸 명령이나 네트워크 도구를 실행하지 않습니다.",
    detail:
      "블루팀 대화와 사건 분석은 저장된 증거를 읽고 판단을 제안합니다. 명령을 실행하거나 대응 조치를 직접 수행하지 않습니다. 분석 결과는 연결된 작업 또는 사건에서, 승인된 대응 조치의 기록은 조사에서 확인하세요.",
    tasks: "분석 작업 보기",
    cases: "사건 보기",
  },
  zh: {
    title: "蓝队工具执行",
    description: "蓝队调查不会执行 Shell 命令或网络工具。",
    detail:
      "蓝队对话和案件分析仅读取已保存的证据并提出判断，无法运行命令或执行响应措施。分析结果可在关联任务或案件中查看；已批准的响应措施记录可在调查中查看。",
    tasks: "查看分析任务",
    cases: "查看案件",
  },
  es: {
    title: "Ejecuciones de herramientas del equipo azul",
    description: "Las investigaciones del equipo azul no ejecutan comandos ni herramientas de red.",
    detail:
      "El chat y el análisis de casos leen pruebas guardadas y proponen conclusiones. No pueden ejecutar comandos ni llevar a cabo acciones de respuesta. Consulte los resultados en la tarea o el caso vinculado y las acciones aprobadas en Investigación.",
    tasks: "Ver tareas de análisis",
    cases: "Ver casos",
  },
} as const;

export default function BlueCommandsPage() {
  const locale = useActiveLocale();
  const t = copy[locale] ?? copy.en;

  return (
    <div className="space-y-5 pb-6">
      <BluePageHeader title={t.title} description={t.description} />
      <Card>
        <CardContent className="flex flex-col gap-5 p-6">
          <ShieldCheck className="size-8 text-primary" aria-hidden="true" />
          <p className="max-w-2xl text-muted-foreground text-sm leading-6">{t.detail}</p>
          <div className="flex flex-wrap gap-2">
            <Button asChild size="sm">
              <Link href="/blue/tasks">
                {t.tasks}
                <ArrowUpRight aria-hidden="true" />
              </Link>
            </Button>
            <Button asChild size="sm" variant="outline">
              <Link href="/blue/cases">
                {t.cases}
                <ArrowUpRight aria-hidden="true" />
              </Link>
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}

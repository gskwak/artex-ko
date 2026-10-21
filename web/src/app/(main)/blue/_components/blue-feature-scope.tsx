"use client";

import Link from "next/link";

import { ArrowUpRight, ShieldCheck } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { useActiveLocale } from "@/i18n/locale-store";

import { BluePageHeader } from "./blue-ui";

type Feature = "assets" | "sync" | "workspace";

const copy = {
  en: {
    assets: {
      title: "Blue Team assets",
      description: "The Red Team asset inventory is separate from Blue Team evidence.",
      detail:
        "Register defensive assets in the Blue Team inventory, then link them to cases, events, and response decisions. Original event asset text remains part of the evidence.",
      link: "View assets",
      href: "/blue/assets",
    },
    sync: {
      title: "Blue Team asset sync",
      description: "Red Team asset synchronization does not import Blue Team telemetry.",
      detail:
        "Import defensive logs or configure a trusted HTTP JSONL source on the Events page. Network records appear in Traffic. Red Team asset synchronization does not populate the Blue Team inventory.",
      link: "Import events",
      href: "/blue/events",
    },
    workspace: {
      title: "Blue Team workspace",
      description: "Red Team workspace files are separate from Blue Team investigations.",
      detail:
        "Use Cases to organize evidence and Tasks to track defensive analysis. A separate Blue Team file workspace is not yet available.",
      link: "View cases",
      href: "/blue/cases",
    },
  },
  ko: {
    assets: {
      title: "블루팀 자산",
      description: "레드팀 자산 목록은 블루팀 조사 증거와 별개입니다.",
      detail:
        "블루팀 전용 목록에 방어 자산을 등록하고 사건·이벤트·대응 결정에 연결하세요. 이벤트의 원래 자산 문자열은 증거로 보존됩니다.",
      link: "자산 보기",
      href: "/blue/assets",
    },
    sync: {
      title: "블루팀 자산 동기화",
      description: "레드팀 자산 동기화는 블루팀 로그를 가져오지 않습니다.",
      detail:
        "이벤트 화면에서 방어 로그를 가져오거나 신뢰할 수 있는 HTTP JSONL 자동 수집 소스를 등록하세요. 네트워크 기록은 트래픽에 표시됩니다. 레드팀 자산 동기화는 블루팀 자산 목록을 채우지 않습니다.",
      link: "이벤트 가져오기",
      href: "/blue/events",
    },
    workspace: {
      title: "블루팀 작업 공간",
      description: "레드팀 작업 공간 파일은 블루팀 조사와 별개입니다.",
      detail:
        "사건에서 증거를 정리하고 작업에서 방어 분석을 추적하세요. 블루팀 전용 파일 작업 공간은 아직 제공되지 않습니다.",
      link: "사건 보기",
      href: "/blue/cases",
    },
  },
  zh: {
    assets: {
      title: "蓝队资产",
      description: "红队资产清单与蓝队调查证据分开。",
      detail: "在蓝队资产清单中登记防御资产，再将其关联到案件、事件和响应决策。事件中的原始资产文本会作为证据保留。",
      link: "查看资产",
      href: "/blue/assets",
    },
    sync: {
      title: "蓝队资产同步",
      description: "红队资产同步不会导入蓝队遥测数据。",
      detail:
        "请在事件页面导入防御日志，或配置可信的 HTTP JSONL 自动采集源。网络记录会显示在流量页面。红队资产同步不会填充蓝队资产清单。",
      link: "导入事件",
      href: "/blue/events",
    },
    workspace: {
      title: "蓝队工作区",
      description: "红队工作区文件与蓝队调查分开。",
      detail: "请在案件中整理证据，在任务中跟踪防御分析。蓝队独立文件工作区尚不可用。",
      link: "查看案件",
      href: "/blue/cases",
    },
  },
  es: {
    assets: {
      title: "Activos del equipo azul",
      description: "El inventario del equipo rojo está separado de las pruebas del equipo azul.",
      detail:
        "Registre activos defensivos en el inventario azul y vincúlelos con casos, eventos y decisiones de respuesta. El texto original del activo del evento se conserva como prueba.",
      link: "Ver activos",
      href: "/blue/assets",
    },
    sync: {
      title: "Sincronización de activos del equipo azul",
      description: "La sincronización del equipo rojo no importa la telemetría del equipo azul.",
      detail:
        "Importe registros defensivos o configure una fuente HTTP JSONL de confianza en Eventos. Los registros de red aparecen en Tráfico. La sincronización roja no rellena el inventario azul.",
      link: "Importar eventos",
      href: "/blue/events",
    },
    workspace: {
      title: "Espacio de trabajo del equipo azul",
      description: "Los archivos del equipo rojo están separados de las investigaciones del equipo azul.",
      detail:
        "Organice las pruebas en Casos y siga el análisis defensivo en Tareas. Aún no hay un espacio de archivos independiente para el equipo azul.",
      link: "Ver casos",
      href: "/blue/cases",
    },
  },
} as const;

export function BlueFeatureScope({ feature }: { feature: Feature }) {
  const locale = useActiveLocale();
  const t = (copy[locale] ?? copy.en)[feature];

  return (
    <div className="space-y-5 pb-6">
      <BluePageHeader title={t.title} description={t.description} />
      <Card>
        <CardContent className="flex flex-col gap-5 p-6">
          <ShieldCheck className="size-8 text-primary" aria-hidden="true" />
          <p className="max-w-2xl text-muted-foreground text-sm leading-6">{t.detail}</p>
          <Button asChild size="sm" className="w-fit">
            <Link href={t.href}>
              {t.link}
              <ArrowUpRight aria-hidden="true" />
            </Link>
          </Button>
        </CardContent>
      </Card>
    </div>
  );
}

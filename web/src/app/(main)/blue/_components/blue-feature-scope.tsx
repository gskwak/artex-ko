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
        "Imported Blue Team events include asset names and network addresses. Investigate those records in Events and Traffic. A separate Blue Team asset inventory is not yet available.",
      link: "View events",
      href: "/blue/events",
    },
    sync: {
      title: "Blue Team asset sync",
      description: "Red Team asset synchronization does not import Blue Team telemetry.",
      detail:
        "Import defensive logs from the Events page. Network records then appear in Traffic. Automated sensor or asset synchronization is not yet available for Blue Team.",
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
        "가져온 블루팀 이벤트에는 자산명과 네트워크 주소가 포함될 수 있습니다. 이벤트와 트래픽에서 조사하세요. 블루팀 전용 자산 목록은 아직 제공되지 않습니다.",
      link: "이벤트 보기",
      href: "/blue/events",
    },
    sync: {
      title: "블루팀 자산 동기화",
      description: "레드팀 자산 동기화는 블루팀 로그를 가져오지 않습니다.",
      detail:
        "방어 로그는 이벤트 화면에서 가져올 수 있습니다. 네트워크 기록은 트래픽에서 확인하세요. 블루팀의 자동 센서·자산 동기화는 아직 제공되지 않습니다.",
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
      detail: "导入的蓝队事件可包含资产名称和网络地址。请在事件和流量中调查这些记录。蓝队独立资产清单尚不可用。",
      link: "查看事件",
      href: "/blue/events",
    },
    sync: {
      title: "蓝队资产同步",
      description: "红队资产同步不会导入蓝队遥测数据。",
      detail: "请从事件页面导入防御日志，然后在流量页面查看网络记录。蓝队自动传感器或资产同步尚不可用。",
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
        "Los eventos importados pueden incluir nombres de activos y direcciones de red. Investíguelos en Eventos y Tráfico. Aún no hay un inventario independiente para el equipo azul.",
      link: "Ver eventos",
      href: "/blue/events",
    },
    sync: {
      title: "Sincronización de activos del equipo azul",
      description: "La sincronización del equipo rojo no importa la telemetría del equipo azul.",
      detail:
        "Importe registros defensivos desde Eventos y consulte los registros de red en Tráfico. Aún no hay sincronización automática de sensores o activos para el equipo azul.",
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

"use client";

import * as React from "react";

import { useRouter, useSearchParams } from "next/navigation";

import { useTranslations } from "next-intl";

import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";

import { BluePageHeader } from "../_components/blue-ui";
import { BlueInvestigationTabs } from "../_components/investigation-tabs";
import BlueAlertsView from "../alerts/alerts-view";
import BlueRulesView from "../rules/rules-view";

type DetectionTab = "alerts" | "rules";

function DetectionTabs() {
  const t = useTranslations("nav.item");
  const router = useRouter();
  const searchParams = useSearchParams();
  const tab: DetectionTab = searchParams.get("tab") === "rules" ? "rules" : "alerts";

  const selectTab = (value: string) => {
    if ((value !== "alerts" && value !== "rules") || value === tab) return;
    const params = new URLSearchParams(searchParams.toString());
    params.set("tab", value);
    router.replace(`/blue/detection?${params.toString()}`, { scroll: false });
  };

  return (
    <Tabs value={tab} onValueChange={selectTab}>
      <TabsList variant="line" aria-label={t("blueDetection")}>
        <TabsTrigger value="alerts">{t("blueAlerts")}</TabsTrigger>
        <TabsTrigger value="rules">{t("blueRules")}</TabsTrigger>
      </TabsList>
      <TabsContent value="alerts" className="mt-4">
        <BlueAlertsView embedded />
      </TabsContent>
      <TabsContent value="rules" className="mt-4">
        <BlueRulesView embedded />
      </TabsContent>
    </Tabs>
  );
}

export default function BlueDetectionPage() {
  const t = useTranslations("nav.item");

  return (
    <div className="space-y-5">
      <BluePageHeader title={t("blueDetection")} />
      <BlueInvestigationTabs active="detection" />
      <React.Suspense fallback={null}>
        <DetectionTabs />
      </React.Suspense>
    </div>
  );
}

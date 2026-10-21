"use client";

import * as React from "react";

import { useTranslations } from "next-intl";

import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";

import { BluePageHeader } from "../_components/blue-ui";
import BlueAlertsView from "../alerts/alerts-view";
import BlueRulesView from "../rules/rules-view";

type DetectionTab = "alerts" | "rules";

function tabFromUrl(): DetectionTab {
  return new URLSearchParams(window.location.search).get("tab") === "rules" ? "rules" : "alerts";
}

export default function BlueDetectionPage() {
  const t = useTranslations("nav.item");
  const [tab, setTab] = React.useState<DetectionTab>("alerts");

  React.useEffect(() => {
    setTab(tabFromUrl());
    const onPopState = () => setTab(tabFromUrl());
    window.addEventListener("popstate", onPopState);
    return () => window.removeEventListener("popstate", onPopState);
  }, []);

  const selectTab = (value: string) => {
    if (value !== "alerts" && value !== "rules") return;
    setTab(value);
    const url = new URL(window.location.href);
    url.searchParams.set("tab", value);
    window.history.replaceState(window.history.state, "", url);
  };

  return (
    <div className="space-y-5">
      <BluePageHeader title={t("blueDetection")} />
      <Tabs value={tab} onValueChange={selectTab}>
        <TabsList aria-label={t("blueDetection")}>
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
    </div>
  );
}

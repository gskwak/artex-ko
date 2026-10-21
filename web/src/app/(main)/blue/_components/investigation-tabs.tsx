"use client";

import { useRouter } from "next/navigation";

import { useTranslations } from "next-intl";

import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";

type InvestigationSection = "cases" | "events" | "traffic" | "detection" | "actions";

const sections = [
  { id: "cases", label: "blueCases", href: "/blue/cases" },
  { id: "events", label: "blueEvents", href: "/blue/events" },
  { id: "traffic", label: "blueTraffic", href: "/blue/traffic" },
  { id: "detection", label: "blueDetection", href: "/blue/detection" },
  { id: "actions", label: "blueActions", href: "/blue/actions" },
] as const;

export function BlueInvestigationTabs({ active }: { active: InvestigationSection }) {
  const t = useTranslations("nav.item");
  const router = useRouter();
  const label = t.has("blueInvestigation") ? t("blueInvestigation") : t("blueCases");

  const selectSection = (value: string) => {
    const section = sections.find((item) => item.id === value);
    if (section && section.id !== active) router.push(section.href);
  };

  return (
    <nav aria-label={label} className="shrink-0">
      <Tabs value={active} onValueChange={selectSection} className="hidden lg:flex">
        <TabsList aria-label={label}>
          {sections.map((section) => (
            <TabsTrigger key={section.id} value={section.id}>
              {t(section.label)}
            </TabsTrigger>
          ))}
        </TabsList>
      </Tabs>
      <div className="lg:hidden">
        <Select value={active} onValueChange={selectSection}>
          <SelectTrigger size="sm" aria-label={label} className="w-44">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {sections.map((section) => (
              <SelectItem key={section.id} value={section.id}>
                {t(section.label)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
    </nav>
  );
}

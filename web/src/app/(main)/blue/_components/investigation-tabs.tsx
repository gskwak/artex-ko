"use client";

import Link from "next/link";

import { useTranslations } from "next-intl";

import { cn } from "@/lib/utils";

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

  return (
    <nav
      aria-label={t.has("blueInvestigation") ? t("blueInvestigation") : t("blueCases")}
      className="flex w-fit max-w-full gap-1 overflow-x-auto rounded-lg bg-muted p-[3px]"
    >
      {sections.map((section) => (
        <Link
          key={section.id}
          href={section.href}
          aria-current={active === section.id ? "page" : undefined}
          className={cn(
            "shrink-0 rounded-md px-3 py-1 text-sm font-medium whitespace-nowrap text-foreground/60 transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
            active === section.id && "bg-background text-foreground shadow-sm",
          )}
        >
          {t(section.label)}
        </Link>
      ))}
    </nav>
  );
}

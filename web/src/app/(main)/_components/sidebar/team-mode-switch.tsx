"use client";

import { Crosshair, Shield } from "lucide-react";
import { useTranslations } from "next-intl";

import type { TeamMode } from "@/hooks/use-team-mode";
import { cn } from "@/lib/utils";

type TeamModeSwitchProps = {
  mode: TeamMode;
  onSelect: (mode: TeamMode) => void;
};

export function TeamModeSwitch({ mode, onSelect }: TeamModeSwitchProps) {
  const t = useTranslations("teamMode");
  const otherMode: TeamMode = mode === "red" ? "blue" : "red";

  return (
    <>
      <fieldset className="relative grid min-w-0 grid-cols-2 rounded-full border border-sidebar-border bg-sidebar-accent/60 p-1 group-data-[collapsible=icon]:hidden">
        <legend className="sr-only">{t("label")}</legend>
        <span
          aria-hidden="true"
          className={cn(
            "pointer-events-none absolute inset-y-1 w-[calc(50%_-_0.25rem)] rounded-full shadow-sm transition-[left,background-color] duration-200 ease-out motion-reduce:transition-none",
            mode === "red" ? "left-1" : "left-1/2",
            mode === "red" ? "bg-red-600" : "bg-blue-600",
          )}
        />
        {(["red", "blue"] as const).map((option) => (
          <button
            key={option}
            type="button"
            aria-pressed={mode === option}
            onClick={() => {
              if (option !== mode) onSelect(option);
            }}
            className={cn(
              "relative z-10 flex min-h-8 items-center justify-center gap-1.5 rounded-full px-2 font-semibold text-xs transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-sidebar-ring focus-visible:ring-offset-2 focus-visible:ring-offset-sidebar",
              mode === option ? "text-white" : "text-sidebar-foreground hover:text-sidebar-primary",
            )}
          >
            {option === "red" ? (
              <Crosshair aria-hidden="true" className="size-3.5 shrink-0" />
            ) : (
              <Shield aria-hidden="true" className="size-3.5 shrink-0" />
            )}
            <span>{t(option)}</span>
          </button>
        ))}
      </fieldset>
      <button
        type="button"
        onClick={() => onSelect(otherMode)}
        aria-label={t(otherMode === "blue" ? "switchToBlue" : "switchToRed")}
        title={t(otherMode === "blue" ? "switchToBlue" : "switchToRed")}
        className="hidden size-8 items-center justify-center rounded-md text-sidebar-foreground hover:bg-sidebar-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-sidebar-ring group-data-[collapsible=icon]:flex"
      >
        {mode === "red" ? (
          <Crosshair aria-hidden="true" className="size-4" />
        ) : (
          <Shield aria-hidden="true" className="size-4" />
        )}
      </button>
    </>
  );
}

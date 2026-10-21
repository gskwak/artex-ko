"use client";

import { Palette } from "lucide-react";
import { useTranslations } from "next-intl";

import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useTeamMode } from "@/hooks/use-team-mode";
import { persistPreference } from "@/lib/preferences/preferences-storage";
import { THEME_VARIANTS } from "@/lib/preferences/theme";
import { applyThemeMode } from "@/lib/preferences/theme-utils";
import { usePreferencesStore } from "@/stores/preferences/preferences-provider";

export function ThemeSwitcher() {
  const t = useTranslations("appearance");
  const { mode } = useTeamMode();
  const themeMode = usePreferencesStore((s) => s.themeMode);
  const setThemeMode = usePreferencesStore((s) => s.setThemeMode);

  const selectTheme = (value: string) => {
    const selected = THEME_VARIANTS.find((theme) => theme.value === value && theme.preset === mode);
    if (!selected) return;
    applyThemeMode(selected.mode);
    setThemeMode(selected.mode);
    void persistPreference("theme_mode", selected.mode);
  };

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button size="icon" aria-label={t("theme")} title={t("theme")}>
          <Palette />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="min-w-44">
        <DropdownMenuLabel>{t("theme")}</DropdownMenuLabel>
        <DropdownMenuRadioGroup value={`${mode}-${themeMode}`} onValueChange={selectTheme}>
          {THEME_VARIANTS.filter((theme) => theme.preset === mode).map((theme) => (
            <DropdownMenuRadioItem key={theme.value} value={theme.value}>
              <span className="size-3 rounded-full" style={{ backgroundColor: theme.color }} />
              {t(theme.value)}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

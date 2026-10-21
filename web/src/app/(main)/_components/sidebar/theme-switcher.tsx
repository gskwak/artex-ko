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
import { persistPreference } from "@/lib/preferences/preferences-storage";
import { THEME_VARIANTS } from "@/lib/preferences/theme";
import { applyThemeMode, applyThemePreset } from "@/lib/preferences/theme-utils";
import { usePreferencesStore } from "@/stores/preferences/preferences-provider";

export function ThemeSwitcher() {
  const t = useTranslations("appearance");
  const themeMode = usePreferencesStore((s) => s.themeMode);
  const themePreset = usePreferencesStore((s) => s.themePreset);
  const setThemeMode = usePreferencesStore((s) => s.setThemeMode);
  const setThemePreset = usePreferencesStore((s) => s.setThemePreset);

  const selectTheme = (value: string) => {
    const selected = THEME_VARIANTS.find((theme) => theme.value === value);
    if (!selected) return;
    applyThemePreset(selected.preset);
    applyThemeMode(selected.mode);
    setThemePreset(selected.preset);
    setThemeMode(selected.mode);
    void persistPreference("theme_preset", selected.preset);
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
        <DropdownMenuRadioGroup value={`${themePreset}-${themeMode}`} onValueChange={selectTheme}>
          {THEME_VARIANTS.map((theme) => (
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

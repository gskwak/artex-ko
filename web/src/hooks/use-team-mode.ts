"use client";

import * as React from "react";

import { usePathname, useRouter } from "next/navigation";

import { getClientCookie } from "@/lib/cookie.client";
import { persistPreference } from "@/lib/preferences/preferences-storage";
import { applyThemePreset } from "@/lib/preferences/theme-utils";
import { usePreferencesStore } from "@/stores/preferences/preferences-provider";

export type TeamMode = "red" | "blue";

const STORAGE_KEY = "artex_team_mode";
const MODE_CHANGE_EVENT = "artex-team-mode-change";

export const teamLandingRoute: Record<TeamMode, string> = {
  red: "/function/tasks",
  blue: "/blue/dashboard",
};

function modeFromPath(pathname: string): TeamMode | null {
  const path = pathname.replace(/\/+$/, "") || "/";
  if (path === "/blue" || path.startsWith("/blue/")) return "blue";
  if (path === "/dashboard" || path === "/chat") return "red";
  // The asset inventory is shared by both teams. System routes are shared too.
  const sharedAssets = path === "/function/assets" || path.startsWith("/function/assets/");
  if (path.startsWith("/function/") && !sharedAssets) return "red";
  return null;
}

function readStoredMode(): TeamMode {
  try {
    return window.localStorage.getItem(STORAGE_KEY) === "blue" ? "blue" : "red";
  } catch {
    return "red";
  }
}

function persistMode(mode: TeamMode) {
  try {
    window.localStorage.setItem(STORAGE_KEY, mode);
  } catch {
    // The route still determines the active mode when storage is unavailable.
  }
}

/** Keeps the selected mode in sync with direct links and shared routes. */
export function useTeamMode() {
  const pathname = usePathname();
  const router = useRouter();
  const themePreset = usePreferencesStore((state) => state.themePreset);
  const setThemePreset = usePreferencesStore((state) => state.setThemePreset);
  const [mode, setMode] = React.useState<TeamMode>(() => modeFromPath(pathname) ?? "red");

  const syncThemePreset = React.useCallback(
    (next: TeamMode) => {
      if (document.documentElement.getAttribute("data-theme-preset") !== next) {
        applyThemePreset(next);
      }
      if (themePreset !== next) {
        setThemePreset(next);
      }
      if (getClientCookie("theme_preset") !== next) void persistPreference("theme_preset", next);
    },
    [setThemePreset, themePreset],
  );

  React.useEffect(() => {
    const routeMode = modeFromPath(pathname);
    const next = routeMode ?? readStoredMode();
    setMode(next);
    syncThemePreset(next);
    if (routeMode) persistMode(routeMode);
  }, [pathname, syncThemePreset]);

  React.useEffect(() => {
    const onStorage = (event: StorageEvent) => {
      if (event.key === STORAGE_KEY && !modeFromPath(pathname)) {
        const next = readStoredMode();
        setMode(next);
        syncThemePreset(next);
      }
    };
    const onModeChange = (event: Event) => {
      const next = (event as CustomEvent<TeamMode>).detail;
      setMode(next);
      syncThemePreset(next);
    };
    window.addEventListener("storage", onStorage);
    window.addEventListener(MODE_CHANGE_EVENT, onModeChange);
    return () => {
      window.removeEventListener("storage", onStorage);
      window.removeEventListener(MODE_CHANGE_EVENT, onModeChange);
    };
  }, [pathname, syncThemePreset]);

  const selectMode = React.useCallback(
    (next: TeamMode) => {
      setMode(next);
      persistMode(next);
      syncThemePreset(next);
      window.dispatchEvent(new CustomEvent<TeamMode>(MODE_CHANGE_EVENT, { detail: next }));
      router.push(teamLandingRoute[next]);
    },
    [router, syncThemePreset],
  );

  return { mode, selectMode };
}

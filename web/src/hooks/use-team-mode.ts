"use client";

import * as React from "react";

import { usePathname, useRouter } from "next/navigation";

import { getClientCookie } from "@/lib/cookie.client";
import { persistPreference } from "@/lib/preferences/preferences-storage";
import { readTeamThemeMode, type TeamMode } from "@/lib/preferences/team-theme";
import { applyThemeMode, applyThemePreset } from "@/lib/preferences/theme-utils";
import {
  correspondingTeamRoute,
  persistLastTeamRoute,
  persistStoredTeamMode,
  readLastTeamRoute,
  readStoredTeamMode,
  TEAM_MODE_STORAGE_KEY,
  teamLandingRoute,
} from "@/lib/team-mode";
import { usePreferencesStore } from "@/stores/preferences/preferences-provider";

export type { TeamMode } from "@/lib/team-mode";
export { teamLandingRoute } from "@/lib/team-mode";

const MODE_CHANGE_EVENT = "artex-team-mode-change";

function modeFromPath(pathname: string): TeamMode | null {
  const path = pathname.replace(/\/+$/, "") || "/";
  if (path === "/blue" || path.startsWith("/blue/")) return "blue";
  if (path === "/redteam" || path.startsWith("/redteam/")) return "red";
  if (path === "/dashboard" || path === "/chat") return "red";
  // The asset inventory is shared by both teams. System routes are shared too.
  const sharedAssets = path === "/function/assets" || path.startsWith("/function/assets/");
  if (path.startsWith("/function/") && !sharedAssets) return "red";
  return null;
}

/** Keeps the selected mode in sync with direct links and shared routes. */
export function useTeamMode() {
  const pathname = usePathname();
  const router = useRouter();
  const themePreset = usePreferencesStore((state) => state.themePreset);
  const setThemePreset = usePreferencesStore((state) => state.setThemePreset);
  const themeMode = usePreferencesStore((state) => state.themeMode);
  const setThemeMode = usePreferencesStore((state) => state.setThemeMode);
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

  const syncThemeMode = React.useCallback(
    (next: TeamMode) => {
      const selected = readTeamThemeMode(next);
      if (document.documentElement.getAttribute("data-theme-mode") !== selected) {
        applyThemeMode(selected);
      }
      if (themeMode !== selected) {
        setThemeMode(selected);
      }
    },
    [setThemeMode, themeMode],
  );

  React.useEffect(() => {
    const routeMode = modeFromPath(pathname);
    const next = routeMode ?? readStoredTeamMode();
    setMode(next);
    syncThemePreset(next);
    syncThemeMode(next);
    if (routeMode) {
      persistStoredTeamMode(routeMode);
      persistLastTeamRoute(routeMode, pathname + window.location.search);
    }
  }, [pathname, syncThemeMode, syncThemePreset]);

  React.useEffect(() => {
    const onStorage = (event: StorageEvent) => {
      if (event.key === TEAM_MODE_STORAGE_KEY && !modeFromPath(pathname)) {
        const next = readStoredTeamMode();
        setMode(next);
        syncThemePreset(next);
        syncThemeMode(next);
      }
    };
    const onModeChange = (event: Event) => {
      const next = (event as CustomEvent<TeamMode>).detail;
      setMode(next);
      syncThemePreset(next);
      syncThemeMode(next);
    };
    window.addEventListener("storage", onStorage);
    window.addEventListener(MODE_CHANGE_EVENT, onModeChange);
    return () => {
      window.removeEventListener("storage", onStorage);
      window.removeEventListener(MODE_CHANGE_EVENT, onModeChange);
    };
  }, [pathname, syncThemeMode, syncThemePreset]);

  const selectMode = React.useCallback(
    (next: TeamMode) => {
      const currentRoute = window.location.pathname + window.location.search;
      const currentMode = modeFromPath(window.location.pathname);
      if (currentMode) persistLastTeamRoute(currentMode, currentRoute);
      const destination =
        readLastTeamRoute(next) ?? correspondingTeamRoute(window.location.pathname, next) ?? teamLandingRoute[next];
      setMode(next);
      persistStoredTeamMode(next);
      syncThemePreset(next);
      syncThemeMode(next);
      window.dispatchEvent(new CustomEvent<TeamMode>(MODE_CHANGE_EVENT, { detail: next }));
      router.push(destination);
    },
    [router, syncThemeMode, syncThemePreset],
  );

  return { mode, selectMode };
}

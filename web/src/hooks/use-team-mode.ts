"use client";

import * as React from "react";

import { usePathname, useRouter } from "next/navigation";

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
  const [mode, setMode] = React.useState<TeamMode>(() => modeFromPath(pathname) ?? "red");

  React.useEffect(() => {
    const routeMode = modeFromPath(pathname);
    const next = routeMode ?? readStoredMode();
    setMode(next);
    if (routeMode) persistMode(routeMode);
  }, [pathname]);

  React.useEffect(() => {
    const onStorage = (event: StorageEvent) => {
      if (event.key === STORAGE_KEY && !modeFromPath(pathname)) setMode(readStoredMode());
    };
    const onModeChange = (event: Event) => {
      setMode((event as CustomEvent<TeamMode>).detail);
    };
    window.addEventListener("storage", onStorage);
    window.addEventListener(MODE_CHANGE_EVENT, onModeChange);
    return () => {
      window.removeEventListener("storage", onStorage);
      window.removeEventListener(MODE_CHANGE_EVENT, onModeChange);
    };
  }, [pathname]);

  const selectMode = React.useCallback(
    (next: TeamMode) => {
      setMode(next);
      persistMode(next);
      window.dispatchEvent(new CustomEvent<TeamMode>(MODE_CHANGE_EVENT, { detail: next }));
      router.push(teamLandingRoute[next]);
    },
    [router],
  );

  return { mode, selectMode };
}

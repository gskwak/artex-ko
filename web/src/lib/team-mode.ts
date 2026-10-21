export type TeamMode = "red" | "blue";

export const TEAM_MODE_STORAGE_KEY = "artex_team_mode";

const LAST_TEAM_ROUTE_STORAGE_KEY: Record<TeamMode, string> = {
  red: "artex_last_red_route",
  blue: "artex_last_blue_route",
};

const MATCHED_SECTIONS: ReadonlyArray<readonly [red: string, blue: string]> = [
  ["dashboard", "dashboard"],
  ["chat", "chat"],
  ["tasks", "tasks"],
  ["findings", "cases"],
  ["traffic", "traffic"],
  ["llm-records", "llm-records"],
];

export const teamLandingRoute: Record<TeamMode, string> = {
  red: "/redteam/tasks",
  blue: "/blue/dashboard",
};

export function readStoredTeamMode(): TeamMode {
  try {
    return window.localStorage.getItem(TEAM_MODE_STORAGE_KEY) === "blue" ? "blue" : "red";
  } catch {
    return "red";
  }
}

export function persistStoredTeamMode(mode: TeamMode) {
  try {
    window.localStorage.setItem(TEAM_MODE_STORAGE_KEY, mode);
  } catch {
    // The team route still determines the active mode when storage is unavailable.
  }
}

function validTeamRoute(mode: TeamMode, value: string): string | null {
  if (!value.startsWith("/") || value.startsWith("//")) return null;
  try {
    const url = new URL(value, window.location.origin);
    const prefix = mode === "red" ? "/redteam" : "/blue";
    if (url.origin !== window.location.origin) return null;
    if (url.pathname !== prefix && !url.pathname.startsWith(`${prefix}/`)) return null;
    return url.pathname + url.search;
  } catch {
    return null;
  }
}

export function readLastTeamRoute(mode: TeamMode): string | null {
  try {
    const stored = window.sessionStorage.getItem(LAST_TEAM_ROUTE_STORAGE_KEY[mode]);
    return stored ? validTeamRoute(mode, stored) : null;
  } catch {
    return null;
  }
}

export function persistLastTeamRoute(mode: TeamMode, pathWithQuery: string) {
  const valid = validTeamRoute(mode, pathWithQuery);
  if (!valid) return;
  try {
    window.sessionStorage.setItem(LAST_TEAM_ROUTE_STORAGE_KEY[mode], valid);
  } catch {
    // Switching teams still works through the default route when storage is unavailable.
  }
}

/** Return the other team's list page for the current feature, without carrying team-specific IDs. */
export function correspondingTeamRoute(pathname: string, next: TeamMode): string | null {
  const sourcePrefix = next === "blue" ? "/redteam/" : "/blue/";
  if (!pathname.startsWith(sourcePrefix)) return null;
  const sourceSection = pathname.slice(sourcePrefix.length).split("/")[0];
  const match = MATCHED_SECTIONS.find(([red, blue]) => sourceSection === (next === "blue" ? red : blue));
  if (match) return `/${next === "blue" ? "blue" : "redteam"}/${next === "blue" ? match[1] : match[0]}`;
  if (next === "red" && ["events", "detection", "alerts", "rules", "actions"].includes(sourceSection)) {
    return "/redteam/findings";
  }
  return null;
}

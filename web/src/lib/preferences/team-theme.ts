import { getClientCookie, setClientCookie } from "@/lib/cookie.client";
import type { TeamMode } from "@/lib/team-mode";

import { TEAM_THEME_MODE_COOKIE_PREFIX, type ThemeMode } from "./theme";

export type { TeamMode } from "@/lib/team-mode";

export function readTeamThemeMode(team: TeamMode): ThemeMode {
  return getClientCookie(`${TEAM_THEME_MODE_COOKIE_PREFIX}${team}`) === "dark" ? "dark" : "light";
}

export function persistTeamThemeMode(team: TeamMode, mode: ThemeMode) {
  setClientCookie(`${TEAM_THEME_MODE_COOKIE_PREFIX}${team}`, mode);
}

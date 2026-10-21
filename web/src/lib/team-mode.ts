export type TeamMode = "red" | "blue";

export const TEAM_MODE_STORAGE_KEY = "artex_team_mode";

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

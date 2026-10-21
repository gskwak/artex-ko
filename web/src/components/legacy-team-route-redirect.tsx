"use client";

import * as React from "react";

import { readStoredTeamMode } from "@/lib/team-mode";

/** Keeps existing bookmarks and deep links working after the team route split. */
export function LegacyTeamRouteRedirect({ redTarget, blueTarget }: { redTarget: string; blueTarget?: string }) {
  React.useEffect(() => {
    let target = redTarget;
    if (blueTarget && readStoredTeamMode() === "blue") target = blueTarget;
    window.location.replace(`${target}${window.location.search}${window.location.hash}`);
  }, [redTarget, blueTarget]);

  return null;
}

import { LegacyTeamRouteRedirect } from "@/components/legacy-team-route-redirect";

export default function Home() {
  return <LegacyTeamRouteRedirect redTarget="/redteam/tasks" blueTarget="/blue/dashboard" />;
}

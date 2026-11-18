/**
 * Maps a pathname to the closed set of route groups the API accepts for client telemetry
 * (`clientRouteGroups` in backend/internal/apiserver/client_event_routes.go). Nothing else of the
 * URL leaves the browser.
 */
export const ROUTE_GROUPS = [
  "home",
  "explore",
  "graduated",
  "token",
  "create",
  "wallet",
  "profile",
  "forum",
  "analytics",
  "docs",
  "leaderboard",
  "notifications",
  "transactions",
  "other",
] as const;

export type RouteGroup = (typeof ROUTE_GROUPS)[number];

/** First path segments that are real top-level routes; `/v1/transactions/*` is handled apart. */
const TOP_LEVEL = new Set<string>(
  ROUTE_GROUPS.filter((group) => group !== "home" && group !== "other" && group !== "transactions"),
);

export function routeGroup(pathname: string): RouteGroup {
  const [first, second] = (pathname.split(/[?#]/, 1)[0] ?? "").split("/").filter(Boolean);
  if (first === undefined) return "home";
  if (first === "v1" && second === "transactions") return "transactions";
  return TOP_LEVEL.has(first) ? (first as RouteGroup) : "other";
}

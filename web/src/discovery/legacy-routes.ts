import { decodeExploreView } from "./query-state";

/** Old home-page views that are now a sort on Trending. */
const RANKED_VIEWS: Record<string, string> = {
  marketcap: "market_cap",
  volume: "volume_24h",
};

/** Params a legacy link may carry that still mean the same thing on /explore. */
const CARRIED_PARAMS = ["q", "age"] as const;

/**
 * Where a legacy home-page link (`/?view=…`) belongs on `/explore`, or null when it has no `view`
 * and the home page should render. Unknown views land on Trending.
 */
export function legacyExploreTarget(params: URLSearchParams): string | null {
  const raw = params.get("view");
  if (raw === null) return null;
  const next = new URLSearchParams();
  const rankedSort = RANKED_VIEWS[raw];
  if (rankedSort) {
    next.set("view", "trending");
    next.set("sort", rankedSort);
  } else {
    next.set("view", decodeExploreView(params));
  }
  for (const key of CARRIED_PARAMS) {
    const value = params.get(key);
    if (value) next.set(key, value);
  }
  return `/explore?${next.toString()}`;
}

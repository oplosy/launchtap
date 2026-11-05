export const TOKEN_LIST_LIMIT = 20;
export const TOKEN_PHASES = ["curve", "graduated"] as const;
export type TokenPhase = (typeof TOKEN_PHASES)[number];
export const TOKEN_SORTS = ["newest", "oldest", "market_cap", "volume_24h"] as const;
export type TokenSort = (typeof TOKEN_SORTS)[number];
export const TOKEN_AGES = ["all", "24h", "7d"] as const;
export type TokenAge = (typeof TOKEN_AGES)[number];
export const EXPLORE_VIEWS = ["trending", "new", "marketcap", "volume", "watchlist"] as const;
export type ExploreView = (typeof EXPLORE_VIEWS)[number];
export const DEFAULT_EXPLORE_VIEW: ExploreView = "trending";

export type TokenListQueryState = {
  q: string;
  phase: TokenPhase;
  sort: TokenSort;
  age: TokenAge;
};

export const VIEW_LABELS: Record<ExploreView, string> = {
  trending: "Trending",
  new: "New",
  marketcap: "Market cap",
  volume: "Volume",
  watchlist: "Watchlist",
};

export const AGE_LABELS: Record<TokenAge, string> = {
  all: "All time",
  "24h": "24h",
  "7d": "7d",
};

export const SORT_LABELS: Record<TokenSort, string> = {
  newest: "Newest launches",
  oldest: "Oldest launches",
  market_cap: "Market cap",
  volume_24h: "24h volume",
};

export const PHASE_LABELS: Record<TokenPhase, string> = {
  curve: "Curve",
  graduated: "Graduated",
};

export function defaultTokenListQuery(defaultPhase: TokenPhase = "curve"): TokenListQueryState {
  return { q: "", phase: defaultPhase, sort: "newest", age: "all" };
}

function isPhase(value: string | null): value is TokenPhase {
  return value !== null && (TOKEN_PHASES as readonly string[]).includes(value);
}

function isSort(value: string | null): value is TokenSort {
  return value !== null && (TOKEN_SORTS as readonly string[]).includes(value);
}

function isAge(value: string | null): value is TokenAge {
  return value !== null && (TOKEN_AGES as readonly string[]).includes(value);
}

function isView(value: string | null): value is ExploreView {
  return value !== null && (EXPLORE_VIEWS as readonly string[]).includes(value);
}

/** The Explore view tab from the URL; unknown values fall back to Trending. */
export function decodeExploreView(input: string | URLSearchParams): ExploreView {
  const params = typeof input === "string" ? new URLSearchParams(input) : input;
  const raw = params.get("view");
  return isView(raw) ? raw : DEFAULT_EXPLORE_VIEW;
}

/** The list sort a view maps to, or null for views that are not a paged token list. */
export function exploreViewSort(view: ExploreView): TokenSort | null {
  switch (view) {
    case "new":
      return "newest";
    case "marketcap":
      return "market_cap";
    case "volume":
      return "volume_24h";
    default:
      return null;
  }
}

/** True for the views whose age control narrows the list (the ranked token lists). */
export function viewSupportsAge(view: ExploreView) {
  return exploreViewSort(view) !== null;
}

/**
 * Merge the params one section owns into the current URL, leaving every other param untouched
 * so the Graduated and Explore sections never clobber each other. A null value removes a param.
 */
export function mergeOwnedParams(
  current: URLSearchParams | string,
  owned: Record<string, string | null>,
) {
  const params = new URLSearchParams(typeof current === "string" ? current : current.toString());
  for (const [key, value] of Object.entries(owned)) {
    if (value === null || value === "") params.delete(key);
    else params.set(key, value);
  }
  return params.toString();
}

/** Parse only the filters supported by GET /v1/tokens; malformed values fall back safely. */
export function decodeTokenListQuery(
  input: string | URLSearchParams,
  defaultPhase: TokenPhase = "curve",
): TokenListQueryState {
  const params = typeof input === "string" ? new URLSearchParams(input) : input;
  const rawQ = params.get("q") ?? "";
  const rawPhase = params.get("phase");
  const rawSort = params.get("sort");
  const rawAge = params.get("age");
  return {
    q: rawQ.trim(),
    phase: isPhase(rawPhase) ? rawPhase : defaultPhase,
    sort: isSort(rawSort) ? rawSort : "newest",
    age: isAge(rawAge) ? rawAge : "all",
  };
}

/** Canonical, shareable URL state. Defaults are omitted to keep links stable and readable. */
export function encodeTokenListQuery(
  state: TokenListQueryState,
  defaultPhase: TokenPhase = "curve",
) {
  const params = new URLSearchParams();
  if (state.q.trim()) params.set("q", state.q.trim());
  if (state.phase !== defaultPhase) params.set("phase", state.phase);
  if (state.sort !== "newest") params.set("sort", state.sort);
  if (state.age !== "all") params.set("age", state.age);
  return params.toString();
}

export function tokenListFilters(state: TokenListQueryState) {
  return {
    phase: state.phase,
    q: state.q,
    sort: state.sort,
    limit: TOKEN_LIST_LIMIT,
    ...(state.age !== "all" ? { age: state.age } : {}),
  };
}

export function sameTokenListQuery(a: TokenListQueryState, b: TokenListQueryState) {
  return a.q === b.q && a.phase === b.phase && a.sort === b.sort && a.age === b.age;
}

export function normalizeTokenListQuery(
  state: TokenListQueryState,
  defaultPhase: TokenPhase = "curve",
): TokenListQueryState {
  return defaultPhase === "graduated" ? { ...state, phase: "graduated" } : state;
}

export function commitTokenListFilters(
  state: TokenListQueryState,
  currentSearchDraft: string,
  changes: Partial<Pick<TokenListQueryState, "phase" | "sort" | "age">>,
): TokenListQueryState {
  return { ...state, ...changes, q: currentSearchDraft.trim() };
}

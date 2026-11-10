export const TOKEN_LIST_LIMIT = 20;
export const TOKEN_PHASES = ["curve", "graduated"] as const;
export type TokenPhase = (typeof TOKEN_PHASES)[number];
export const TOKEN_SORTS = ["newest", "oldest", "market_cap", "volume_24h"] as const;
export type TokenSort = (typeof TOKEN_SORTS)[number];
export const TOKEN_AGES = ["all", "24h", "7d"] as const;
export type TokenAge = (typeof TOKEN_AGES)[number];
export const EXPLORE_VIEWS = ["trending", "new", "graduated", "watchlist"] as const;
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
  graduated: "Graduated",
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

/** The sorts that turn Trending from the highlights ranking into a ranked token list. */
const TRENDING_LIST_SORTS: ReadonlyArray<TokenSort> = ["market_cap", "volume_24h"];

/**
 * The paged `/v1/tokens` list a view shows, or null when the view is not a paged list (Trending's
 * highlights ranking, Watchlist). Trending becomes a curve list when sorted by market cap or
 * volume (the old Market cap and Volume tabs) or while a search is active.
 */
export function exploreListQuery(
  view: ExploreView,
  params: URLSearchParams,
): { phase: TokenPhase; sort: TokenSort } | null {
  const rawSort = params.get("sort");
  switch (view) {
    case "new":
      return { phase: "curve", sort: "newest" };
    case "graduated":
      return { phase: "graduated", sort: isSort(rawSort) ? rawSort : "newest" };
    case "trending":
      if (isSort(rawSort) && TRENDING_LIST_SORTS.includes(rawSort))
        return { phase: "curve", sort: rawSort };
      return (params.get("q") ?? "").trim() ? { phase: "curve", sort: "newest" } : null;
    default:
      return null;
  }
}

/**
 * Merge the params a control owns into the current URL, leaving every other param untouched.
 * A null value removes a param.
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

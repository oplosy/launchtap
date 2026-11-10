import type { components, paths } from "./generated";
import { mapProblem } from "./problems";

export type ProtocolDailyResponse =
  paths["/stats/protocol/daily"]["get"]["responses"][200]["content"]["application/json"];
export type ProtocolDailyItem = components["schemas"]["ProtocolDailyDTO"];
export type TokenListResponse = components["schemas"]["TokenListBody"];
export type TokenDetailResponse = components["schemas"]["TokenDetailDTO"];
export type CandleResponse = components["schemas"]["CandleBody"];
export type TradesResponse = components["schemas"]["TradesBody"];
export type HoldersResponse = components["schemas"]["HoldersBody"];
export type QuoteResponse = components["schemas"]["QuoteBody"];
export type CanonicalTransactionResponse = components["schemas"]["CanonicalObservationBody"];
export type EthUsdReference = components["schemas"]["EthUsdBody"];
export type RecentTradesResponse = components["schemas"]["RecentTradesBody"];
export type TokenHighlights = components["schemas"]["HighlightsBody"];
export type WatchlistResponse = components["schemas"]["WatchlistBody"];
export type WalletResponse = components["schemas"]["WalletDTO"];
export type WalletPositionsResponse = components["schemas"]["WalletPositionsBody"];
export type WalletTradesResponse = components["schemas"]["WalletTradesBody"];
export type WalletCreatedResponse = components["schemas"]["WalletCreatedBody"];
export type LeaderboardResponse = components["schemas"]["LeaderboardBody"];
export type TokenListQuery = {
  age?: "all" | "24h" | "7d";
  phase?: string;
  q?: string;
  sort?: string;
  cursor?: string;
  limit?: number;
};
export type AuthHeaders = { accessToken?: string; identityToken?: string };
export type RevisionResponse = components["schemas"]["RevisionBody"];
export type MetadataReadResponse = components["schemas"]["MetadataReadBody"];
export type ProfileResponse = components["schemas"]["ProfileBody"];
export type CommentListResponse = components["schemas"]["CommentListBody"];
export type CommentResponse = components["schemas"]["CommentDTO"];
export type CommentEligibilityResponse = components["schemas"]["EligibilityBody"];
export type ReportReason = "spam" | "scam" | "abuse" | "other";
export type PublicProfileResponse = components["schemas"]["PublicProfileDTO"];

/** Resolve API-owned relative image paths without allowing a metadata URL to change origin. */
export function resolveApiAssetUrl(
  baseUrl: string | null | undefined,
  value: string | null | undefined,
) {
  if (!value) return null;
  try {
    const parsed = new URL(value);
    if (parsed.protocol === "https:") return parsed.toString();
    if (!baseUrl || !value.startsWith("/")) return null;
    const base = new URL(baseUrl);
    const resolved = new URL(value, base);
    return resolved.origin === base.origin &&
      (base.protocol === "https:" || isLocalHost(base.hostname))
      ? resolved.toString()
      : null;
  } catch {
    if (!baseUrl || !value.startsWith("/")) return null;
    try {
      const base = new URL(baseUrl);
      const resolved = new URL(value, base);
      return resolved.origin === base.origin &&
        (base.protocol === "https:" || isLocalHost(base.hostname))
        ? resolved.toString()
        : null;
    } catch {
      return null;
    }
  }
}

function isLocalHost(hostname: string) {
  return hostname === "localhost" || hostname === "127.0.0.1" || hostname === "[::1]";
}

export class ApiClient {
  private readonly fetchImpl: typeof globalThis.fetch;
  constructor(private readonly options: { baseUrl: string; fetch?: typeof globalThis.fetch }) {
    this.fetchImpl = options.fetch ?? ((input, init) => globalThis.fetch(input, init));
  }

  async request<T>(path: string, init: RequestInit = {}, auth?: AuthHeaders): Promise<T> {
    const url = new URL(path, this.options.baseUrl);
    for (const key of url.searchParams.keys()) {
      if (/authorization|token|signature|secret|private|password/i.test(key))
        throw new Error("Sensitive values are not permitted in request URLs");
    }
    const headers = new Headers(init.headers);
    headers.set("Accept", "application/json");
    if (auth?.accessToken) headers.set("Authorization", `Bearer ${auth.accessToken}`);
    if (auth?.identityToken) headers.set("privy-id-token", auth.identityToken);
    const response = await this.fetchImpl(url, { ...init, headers });
    if (!response.ok) {
      let body: unknown;
      try {
        body = await response.json();
      } catch {
        body = undefined;
      }
      throw mapProblem(response.status, body);
    }
    return (await response.json()) as T;
  }

  private async requestNoContent(path: string, init: RequestInit, auth: AuthHeaders) {
    const url = new URL(path, this.options.baseUrl);
    const headers = new Headers(init.headers);
    headers.set("Accept", "application/json");
    if (auth.accessToken) headers.set("Authorization", `Bearer ${auth.accessToken}`);
    if (auth.identityToken) headers.set("privy-id-token", auth.identityToken);
    const response = await this.fetchImpl(url, { ...init, headers });
    if (!response.ok) {
      let body: unknown;
      try {
        body = await response.json();
      } catch {
        body = undefined;
      }
      throw mapProblem(response.status, body);
    }
  }

  private async requestWithResponse<T>(path: string, init: RequestInit, auth?: AuthHeaders) {
    const url = new URL(path, this.options.baseUrl);
    const headers = new Headers(init.headers);
    headers.set("Accept", "application/json");
    if (auth?.accessToken) headers.set("Authorization", `Bearer ${auth.accessToken}`);
    if (auth?.identityToken) headers.set("privy-id-token", auth.identityToken);
    const response = await this.fetchImpl(url, { ...init, headers });
    if (!response.ok) {
      let body: unknown;
      try {
        body = await response.json();
      } catch {
        body = undefined;
      }
      throw mapProblem(response.status, body);
    }
    return {
      body: (await response.json()) as T,
      etag: response.headers.get("ETag"),
    };
  }

  async getTokenImageRevision(address: string, signal?: AbortSignal) {
    const url = new URL(`/v1/tokens/${encodeURIComponent(address)}/image`, this.options.baseUrl);
    const response = await this.fetchImpl(url, { headers: { Accept: "image/*" }, signal });
    if (!response.ok) {
      let body: unknown;
      try {
        body = await response.json();
      } catch {
        body = undefined;
      }
      throw mapProblem(response.status, body);
    }
    const revision = Number(response.headers.get("X-Revision") ?? "0");
    return {
      revision: Number.isSafeInteger(revision) && revision >= 0 ? revision : 0,
      etag: response.headers.get("ETag"),
    };
  }

  updateTokenMetadata(
    address: string,
    body: components["schemas"]["MetadataBody"],
    revision: number,
    etag: string | null | undefined,
    auth: AuthHeaders,
  ) {
    return this.requestWithResponse<RevisionResponse>(
      `/v1/tokens/${encodeURIComponent(address)}/metadata`,
      {
        method: "PUT",
        headers: { "Content-Type": "application/json", "If-Match": etag ?? `"${revision}"` },
        body: JSON.stringify(body),
      },
      auth,
    );
  }

  getTokenMetadata(address: string, signal?: AbortSignal) {
    return this.requestWithResponse<MetadataReadResponse>(
      `/v1/tokens/${encodeURIComponent(address)}/metadata`,
      { signal },
    );
  }

  updateTokenImage(address: string, file: File, revision: number, auth: AuthHeaders) {
    return this.requestWithResponse<RevisionResponse>(
      `/v1/tokens/${encodeURIComponent(address)}/image`,
      {
        method: "PUT",
        // The image GET validator is a content hash. Image writes use the
        // independent numeric revision exposed by X-Revision instead.
        headers: { "Content-Type": file.type, "If-Match": `"${revision}"` },
        body: file,
      },
      auth,
    );
  }

  getProtocolDaily(options: { from?: string; to?: string; limit?: number } = {}) {
    const query = new URLSearchParams();
    for (const [key, value] of Object.entries(options))
      if (value !== undefined) query.set(key, String(value));
    return this.request<ProtocolDailyResponse>(
      `/v1/stats/protocol/daily${query.size ? `?${query}` : ""}`,
    );
  }

  getProtocol(signal?: AbortSignal) {
    return this.request<components["schemas"]["ProtocolDTO"]>("/v1/stats/protocol", { signal });
  }

  getProfile(auth: AuthHeaders, signal?: AbortSignal) {
    return this.request<ProfileResponse>("/v1/profile", { signal }, auth);
  }

  getEthUsd(signal?: AbortSignal) {
    return this.request<EthUsdReference>("/v1/reference/eth-usd", { signal });
  }

  listRecentTrades(limit = 50, signal?: AbortSignal) {
    return this.request<RecentTradesResponse>(`/v1/trades/recent?limit=${limit}`, { signal });
  }

  getHighlights(signal?: AbortSignal) {
    return this.request<TokenHighlights>("/v1/tokens/highlights", { signal });
  }

  listTokenComments(
    address: string,
    query: { cursor?: string; limit?: number } = {},
    init: { signal?: AbortSignal; cache?: RequestCache } = {},
  ) {
    const params = new URLSearchParams();
    if (query.cursor) params.set("cursor", query.cursor);
    if (query.limit) params.set("limit", String(query.limit));
    const encoded = params.toString();
    const suffix = encoded ? `?${encoded}` : "";
    return this.request<CommentListResponse>(
      `/v1/tokens/${encodeURIComponent(address)}/comments${suffix}`,
      init,
    );
  }

  getCommentEligibility(address: string, auth: AuthHeaders, signal?: AbortSignal) {
    return this.request<CommentEligibilityResponse>(
      `/v1/tokens/${encodeURIComponent(address)}/comments/eligibility`,
      { signal, cache: "no-store" },
      auth,
    );
  }

  postTokenComment(
    address: string,
    body: { body: string; wallet: string },
    auth: AuthHeaders,
    signal?: AbortSignal,
  ) {
    return this.request<CommentResponse>(
      `/v1/tokens/${encodeURIComponent(address)}/comments`,
      {
        method: "POST",
        signal,
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      },
      auth,
    );
  }

  reportComment(id: string, reason: ReportReason, auth: AuthHeaders, signal?: AbortSignal) {
    return this.requestNoContent(
      `/v1/comments/${encodeURIComponent(id)}/report`,
      {
        method: "PUT",
        signal,
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ reason }),
      },
      auth,
    );
  }

  getPublicProfile(auth: AuthHeaders, signal?: AbortSignal) {
    return this.request<PublicProfileResponse>(
      "/v1/me/public-profile",
      { signal, cache: "no-store" },
      auth,
    );
  }

  putPublicProfile(
    body: { username?: string; public_wallets: string[] },
    auth: AuthHeaders,
    signal?: AbortSignal,
  ) {
    return this.request<PublicProfileResponse>(
      "/v1/me/public-profile",
      {
        method: "PUT",
        signal,
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      },
      auth,
    );
  }

  deletePublicProfile(auth: AuthHeaders, signal?: AbortSignal) {
    return this.requestNoContent("/v1/me/public-profile", { method: "DELETE", signal }, auth);
  }

  getWatchlist(auth: AuthHeaders, signal?: AbortSignal) {
    return this.request<WatchlistResponse>("/v1/me/watchlist", { signal }, auth);
  }

  addWatchlist(address: string, auth: AuthHeaders, signal?: AbortSignal) {
    return this.requestNoContent(
      `/v1/me/watchlist/${encodeURIComponent(address)}`,
      { method: "PUT", signal },
      auth,
    );
  }

  removeWatchlist(address: string, auth: AuthHeaders, signal?: AbortSignal) {
    return this.requestNoContent(
      `/v1/me/watchlist/${encodeURIComponent(address)}`,
      { method: "DELETE", signal },
      auth,
    );
  }

  getToken(address: string, signal?: AbortSignal) {
    return this.request<TokenDetailResponse>(`/v1/tokens/${encodeURIComponent(address)}`, {
      signal,
    });
  }

  getCandles(
    address: string,
    options: {
      interval?: string;
      from?: string;
      to?: string;
      limit?: number;
      cursor?: string;
    } = {},
    signal?: AbortSignal,
  ) {
    const query = new URLSearchParams();
    for (const [key, value] of Object.entries(options)) {
      if (value !== undefined && value !== "") query.set(key, String(value));
    }
    return this.request<CandleResponse>(
      `/v1/tokens/${encodeURIComponent(address)}/candles${query.size ? `?${query}` : ""}`,
      { signal },
    );
  }

  getTrades(
    address: string,
    options: { cursor?: string; limit?: number } = {},
    signal?: AbortSignal,
  ) {
    return this.getCursorCollection<TradesResponse>(address, "trades", options, signal);
  }

  getHolders(
    address: string,
    options: { cursor?: string; limit?: number } = {},
    signal?: AbortSignal,
  ) {
    return this.getCursorCollection<HoldersResponse>(address, "holders", options, signal);
  }

  private getCursorCollection<T>(
    address: string,
    resource: "trades" | "holders",
    options: { cursor?: string; limit?: number },
    signal?: AbortSignal,
  ) {
    const query = new URLSearchParams();
    for (const [key, value] of Object.entries(options)) {
      if (value !== undefined && value !== "") query.set(key, String(value));
    }
    return this.request<T>(
      `/v1/tokens/${encodeURIComponent(address)}/${resource}${query.size ? `?${query}` : ""}`,
      { signal },
    );
  }

  getWallet(address: string, signal?: AbortSignal) {
    return this.request<WalletResponse>(`/v1/wallets/${encodeURIComponent(address)}`, { signal });
  }

  /** Open positions (`closed` false) come in one response and take no cursor. */
  listWalletPositions(
    address: string,
    options: { closed?: boolean; cursor?: string; limit?: number } = {},
    signal?: AbortSignal,
  ) {
    return this.getWalletCollection<WalletPositionsResponse>(address, "positions", options, signal);
  }

  listWalletTrades(
    address: string,
    options: { cursor?: string; limit?: number } = {},
    signal?: AbortSignal,
  ) {
    return this.getWalletCollection<WalletTradesResponse>(address, "trades", options, signal);
  }

  listWalletCreated(
    address: string,
    options: { cursor?: string; limit?: number } = {},
    signal?: AbortSignal,
  ) {
    return this.getWalletCollection<WalletCreatedResponse>(address, "created", options, signal);
  }

  getLeaderboard(limit = 100, signal?: AbortSignal) {
    return this.request<LeaderboardResponse>(`/v1/leaderboard?limit=${limit}`, { signal });
  }

  private getWalletCollection<T>(
    address: string,
    resource: "positions" | "trades" | "created",
    options: { closed?: boolean; cursor?: string; limit?: number },
    signal?: AbortSignal,
  ) {
    const query = new URLSearchParams();
    for (const [key, value] of Object.entries(options)) {
      if (value !== undefined && value !== "") query.set(key, String(value));
    }
    return this.request<T>(
      `/v1/wallets/${encodeURIComponent(address)}/${resource}${query.size ? `?${query}` : ""}`,
      { signal },
    );
  }

  getTokens(options: TokenListQuery = {}, signal?: AbortSignal) {
    const query = new URLSearchParams();
    for (const [key, value] of Object.entries(options)) {
      if (value !== undefined && value !== "") query.set(key, String(value));
    }
    return this.request<TokenListResponse>(
      `/v1/tokens${query.size ? `?${query.toString()}` : ""}`,
      { signal },
    );
  }

  getQuote(address: string, input: components["schemas"]["QuoteInputBody"], signal?: AbortSignal) {
    return this.request<QuoteResponse>(`/v1/tokens/${encodeURIComponent(address)}/quote`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(input),
      signal,
    });
  }

  getCanonicalTransaction(hash: string, signal?: AbortSignal) {
    return this.request<CanonicalTransactionResponse>(
      `/v1/transactions/${encodeURIComponent(hash)}`,
      { signal },
    );
  }
}

export function getProtocolDaily(
  baseUrl: string,
  options: { from?: string; to?: string; limit?: number } = {},
) {
  return new ApiClient({ baseUrl }).getProtocolDaily(options);
}

export function getTokens(baseUrl: string, options: TokenListQuery = {}, signal?: AbortSignal) {
  return new ApiClient({ baseUrl }).getTokens(options, signal);
}

export function getEthUsd(baseUrl: string, signal?: AbortSignal) {
  return new ApiClient({ baseUrl }).getEthUsd(signal);
}

export function getHighlights(baseUrl: string, signal?: AbortSignal) {
  return new ApiClient({ baseUrl }).getHighlights(signal);
}

export function getWatchlist(baseUrl: string, auth: AuthHeaders, signal?: AbortSignal) {
  return new ApiClient({ baseUrl }).getWatchlist(auth, signal);
}

export function addWatchlist(
  baseUrl: string,
  address: string,
  auth: AuthHeaders,
  signal?: AbortSignal,
) {
  return new ApiClient({ baseUrl }).addWatchlist(address, auth, signal);
}

export function removeWatchlist(
  baseUrl: string,
  address: string,
  auth: AuthHeaders,
  signal?: AbortSignal,
) {
  return new ApiClient({ baseUrl }).removeWatchlist(address, auth, signal);
}

import type { components } from "./generated";
import { AccountApi } from "./account-client";
import type {
  ProtocolDailyResponse,
  ProtocolRevenueResponse,
  TokenListResponse,
  TokenDetailResponse,
  CandleResponse,
  TradesResponse,
  HoldersResponse,
  EarlyBuyersResponse,
  QuoteResponse,
  CanonicalTransactionResponse,
  EthUsdReference,
  RecentTradesResponse,
  TokenHighlights,
  WalletResponse,
  WalletPositionsResponse,
  WalletTradesResponse,
  WalletCreatedResponse,
  LeaderboardResponse,
  TokenListQuery,
  AuthHeaders,
  RevisionResponse,
  MetadataReadResponse,
} from "./response-types";

export type * from "./response-types";
export { resolveApiAssetUrl } from "./asset-url";

/** The API client: token, market and wallet reads on top of the account endpoints and transport. */
export class ApiClient extends AccountApi {
  async getTokenImageRevision(address: string, signal?: AbortSignal) {
    const response = await this.send(
      `/v1/tokens/${encodeURIComponent(address)}/image`,
      { signal },
      undefined,
      "image/*",
    );
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

  getProtocolRevenue(signal?: AbortSignal) {
    return this.request<ProtocolRevenueResponse>("/v1/stats/protocol/revenue", { signal });
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

  getEarlyBuyers(address: string, signal?: AbortSignal) {
    return this.request<EarlyBuyersResponse>(
      `/v1/tokens/${encodeURIComponent(address)}/early-buyers`,
      { signal },
    );
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

import type { components } from "./generated";
import { ApiTransport } from "./transport";
import type {
  WatchlistResponse,
  AuthHeaders,
  ProfileResponse,
  CommentListResponse,
  TokenWatchersResponse,
  CommentResponse,
  CommentEligibilityResponse,
  ReportReason,
  PublicProfileResponse,
  InboxResponse,
  TelegramStatus,
  TelegramLink,
  TelegramSettings,
  ForumPostsResponse,
  ForumCommunitiesResponse,
} from "./response-types";

/** Account-scoped and social endpoints: profile, comments, watchlist, notifications, forum, Telegram. */
export class AccountApi extends ApiTransport {
  getProfile(auth: AuthHeaders, signal?: AbortSignal) {
    return this.request<ProfileResponse>("/v1/profile", { signal }, auth);
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

  getTokenWatchers(address: string, signal?: AbortSignal) {
    return this.request<TokenWatchersResponse>(
      `/v1/tokens/${encodeURIComponent(address)}/watchers`,
      { signal },
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

  voteComment(id: string, value: -1 | 0 | 1, auth: AuthHeaders, signal?: AbortSignal) {
    return this.request<components["schemas"]["VoteBody"]>(
      `/v1/comments/${encodeURIComponent(id)}/vote`,
      {
        method: "PUT",
        signal,
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ value }),
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

  /** Visible comments across tokens, newest first (spec P0-C5). */
  /** `best` is one page of the last 7 days' posts by score; `new` (default) pages by cursor. */
  getForumPosts(
    query: { sort?: "new" | "best"; cursor?: string; limit?: number } = {},
    signal?: AbortSignal,
  ) {
    const params = new URLSearchParams();
    if (query.sort === "best") params.set("sort", "best");
    if (query.cursor) params.set("cursor", query.cursor);
    if (query.limit) params.set("limit", String(query.limit));
    const encoded = params.toString();
    return this.request<ForumPostsResponse>(`/v1/forum/posts${encoded ? `?${encoded}` : ""}`, {
      signal,
    });
  }

  /** Communities ranked by visible comments: `hot` (24 hours) or `top` (all time). */
  getForumCommunities(sort: "hot" | "top", limit?: number, signal?: AbortSignal) {
    const params = new URLSearchParams({ sort });
    if (limit) params.set("limit", String(limit));
    return this.request<ForumCommunitiesResponse>(`/v1/forum/communities?${params.toString()}`, {
      signal,
    });
  }

  /** Newest first; `cursor` is the `next_cursor` of the previous page (spec P0-C4). */
  getNotifications(
    auth: AuthHeaders,
    query: { cursor?: string; limit?: number } = {},
    signal?: AbortSignal,
  ) {
    const params = new URLSearchParams();
    if (query.cursor) params.set("cursor", query.cursor);
    if (query.limit) params.set("limit", String(query.limit));
    const encoded = params.toString();
    return this.request<InboxResponse>(
      `/v1/me/notifications${encoded ? `?${encoded}` : ""}`,
      { signal, cache: "no-store" },
      auth,
    );
  }

  /** Marks every notification up to and including `through` as read; never moves backwards. */
  markNotificationsRead(through: string, auth: AuthHeaders, signal?: AbortSignal) {
    return this.requestNoContent(
      "/v1/me/notifications/read",
      {
        method: "PUT",
        signal,
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ through }),
      },
      auth,
    );
  }

  /** Telegram alert link and settings (docs/specs/2026-11-12-telegram-alerts-design.md). */
  getTelegram(auth: AuthHeaders, signal?: AbortSignal) {
    return this.request<TelegramStatus>("/v1/me/telegram", { signal, cache: "no-store" }, auth);
  }

  updateTelegram(settings: TelegramSettings, auth: AuthHeaders, signal?: AbortSignal) {
    return this.request<TelegramStatus>(
      "/v1/me/telegram",
      {
        method: "PUT",
        signal,
        cache: "no-store",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(settings),
      },
      auth,
    );
  }

  unlinkTelegram(auth: AuthHeaders, signal?: AbortSignal) {
    return this.requestNoContent("/v1/me/telegram", { method: "DELETE", signal }, auth);
  }

  /** A one-time deep link; the chat connects when the reader presses Start in Telegram. */
  createTelegramLink(auth: AuthHeaders, signal?: AbortSignal) {
    return this.request<TelegramLink>(
      "/v1/me/telegram/link",
      { method: "POST", signal, cache: "no-store" },
      auth,
    );
  }
}

// API request and response types derived from the generated OpenAPI schema.
import type { components, paths } from "./generated";

export type ProtocolDailyResponse =
  paths["/stats/protocol/daily"]["get"]["responses"][200]["content"]["application/json"];
export type ProtocolDailyItem = components["schemas"]["ProtocolDailyDTO"];
export type ProtocolRevenueResponse = components["schemas"]["RevenueBody"];
export type TokenListResponse = components["schemas"]["TokenListBody"];
export type TokenDetailResponse = components["schemas"]["TokenDetailDTO"];
export type CandleResponse = components["schemas"]["CandleBody"];
export type TradesResponse = components["schemas"]["TradesBody"];
export type HoldersResponse = components["schemas"]["HoldersBody"];
export type EarlyBuyersResponse = components["schemas"]["EarlyBuyersBody"];
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
export type TokenWatchersResponse = components["schemas"]["WatchersBody"];
export type CommentResponse = components["schemas"]["CommentDTO"];
export type CommentEligibilityResponse = components["schemas"]["EligibilityBody"];
export type ReportReason = "spam" | "scam" | "abuse" | "other";
export type PublicProfileResponse = components["schemas"]["PublicProfileDTO"];
export type InboxResponse = components["schemas"]["InboxBody"];
export type TelegramStatus = components["schemas"]["TelegramStatusBody"];
export type TelegramLink = components["schemas"]["TelegramLinkBody"];
export type TelegramSettings = Pick<
  TelegramStatus,
  "graduations" | "large_trades" | "min_trade_wei"
>;
export type InboxItem = components["schemas"]["InboxItemDTO"];
export type ForumPostsResponse = components["schemas"]["ForumPostsBody"];
export type ForumPost = components["schemas"]["ForumPostDTO"];
export type ForumCommunitiesResponse = components["schemas"]["ForumCommunitiesBody"];
export type ForumCommunity = components["schemas"]["ForumCommunityDTO"];

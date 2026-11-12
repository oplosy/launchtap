export interface paths {
    "/comments/{id}/report": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put: operations["reportComment"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/events": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["events"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/forum/communities": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["listForumCommunities"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/forum/posts": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["listForumPosts"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/healthz": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["healthz"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/leaderboard": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["getLeaderboard"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/me/notifications": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["listNotifications"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/me/notifications/read": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put: operations["markNotificationsRead"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/me/public-profile": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["getPublicProfile"];
        put: operations["putPublicProfile"];
        post?: never;
        delete: operations["deletePublicProfile"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/me/watchlist": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["listWatchlist"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/me/watchlist/{address}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put: operations["addWatchlistEntry"];
        post?: never;
        delete: operations["removeWatchlistEntry"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/profile": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["getProfile"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/readyz": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["readyz"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/reference/eth-usd": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["getEthUsdReference"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/stats/protocol": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["getProtocolStats"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/stats/protocol/daily": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["listProtocolDaily"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/stats/protocol/revenue": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description ETH credited to the protocol, all-time, by source and engine (earned basis, wei). */
        get: operations["getProtocolRevenue"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/tokens": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["listTokens"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/tokens/highlights": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["getTokenHighlights"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/tokens/{address}/comments": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["listTokenComments"];
        put?: never;
        post: operations["postTokenComment"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/tokens/{address}/comments/eligibility": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["getCommentEligibility"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/tokens/{address}/watchers": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["getTokenWatchers"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/tokens/{token}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["getToken"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/tokens/{token}/candles": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["listCandles"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/tokens/{token}/early-buyers": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description Wallets that received tokens from the curve within the early window after launch, grouped by same-transaction deliveries and token transfers between them. */
        get: operations["getEarlyBuyers"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/tokens/{token}/holders": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["listHolders"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/tokens/{token}/image": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["getTokenImage"];
        put: operations["replaceTokenImage"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/tokens/{token}/metadata": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["getTokenMetadata"];
        put: operations["replaceTokenMetadata"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/tokens/{token}/quote": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        post: operations["quoteCurve"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/tokens/{token}/trades": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["listTrades"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/trades/recent": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["listRecentTrades"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/transactions/{tx_hash}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["getCanonicalTransaction"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/wallets/{address}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["getWallet"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/wallets/{address}/created": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["listWalletCreated"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/wallets/{address}/positions": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["listWalletPositions"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/wallets/{address}/trades": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["listWalletTrades"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
}
export type webhooks = Record<string, never>;
export interface components {
    schemas: {
        AntiSnipeDTO: {
            /** Format: int32 */
            duration_seconds: number;
            /** @description RFC 3339 time at which the anti-snipe tax reaches zero. */
            ends_at: string;
            /** Format: int32 */
            start_bps: number;
        };
        BurstItem: {
            /**
             * Format: int64
             * @description Trades of this token folded into the window.
             */
            count: number;
            /** @description Largest gross ETH amount among the folded trades, decimal wei string (same as a trade's eth_volume). "0" means none or unknown: no folded trade carried an amount. */
            max_eth_wei: string;
            token: string;
            /** @description Distinct non-null traders of the folded trades, lowercase hex, first-seen order, at most 32. Always an array, possibly empty. */
            traders: string[];
            /** @description True when more distinct traders were folded than traders lists. */
            traders_truncated: boolean;
        };
        CandleBody: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example /v1/schemas/CandleBody.json
             */
            readonly $schema?: string;
            items: components["schemas"]["CandleDTO"][] | null;
            next_cursor?: string;
            snapshot: components["schemas"]["SnapshotDTO"];
        };
        CandleDTO: {
            close: string;
            eth_volume: string;
            high: string;
            low: string;
            open: string;
            start: string;
            token_volume: string;
            /** Format: int64 */
            trade_count: number;
        };
        CanonicalEventDTO: {
            block_hash: string;
            /** Format: int64 */
            block_number: number;
            block_time: string;
            finality: string;
            kind: string;
            /** Format: int32 */
            log_index: number;
            pair?: string;
            token?: string;
            /** Format: int32 */
            transaction_index: number;
            tx_hash: string;
        };
        CanonicalObservationBody: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example /v1/schemas/CanonicalObservationBody.json
             */
            readonly $schema?: string;
            /** Format: int64 */
            chain_id: number;
            deployment_id: string;
            events: components["schemas"]["CanonicalEventDTO"][] | null;
            finality: string;
            snapshot: components["schemas"]["SnapshotDTO"];
            tx_hash: string;
        };
        CommentAuthorDTO: {
            /** @enum {string} */
            role: "creator" | "holder";
            /** @description Lowercase username, or null when the author has none or is not public. */
            username: string | null;
            /** @description Checksummed posting wallet, or null when the author shows a username only. */
            wallet: string | null;
        };
        CommentDTO: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example /v1/schemas/CommentDTO.json
             */
            readonly $schema?: string;
            author: components["schemas"]["CommentAuthorDTO"];
            body: string;
            created_at: string;
            /** @description Decimal string. */
            id: string;
            token: string;
        };
        CommentEvent: {
            /** Format: int64 */
            chain_id: number;
            deployment_id: string;
            token: string;
        };
        CommentListBody: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example /v1/schemas/CommentListBody.json
             */
            readonly $schema?: string;
            /** Format: int64 */
            comment_count: number;
            items: components["schemas"]["CommentDTO"][] | null;
            next_cursor?: string;
        };
        EarlyBuyersBody: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example /v1/schemas/EarlyBuyersBody.json
             */
            readonly $schema?: string;
            /** Format: int64 */
            cluster_count: number;
            /** @description At most 10 clusters, largest combined balance first. */
            clusters: components["schemas"]["EarlyClusterDTO"][] | null;
            /** @description What every early wallet holds now. */
            early_balance: string;
            /** Format: int64 */
            early_wallet_count: number;
            snapshot: components["schemas"]["SnapshotDTO"];
            /** @description The early window had not ended at the snapshot block. */
            window_open: boolean;
            /** Format: int64 */
            window_seconds: number;
        };
        EarlyClusterDTO: {
            /** @description What every wallet of the cluster holds now. */
            balance: string;
            includes_creator: boolean;
            /** @description Link kinds present: same_transaction, token_transfer. */
            links: string[] | null;
            /** Format: int64 */
            wallet_count: number;
            /** @description At most 20 wallets, largest balance first. */
            wallets: components["schemas"]["EarlyWalletDTO"][] | null;
        };
        EarlyWalletDTO: {
            address: string;
            balance: string;
        };
        EligibilityBody: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example /v1/schemas/EligibilityBody.json
             */
            readonly $schema?: string;
            banned: boolean;
            eligible_wallets: components["schemas"]["EligibleWalletDTO"][] | null;
            username: string | null;
        };
        EligibleWalletDTO: {
            address: string;
            /** @enum {string} */
            role: "creator" | "holder";
        };
        ErrorDetail: {
            /** @description Where the error occurred, e.g. 'body.items[3].tags' or 'path.thing-id' */
            location?: string;
            /** @description Error message text */
            message?: string;
            /** @description The value at the given location */
            value?: unknown;
        };
        ErrorModel: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example /v1/schemas/ErrorModel.json
             */
            readonly $schema?: string;
            /**
             * @description A human-readable explanation specific to this occurrence of the problem.
             * @example Property foo is required but is missing.
             */
            detail?: string;
            /** @description Optional list of individual error details */
            errors?: components["schemas"]["ErrorDetail"][] | null;
            /**
             * Format: uri
             * @description A URI reference that identifies the specific occurrence of the problem.
             * @example https://example.com/error-log/abc123
             */
            instance?: string;
            /**
             * Format: int64
             * @description HTTP status code
             * @example 400
             */
            status?: number;
            /**
             * @description A short, human-readable summary of the problem type. This value should not change between occurrences of the error.
             * @example Bad Request
             */
            title?: string;
            /**
             * Format: uri
             * @description A URI reference to human-readable documentation for the error.
             * @default about:blank
             * @example https://example.com/errors/example
             */
            type: string;
        };
        EthUsdBody: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example /v1/schemas/EthUsdBody.json
             */
            readonly $schema?: string;
            attribution_url: string | null;
            /** Format: date-time */
            fetched_at: string | null;
            /** @description ETH/USD decimal string; null when disabled, missing or stale. */
            price: string | null;
            /** Format: date-time */
            provider_updated_at: string | null;
            source: string | null;
        };
        FinalityFeedEvent: {
            /** Format: int64 */
            chain_id: number;
            deployment_id: string;
            /** Format: int64 */
            finalized_block: number;
            /** Format: int64 */
            safe_block: number;
        };
        ForumCommunitiesBody: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example /v1/schemas/ForumCommunitiesBody.json
             */
            readonly $schema?: string;
            items: components["schemas"]["ForumCommunityDTO"][] | null;
        };
        ForumCommunityDTO: {
            /**
             * Format: int64
             * @description Visible comments in the sort's window.
             */
            comment_count: number;
            last_comment_at: string;
            name: string;
            symbol: string;
            token: string;
        };
        ForumPostDTO: {
            author: components["schemas"]["CommentAuthorDTO"];
            body: string;
            created_at: string;
            /** @description Decimal string. */
            id: string;
            /** @description Token launch name; user-controlled text. */
            name: string;
            /** @description Token launch symbol; user-controlled text. */
            symbol: string;
            token: string;
        };
        ForumPostsBody: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example /v1/schemas/ForumPostsBody.json
             */
            readonly $schema?: string;
            items: components["schemas"]["ForumPostDTO"][] | null;
            next_cursor?: string;
        };
        GraduationFeedEvent: {
            /** Format: int64 */
            block_number: number;
            /** Format: int64 */
            chain_id: number;
            deployment_id: string;
            name: string;
            pair: string;
            symbol: string;
            token: string;
            tx_hash: string;
        };
        HealthResponse: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example /v1/schemas/HealthResponse.json
             */
            readonly $schema?: string;
            status: string;
        };
        HighlightsBody: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example /v1/schemas/HighlightsBody.json
             */
            readonly $schema?: string;
            as_of: string;
            finality: string;
            king_of_the_hill: components["schemas"]["KingDTO"] | null;
            snapshot: components["schemas"]["SnapshotDTO"];
            trending: components["schemas"]["TrendingDTO"][] | null;
            window_start: string;
        };
        HolderDTO: {
            address: string;
            balance: string;
            /** Format: int64 */
            first_acquired_block: number;
        };
        HoldersBody: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example /v1/schemas/HoldersBody.json
             */
            readonly $schema?: string;
            items: components["schemas"]["HolderDTO"][] | null;
            next_cursor?: string;
            snapshot: components["schemas"]["SnapshotDTO"];
        };
        InboxBody: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example /v1/schemas/InboxBody.json
             */
            readonly $schema?: string;
            items: components["schemas"]["InboxItemDTO"][] | null;
            next_cursor?: string;
            /**
             * Format: int64
             * @description Unread notifications, at most 100.
             */
            unread_count: number;
        };
        InboxItemDTO: {
            block_time: string;
            /** @description Gross trade amount, or the ETH moved to the pool at graduation. */
            eth_wei: string;
            /** @description <block>-<tx_index>-<log_index>; also usable as a cursor and a read position. */
            id: string;
            /** @enum {string} */
            kind: "graduation" | "large_trade";
            name: string;
            /** @description large_trade only. */
            side_buy: boolean | null;
            /**
             * @description large_trade only.
             * @enum {string|null}
             */
            source: "curve" | "dex" | null;
            symbol: string;
            token: string;
            /** @description large_trade only; null when the pool-swap sender is unknown. */
            trader: string | null;
            tx_hash: string;
        };
        InboxReadInputBody: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example /v1/schemas/InboxReadInputBody.json
             */
            readonly $schema?: string;
            /** @description Mark every notification up to and including this id as read. */
            through: string;
        };
        KingDTO: {
            address: string;
            /** Format: int32 */
            graduation_progress_bps: number;
            /** Format: int64 */
            holder_count: number;
            /** Format: int64 */
            launch_block: number;
            launch_time: string;
            market_cap_eth: string;
            name: string;
            phase: string;
            remaining_eth: string;
            symbol: string;
            total_supply: string;
            volume_24h_eth: string;
        };
        LaunchEvent: {
            /** Format: int64 */
            as_of_block?: number;
            as_of_block_hash?: string;
            /** Format: int64 */
            chain_id: number;
            deployment_id: string;
            token: string;
        };
        LeaderboardBody: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example /v1/schemas/LeaderboardBody.json
             */
            readonly $schema?: string;
            items: components["schemas"]["LeaderboardItemDTO"][] | null;
            snapshot: components["schemas"]["SnapshotDTO"];
        };
        LeaderboardItemDTO: {
            address: string;
            /** Format: int64 */
            rank: number;
            realized_pnl_wei: string;
            /** Format: int64 */
            tokens_traded: number;
            /** @description Username published on this wallet, or null. */
            username: string | null;
        };
        LockerOwedDTO: {
            account: string;
            eth: string;
            token: string;
        };
        MetadataBody: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example /v1/schemas/MetadataBody.json
             */
            readonly $schema?: string;
            description?: string;
            telegram_url?: string;
            x_url?: string;
        };
        MetadataReadBody: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example /v1/schemas/MetadataReadBody.json
             */
            readonly $schema?: string;
            description?: string;
            image_url?: string;
            /** Format: int64 */
            revision: number;
            telegram_url?: string;
            x_url?: string;
        };
        NewTokenFeedEvent: {
            /** Format: int64 */
            block_number: number;
            /** Format: int64 */
            chain_id: number;
            creator: string;
            deployment_id: string;
            name: string;
            symbol: string;
            token: string;
            tx_hash: string;
        };
        PostCommentInputBody: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example /v1/schemas/PostCommentInputBody.json
             */
            readonly $schema?: string;
            /** @description Comment text, at most 500 code points after normalization. */
            body: string;
            /** @description Linked wallet that posts; must be the token creator or a holder. */
            wallet: string;
        };
        ProfileActionDTO: {
            creator_fees: string;
            curve: string;
            name: string;
            phase: string;
            refund: string;
            symbol: string;
            token: string;
        };
        ProfileBody: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example /v1/schemas/ProfileBody.json
             */
            readonly $schema?: string;
            items: components["schemas"]["ProfileActionDTO"][] | null;
            snapshot: components["schemas"]["SnapshotDTO"];
        };
        ProtocolDTO: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example /v1/schemas/ProtocolDTO.json
             */
            readonly $schema?: string;
            /** Format: int64 */
            graduations_24h: number;
            /** Format: int64 */
            graduations_all_time: number;
            /** Format: int64 */
            launches_24h: number;
            /** Format: int64 */
            launches_all_time: number;
            snapshot: components["schemas"]["SnapshotDTO"];
            /** Format: int64 */
            trades_24h: number;
            /** Format: int64 */
            trades_all_time: number;
            updated_at: string;
            volume_24h_eth: string;
            volume_all_time_eth: string;
        };
        ProtocolDailyBody: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example /v1/schemas/ProtocolDailyBody.json
             */
            readonly $schema?: string;
            items: components["schemas"]["ProtocolDailyDTO"][] | null;
            snapshot: components["schemas"]["SnapshotDTO"];
        };
        ProtocolDailyDTO: {
            day: string;
            /** Format: int64 */
            graduations: number;
            /** Format: int64 */
            launches: number;
            /** Format: int64 */
            trades: number;
            volume_eth: string;
        };
        PublicProfileDTO: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example /v1/schemas/PublicProfileDTO.json
             */
            readonly $schema?: string;
            banned: boolean;
            /** @description Null when a username change is allowed now. */
            next_username_change_at: string | null;
            /** @description Checksummed wallets the username is published on. */
            public_wallets: string[] | null;
            username: string | null;
            username_changed_at: string | null;
        };
        PutPublicProfileInputBody: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example /v1/schemas/PutPublicProfileInputBody.json
             */
            readonly $schema?: string;
            /** @description Linked wallets to publish the username on, at most 5. */
            public_wallets: string[] | null;
            /** @description New username; omit to keep the current one. */
            username?: string;
        };
        QuoteBody: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example /v1/schemas/QuoteBody.json
             */
            readonly $schema?: string;
            /** Format: int64 */
            as_of_block: number;
            creator_fee: string;
            finality: string;
            graduates: boolean;
            informational: boolean;
            input: string;
            next_virtual_eth: string;
            next_virtual_token: string;
            output: string;
            protocol_fee: string;
            refund: string;
            /** Format: int64 */
            reserve_source_block: number;
            reserve_source_hash: string;
            /** @description Engine-2 anti-snipe tax taken from a buy at the indexed head's block time; 0 for engine 1, sells and after the window. */
            snipe_tax: string;
            /**
             * Format: int32
             * @description Engine-2 anti-snipe rate in force at the indexed head's block time; 0 for engine 1 and after the window.
             */
            tax_bps: number;
            /** @description RFC 3339 time at which the anti-snipe tax reaches zero; empty when no tax is in force. */
            tax_ends_at: string;
        };
        QuoteInputBody: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example /v1/schemas/QuoteInputBody.json
             */
            readonly $schema?: string;
            amount: string;
            /** @enum {string} */
            side: "buy" | "sell";
        };
        RecentTradesBody: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example /v1/schemas/RecentTradesBody.json
             */
            readonly $schema?: string;
            items: components["schemas"]["TradeFeedItem"][] | null;
            snapshot: components["schemas"]["SnapshotDTO"];
        };
        ReorgEvent: {
            /** Format: int64 */
            as_of_block?: number;
            as_of_block_hash?: string;
            /** Format: int64 */
            chain_id: number;
            /** Format: int64 */
            common_ancestor: number;
            deployment_id: string;
        };
        ReportCommentInputBody: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example /v1/schemas/ReportCommentInputBody.json
             */
            readonly $schema?: string;
            /** @description One of spam, scam, abuse, other. */
            reason: string;
        };
        RevenueBody: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example /v1/schemas/RevenueBody.json
             */
            readonly $schema?: string;
            snapshot: components["schemas"]["SnapshotDTO"];
            sources: components["schemas"]["RevenueSourcesDTO"];
            total: string;
        };
        RevenueSourceDTO: {
            engine_1: string;
            engine_2: string;
            total: string;
        };
        RevenueSourcesDTO: {
            graduation_remainder: components["schemas"]["RevenueSourceDTO"];
            launch_fees: components["schemas"]["RevenueSourceDTO"];
            lp_fees: components["schemas"]["RevenueSourceDTO"];
            snipe_tax: components["schemas"]["RevenueSourceDTO"];
            trade_fees: components["schemas"]["RevenueSourceDTO"];
        };
        RevisionBody: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example /v1/schemas/RevisionBody.json
             */
            readonly $schema?: string;
            image_url?: string;
            /** Format: int64 */
            revision: number;
        };
        SnapshotDTO: {
            /** Format: int64 */
            as_of_block: number;
            as_of_block_hash: string;
            /** Format: int64 */
            chain_id: number;
            finality: string;
        };
        TokenDTO: {
            address: string;
            /** Format: int64 */
            holder_count: number;
            /** Format: int64 */
            launch_block: number;
            launch_time: string;
            market_cap_eth: string;
            name: string;
            phase: string;
            symbol: string;
            total_supply: string;
            volume_24h_eth: string;
        };
        TokenDetailDTO: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example /v1/schemas/TokenDetailDTO.json
             */
            readonly $schema?: string;
            address: string;
            ath_at: string;
            ath_price_eth: string;
            creator: string;
            curve: string;
            curve_tokens: string;
            description: string;
            /** Format: int32 */
            engine_version: number;
            eth_reserve: string;
            fdv_eth: string;
            graduation_eth: string;
            /** Format: int32 */
            graduation_progress_bps: number;
            /** Format: int64 */
            holder_count: number;
            image_url: string;
            initial_virtual_eth: string;
            initial_virtual_token: string;
            /**
             * Format: int64
             * @description Block of the launch transaction; holders first acquiring in it were bundled with the launch.
             */
            launch_block: number;
            launch_time: string;
            liquidity_eth: string;
            lp_tokens: string;
            market_cap_eth: string;
            name: string;
            pair: string;
            phase: string;
            /** Format: int64 */
            price_change_24h_bps: number;
            /** Format: int32 */
            protocol_share_bps: number;
            protocol_treasury: string;
            real_curve_eth: string;
            /** Format: int64 */
            reserve_block: number;
            reserve_hash: string;
            reserve_source: string;
            snapshot: components["schemas"]["SnapshotDTO"];
            spot_price_eth: string;
            symbol: string;
            telegram_url: string;
            token_reserve: string;
            total_supply: string;
            /** Format: int32 */
            trade_fee_bps: number;
            /** @description Engine-2 launch, pool and fee facts; null for engine 1. */
            v2: components["schemas"]["TokenV2DTO"];
            volume_24h_eth: string;
            weth: string;
            x_url: string;
        };
        TokenEvent: {
            /** Format: int64 */
            as_of_block?: number;
            as_of_block_hash?: string;
            /** Format: int64 */
            chain_id: number;
            deployment_id: string;
            token?: string;
        };
        TokenListBody: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example /v1/schemas/TokenListBody.json
             */
            readonly $schema?: string;
            items: components["schemas"]["TokenDTO"][] | null;
            next_cursor?: string;
            snapshot: components["schemas"]["SnapshotDTO"];
        };
        TokenV2DTO: {
            anti_snipe: components["schemas"]["AntiSnipeDTO"];
            fee_recipient: string;
            /** @description What the LP fee locker owes each account, by account. */
            locker_owed: components["schemas"]["LockerOwedDTO"][] | null;
            /** Format: int32 */
            lp_creator_share_bps: number;
            pending_fee_recipient: string | null;
            /** @description The graduated Uniswap V4 pool; null before graduation. */
            pool: components["schemas"]["V2PoolDTO"];
        } | null;
        TradeBurstFeedEvent: {
            /** Format: int64 */
            chain_id: number;
            deployment_id: string;
            items: components["schemas"]["BurstItem"][] | null;
            /**
             * Format: int64
             * @description new_token events folded into this window.
             */
            launches?: number;
            window_end: string;
            window_start: string;
        };
        TradeDTO: {
            /** Format: int64 */
            block_number: number;
            eth_volume: string;
            execution_price: string;
            finality: string;
            /** Format: int32 */
            log_index: number;
            side: string;
            source: string;
            spot_price: string;
            time: string;
            token_volume: string;
            trader: string | null;
            /** Format: int32 */
            transaction_index: number;
            tx_hash: string;
        };
        TradeFeedItem: {
            /** Format: int64 */
            block_number: number;
            /** Format: int64 */
            chain_id: number;
            deployment_id: string;
            eth_volume: string;
            finality: string;
            /** Format: int32 */
            log_index: number;
            name: string;
            side: string;
            source: string;
            symbol: string;
            time: string;
            token: string;
            token_volume: string;
            trader: string | null;
            /** Format: int32 */
            transaction_index: number;
            tx_hash: string;
        };
        TradesBody: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example /v1/schemas/TradesBody.json
             */
            readonly $schema?: string;
            items: components["schemas"]["TradeDTO"][] | null;
            next_cursor?: string;
            snapshot: components["schemas"]["SnapshotDTO"];
        };
        TrendingDTO: {
            address: string;
            /** Format: int64 */
            distinct_buyers: number;
            /** Format: int64 */
            holder_count: number;
            /** Format: int64 */
            launch_block: number;
            launch_time: string;
            market_cap_eth: string;
            name: string;
            phase: string;
            symbol: string;
            total_supply: string;
            volume_24h_eth: string;
            window_volume_eth: string;
        };
        V2PoolDTO: {
            hook: string;
            pool_id: string;
            position_token_id: string;
        } | null;
        WalletCreatedBody: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example /v1/schemas/WalletCreatedBody.json
             */
            readonly $schema?: string;
            items: components["schemas"]["WalletCreatedDTO"][] | null;
            next_cursor?: string;
            snapshot: components["schemas"]["SnapshotDTO"];
        };
        WalletCreatedDTO: {
            /** Format: int64 */
            launch_block: number;
            market_cap_wei: string;
            name: string;
            phase: string;
            symbol: string;
            token: string;
        };
        WalletDTO: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example /v1/schemas/WalletDTO.json
             */
            readonly $schema?: string;
            address: string;
            attribution_complete: boolean;
            /** Format: int64 */
            created_count: number;
            /** Format: int64 */
            first_block: number | null;
            /** Format: int64 */
            held_count: number;
            /** Format: int64 */
            last_block: number | null;
            portfolio_value_wei: string;
            realized_pnl_wei: string;
            snapshot: components["schemas"]["SnapshotDTO"];
            /** Format: int64 */
            tokens_traded: number;
            unrealized_pnl_wei: string;
            /** @description Username published on this wallet, or null. */
            username: string | null;
        };
        WalletPositionDTO: {
            average_cost_wad: string | null;
            balance: string;
            current_value_wei: string;
            /** Format: int64 */
            last_block: number;
            name: string;
            phase: string;
            realized_pnl_wei: string;
            remaining_cost_wei: string;
            symbol: string;
            token: string;
            /** Format: int64 */
            trade_count: number;
            transferred_in: string;
            unrealized_pnl_wei: string;
        };
        WalletPositionsBody: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example /v1/schemas/WalletPositionsBody.json
             */
            readonly $schema?: string;
            items: components["schemas"]["WalletPositionDTO"][] | null;
            next_cursor?: string;
            snapshot: components["schemas"]["SnapshotDTO"];
            /** @description Open mode only: true when the wallet has more open positions than the 500 returned. */
            truncated: boolean;
        };
        WalletTradeDTO: {
            /** Format: int64 */
            block_number: number;
            eth_volume: string;
            execution_price: string;
            finality: string;
            /** Format: int32 */
            log_index: number;
            name: string;
            side: string;
            source: string;
            spot_price: string;
            symbol: string;
            time: string;
            token: string;
            token_volume: string;
            trader: string | null;
            /** Format: int32 */
            transaction_index: number;
            tx_hash: string;
        };
        WalletTradesBody: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example /v1/schemas/WalletTradesBody.json
             */
            readonly $schema?: string;
            items: components["schemas"]["WalletTradeDTO"][] | null;
            next_cursor?: string;
            snapshot: components["schemas"]["SnapshotDTO"];
        };
        WatchersBody: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example /v1/schemas/WatchersBody.json
             */
            readonly $schema?: string;
            /**
             * Format: int64
             * @description Number of signed-in users watching this token in this deployment.
             */
            watch_count: number;
        };
        WatchlistBody: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example /v1/schemas/WatchlistBody.json
             */
            readonly $schema?: string;
            items: components["schemas"]["WatchlistEntryDTO"][] | null;
        };
        WatchlistEntryDTO: {
            added_at: string;
            address: string;
            token: components["schemas"]["TokenDTO"] | null;
        };
    };
    responses: never;
    parameters: never;
    requestBodies: never;
    headers: never;
    pathItems: never;
}
export type $defs = Record<string, never>;
export interface operations {
    reportComment: {
        parameters: {
            query?: never;
            header?: {
                Authorization?: string;
                "privy-id-token"?: string;
            };
            path: {
                id: number;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["ReportCommentInputBody"];
            };
        };
        responses: {
            /** @description No Content */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    events: {
        parameters: {
            query?: {
                /** @description Forward only token hints for this address, plus reorgs. */
                token?: string;
            };
            header?: {
                "Last-Event-ID"?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "text/event-stream": ({
                        data: components["schemas"]["CommentEvent"];
                        /**
                         * @description The event name.
                         * @constant
                         */
                        event: "comment";
                        /** @description The event ID. */
                        id?: number;
                        /** @description The retry time in milliseconds. */
                        retry?: number;
                    } | {
                        data: components["schemas"]["FinalityFeedEvent"];
                        /**
                         * @description The event name.
                         * @constant
                         */
                        event: "finality";
                        /** @description The event ID. */
                        id?: number;
                        /** @description The retry time in milliseconds. */
                        retry?: number;
                    } | {
                        data: components["schemas"]["GraduationFeedEvent"];
                        /**
                         * @description The event name.
                         * @constant
                         */
                        event: "graduation";
                        /** @description The event ID. */
                        id?: number;
                        /** @description The retry time in milliseconds. */
                        retry?: number;
                    } | {
                        data: components["schemas"]["LaunchEvent"];
                        /**
                         * @description The event name.
                         * @constant
                         */
                        event: "launch";
                        /** @description The event ID. */
                        id?: number;
                        /** @description The retry time in milliseconds. */
                        retry?: number;
                    } | {
                        data: components["schemas"]["NewTokenFeedEvent"];
                        /**
                         * @description The event name.
                         * @constant
                         */
                        event: "new_token";
                        /** @description The event ID. */
                        id?: number;
                        /** @description The retry time in milliseconds. */
                        retry?: number;
                    } | {
                        data: components["schemas"]["ReorgEvent"];
                        /**
                         * @description The event name.
                         * @constant
                         */
                        event: "reorg";
                        /** @description The event ID. */
                        id?: number;
                        /** @description The retry time in milliseconds. */
                        retry?: number;
                    } | {
                        data: components["schemas"]["TokenEvent"];
                        /**
                         * @description The event name.
                         * @constant
                         */
                        event: "token";
                        /** @description The event ID. */
                        id?: number;
                        /** @description The retry time in milliseconds. */
                        retry?: number;
                    } | {
                        data: components["schemas"]["TradeFeedItem"];
                        /**
                         * @description The event name.
                         * @constant
                         */
                        event: "trade";
                        /** @description The event ID. */
                        id?: number;
                        /** @description The retry time in milliseconds. */
                        retry?: number;
                    } | {
                        data: components["schemas"]["TradeBurstFeedEvent"];
                        /**
                         * @description The event name.
                         * @constant
                         */
                        event: "trade_burst";
                        /** @description The event ID. */
                        id?: number;
                        /** @description The retry time in milliseconds. */
                        retry?: number;
                    })[];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    listForumCommunities: {
        parameters: {
            query?: {
                /** @description hot (default): visible comments in the last 24 hours; top: all time. Any other value is a 400 invalid_sort. */
                sort?: string;
                /** @description Number of communities, 1-50; defaults to 20. */
                limit?: number;
            };
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    "Cache-Control"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ForumCommunitiesBody"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    listForumPosts: {
        parameters: {
            query?: {
                cursor?: string;
                /** @description Number of posts, 1-50; defaults to 20. */
                limit?: number;
            };
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    "Cache-Control"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ForumPostsBody"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    healthz: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["HealthResponse"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    getLeaderboard: {
        parameters: {
            query?: {
                /** @description Ranking period: all (default, all-time realized PnL), 7d or 30d (realized PnL booked in the last 7 or 30 days). Any other value is a 400 invalid_window. */
                window?: string;
                /** @description Maximum number of wallets, 1-100. Only wallets with a positive realized PnL are ranked, so fewer may be returned. */
                limit?: number;
            };
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["LeaderboardBody"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    listNotifications: {
        parameters: {
            query?: {
                /** @description next_cursor of the previous page. */
                cursor?: string;
                /** @description Number of notifications, 1-50; defaults to 20. */
                limit?: number;
            };
            header?: {
                Authorization?: string;
                "privy-id-token"?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["InboxBody"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    markNotificationsRead: {
        parameters: {
            query?: never;
            header?: {
                Authorization?: string;
                "privy-id-token"?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["InboxReadInputBody"];
            };
        };
        responses: {
            /** @description No Content */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    getPublicProfile: {
        parameters: {
            query?: never;
            header?: {
                Authorization?: string;
                "privy-id-token"?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["PublicProfileDTO"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    putPublicProfile: {
        parameters: {
            query?: never;
            header?: {
                Authorization?: string;
                "privy-id-token"?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["PutPublicProfileInputBody"];
            };
        };
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["PublicProfileDTO"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    deletePublicProfile: {
        parameters: {
            query?: never;
            header?: {
                Authorization?: string;
                "privy-id-token"?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description No Content */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    listWatchlist: {
        parameters: {
            query?: never;
            header?: {
                Authorization?: string;
                "privy-id-token"?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["WatchlistBody"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    addWatchlistEntry: {
        parameters: {
            query?: never;
            header?: {
                Authorization?: string;
                "privy-id-token"?: string;
            };
            path: {
                address: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description No Content */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    removeWatchlistEntry: {
        parameters: {
            query?: never;
            header?: {
                Authorization?: string;
                "privy-id-token"?: string;
            };
            path: {
                address: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description No Content */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    getProfile: {
        parameters: {
            query?: never;
            header: {
                Authorization: string;
                "privy-id-token": string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ProfileBody"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    readyz: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["HealthResponse"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    getEthUsdReference: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    "Cache-Control"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["EthUsdBody"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    getProtocolStats: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ProtocolDTO"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    listProtocolDaily: {
        parameters: {
            query?: {
                from?: string;
                to?: string;
                limit?: number;
            };
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ProtocolDailyBody"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    getProtocolRevenue: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["RevenueBody"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    listTokens: {
        parameters: {
            query?: {
                phase?: string;
                q?: string;
                sort?: string;
                age?: string;
                cursor?: string;
                limit?: number;
            };
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["TokenListBody"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    getTokenHighlights: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["HighlightsBody"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    listTokenComments: {
        parameters: {
            query?: {
                cursor?: string;
                /** @description Number of comments, 1-50; defaults to 20. */
                limit?: number;
            };
            header?: never;
            path: {
                address: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    "Cache-Control"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["CommentListBody"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    postTokenComment: {
        parameters: {
            query?: never;
            header?: {
                Authorization?: string;
                "privy-id-token"?: string;
            };
            path: {
                address: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["PostCommentInputBody"];
            };
        };
        responses: {
            /** @description Created */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["CommentDTO"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    getCommentEligibility: {
        parameters: {
            query?: never;
            header?: {
                Authorization?: string;
                "privy-id-token"?: string;
            };
            path: {
                address: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["EligibilityBody"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    getTokenWatchers: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                address: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    "Cache-Control"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["WatchersBody"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    getToken: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                token: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["TokenDetailDTO"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    listCandles: {
        parameters: {
            query?: {
                interval?: string;
                from?: string;
                to?: string;
                limit?: number;
                cursor?: string;
            };
            header?: never;
            path: {
                token: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["CandleBody"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    getEarlyBuyers: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                token: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["EarlyBuyersBody"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    listHolders: {
        parameters: {
            query?: {
                cursor?: string;
                limit?: number;
            };
            header?: never;
            path: {
                token: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["HoldersBody"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    getTokenImage: {
        parameters: {
            query?: never;
            header?: {
                "If-None-Match"?: string;
            };
            path: {
                token: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Token image */
            200: {
                headers: {
                    "Cache-Control"?: string;
                    "Content-Length"?: number;
                    "Content-Type"?: string;
                    ETag?: string;
                    "X-Content-Type-Options"?: string;
                    "X-Revision"?: number;
                    [name: string]: unknown;
                };
                content: {
                    "image/jpeg": string;
                    "image/png": string;
                    "image/webp": string;
                };
            };
            /** @description Not modified when If-None-Match matches; the response has no body. */
            304: {
                headers: {
                    /** @description Image cache policy. */
                    "Cache-Control"?: string;
                    /** @description Image content hash validator. */
                    ETag?: string;
                    /** @description Image response security policy. */
                    "X-Content-Type-Options"?: string;
                    /** @description Image revision. */
                    "X-Revision"?: number;
                    [name: string]: unknown;
                };
                content?: never;
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    replaceTokenImage: {
        parameters: {
            query?: never;
            header: {
                Authorization: string;
                "privy-id-token": string;
                "If-Match": string;
                "Content-Type": string;
            };
            path: {
                token: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "image/jpeg": string;
                "image/png": string;
                "image/webp": string;
            };
        };
        responses: {
            /** @description OK */
            200: {
                headers: {
                    ETag?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["RevisionBody"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    getTokenMetadata: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                token: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    ETag?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["MetadataReadBody"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    replaceTokenMetadata: {
        parameters: {
            query?: never;
            header: {
                Authorization: string;
                "privy-id-token": string;
                "If-Match": string;
            };
            path: {
                token: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["MetadataBody"];
            };
        };
        responses: {
            /** @description OK */
            200: {
                headers: {
                    ETag?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["RevisionBody"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    quoteCurve: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                token: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["QuoteInputBody"];
            };
        };
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["QuoteBody"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    listTrades: {
        parameters: {
            query?: {
                cursor?: string;
                limit?: number;
            };
            header?: never;
            path: {
                token: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["TradesBody"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    listRecentTrades: {
        parameters: {
            query?: {
                /** @description Number of trades, 1-100; defaults to 50. */
                limit?: number;
            };
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["RecentTradesBody"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    getCanonicalTransaction: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                tx_hash: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["CanonicalObservationBody"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    getWallet: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                address: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["WalletDTO"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    listWalletCreated: {
        parameters: {
            query?: {
                cursor?: string;
                /** @description Page size, 1-100. */
                limit?: number;
            };
            header?: never;
            path: {
                address: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["WalletCreatedBody"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    listWalletPositions: {
        parameters: {
            query?: {
                /** @description Closed mode only; supplying it with closed=false is rejected with 422. */
                cursor?: string;
                /** @description List closed positions (balance 0) newest first, paginated. When false (default), all open positions are returned in one response ordered by current value, capped at 500 (see truncated). */
                closed?: boolean;
                /** @description Page size, 1-100. Applies to closed mode only; open mode returns one capped page. */
                limit?: number;
            };
            header?: never;
            path: {
                address: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["WalletPositionsBody"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    listWalletTrades: {
        parameters: {
            query?: {
                cursor?: string;
                /** @description Page size, 1-100. */
                limit?: number;
            };
            header?: never;
            path: {
                address: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["WalletTradesBody"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
}

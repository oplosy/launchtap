export interface paths {
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
}
export type webhooks = Record<string, never>;
export interface components {
    schemas: {
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
        HealthResponse: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example /v1/schemas/HealthResponse.json
             */
            readonly $schema?: string;
            status: string;
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
        LaunchEvent: {
            /** Format: int64 */
            as_of_block?: number;
            as_of_block_hash?: string;
            /** Format: int64 */
            chain_id: number;
            deployment_id: string;
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
    };
    responses: never;
    parameters: never;
    requestBodies: never;
    headers: never;
    pathItems: never;
}
export type $defs = Record<string, never>;
export interface operations {
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
    listTokens: {
        parameters: {
            query?: {
                phase?: string;
                q?: string;
                sort?: string;
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
}

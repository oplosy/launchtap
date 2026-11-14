-- name: ListTokenCardsNewest :many
SELECT t.token_address, t.name, t.symbol, t.phase,
       t.launch_block_number, t.launch_block_time, t.total_supply,
       COALESCE(s.market_cap_eth_wad, 0::numeric) AS market_cap_eth_wad,
       COALESCE(s.volume_24h_eth_wad, 0::numeric) AS volume_24h_eth_wad,
       COALESCE(s.holder_count, 0)::BIGINT AS holder_count,
       m.description, m.image_url, m.x_url, m.telegram_url,
       t.launch_block_hash
FROM tokens AS t
LEFT JOIN token_metadata AS m
  ON m.chain_id = t.chain_id AND m.token_address = t.token_address
 AND m.launch_tx_hash = t.launch_tx_hash AND m.launch_log_index = t.launch_log_index
LEFT JOIN token_stats AS s
  ON s.chain_id = t.chain_id AND s.token_address = t.token_address
WHERE t.chain_id = sqlc.arg(chain_id)
  AND t.phase = sqlc.arg(phase)
  AND (sqlc.arg(search)::text = '' OR lower(t.name) LIKE lower(sqlc.arg(search)::text) || '%'
       OR lower(t.symbol) LIKE lower(sqlc.arg(search)::text) || '%'
       OR encode(t.token_address, 'hex') = lower(CASE WHEN left(sqlc.arg(search)::text,2)='0x' THEN substr(sqlc.arg(search)::text,3) ELSE sqlc.arg(search)::text END))
  AND (sqlc.narg(after_block)::bigint IS NULL
       OR (t.launch_block_number, t.token_address) < (sqlc.narg(after_block)::bigint, sqlc.narg(after_address)::bytea))
  AND (sqlc.narg(launched_after)::timestamptz IS NULL OR t.launch_block_time >= sqlc.narg(launched_after)::timestamptz)
ORDER BY t.launch_block_number DESC, t.token_address DESC
LIMIT sqlc.arg(page_size)::integer;

-- name: ListTokenCardsOldest :many
SELECT t.token_address, t.name, t.symbol, t.phase, t.launch_block_number, t.launch_block_time, t.total_supply,
       COALESCE(s.market_cap_eth_wad, 0::numeric) AS market_cap_eth_wad, COALESCE(s.volume_24h_eth_wad, 0::numeric) AS volume_24h_eth_wad,
       COALESCE(s.holder_count, 0)::BIGINT AS holder_count, m.description, m.image_url, m.x_url, m.telegram_url, t.launch_block_hash
FROM tokens AS t
LEFT JOIN token_metadata AS m ON m.chain_id = t.chain_id AND m.token_address = t.token_address
    AND m.launch_tx_hash = t.launch_tx_hash AND m.launch_log_index = t.launch_log_index
LEFT JOIN token_stats AS s ON s.chain_id = t.chain_id AND s.token_address = t.token_address
WHERE t.chain_id = sqlc.arg(chain_id) AND t.phase = sqlc.arg(phase)
  AND (sqlc.arg(search)::text = '' OR lower(t.name) LIKE lower(sqlc.arg(search)::text) || '%' OR lower(t.symbol) LIKE lower(sqlc.arg(search)::text) || '%' OR encode(t.token_address, 'hex') = lower(CASE WHEN left(sqlc.arg(search)::text,2)='0x' THEN substr(sqlc.arg(search)::text,3) ELSE sqlc.arg(search)::text END))
  AND (sqlc.narg(after_block)::bigint IS NULL OR (t.launch_block_number, t.token_address) > (sqlc.narg(after_block)::bigint, sqlc.narg(after_address)::bytea))
  AND (sqlc.narg(launched_after)::timestamptz IS NULL OR t.launch_block_time >= sqlc.narg(launched_after)::timestamptz)
ORDER BY t.launch_block_number ASC, t.token_address ASC LIMIT sqlc.arg(page_size)::integer;

-- Metric sorts walk tokens_phase_*_cursor_idx; the sort columns mirror token_stats (0 when
-- no stats row exists) through triggers, see migration 00014.
-- name: ListTokenCardsMarketCap :many
SELECT t.token_address, t.name, t.symbol, t.phase, t.launch_block_number, t.launch_block_time, t.total_supply,
       t.sort_market_cap_eth_wad AS market_cap_eth_wad, t.sort_volume_24h_eth_wad AS volume_24h_eth_wad,
       COALESCE(s.holder_count, 0)::BIGINT AS holder_count, m.description, m.image_url, m.x_url, m.telegram_url, t.launch_block_hash
FROM tokens AS t
LEFT JOIN token_metadata AS m ON m.chain_id = t.chain_id AND m.token_address = t.token_address
    AND m.launch_tx_hash = t.launch_tx_hash AND m.launch_log_index = t.launch_log_index
LEFT JOIN token_stats AS s ON s.chain_id = t.chain_id AND s.token_address = t.token_address
WHERE t.chain_id = sqlc.arg(chain_id) AND t.phase = sqlc.arg(phase)
  AND (sqlc.arg(search)::text = '' OR lower(t.name) LIKE lower(sqlc.arg(search)::text) || '%' OR lower(t.symbol) LIKE lower(sqlc.arg(search)::text) || '%' OR encode(t.token_address, 'hex') = lower(CASE WHEN left(sqlc.arg(search)::text,2)='0x' THEN substr(sqlc.arg(search)::text,3) ELSE sqlc.arg(search)::text END))
  AND (sqlc.narg(after_metric)::numeric IS NULL OR (t.sort_market_cap_eth_wad, t.token_address) < (sqlc.narg(after_metric)::numeric, sqlc.narg(after_address)::bytea))
  AND (sqlc.narg(launched_after)::timestamptz IS NULL OR t.launch_block_time >= sqlc.narg(launched_after)::timestamptz)
ORDER BY t.sort_market_cap_eth_wad DESC, t.token_address DESC LIMIT sqlc.arg(page_size)::integer;

-- name: ListTokenCardsVolume :many
SELECT t.token_address, t.name, t.symbol, t.phase, t.launch_block_number, t.launch_block_time, t.total_supply,
       t.sort_market_cap_eth_wad AS market_cap_eth_wad, t.sort_volume_24h_eth_wad AS volume_24h_eth_wad,
       COALESCE(s.holder_count, 0)::BIGINT AS holder_count, m.description, m.image_url, m.x_url, m.telegram_url, t.launch_block_hash
FROM tokens AS t
LEFT JOIN token_metadata AS m ON m.chain_id = t.chain_id AND m.token_address = t.token_address
    AND m.launch_tx_hash = t.launch_tx_hash AND m.launch_log_index = t.launch_log_index
LEFT JOIN token_stats AS s ON s.chain_id = t.chain_id AND s.token_address = t.token_address
WHERE t.chain_id = sqlc.arg(chain_id) AND t.phase = sqlc.arg(phase)
  AND (sqlc.arg(search)::text = '' OR lower(t.name) LIKE lower(sqlc.arg(search)::text) || '%' OR lower(t.symbol) LIKE lower(sqlc.arg(search)::text) || '%' OR encode(t.token_address, 'hex') = lower(CASE WHEN left(sqlc.arg(search)::text,2)='0x' THEN substr(sqlc.arg(search)::text,3) ELSE sqlc.arg(search)::text END))
  AND (sqlc.narg(after_metric)::numeric IS NULL OR (t.sort_volume_24h_eth_wad, t.token_address) < (sqlc.narg(after_metric)::numeric, sqlc.narg(after_address)::bytea))
  AND (sqlc.narg(launched_after)::timestamptz IS NULL OR t.launch_block_time >= sqlc.narg(launched_after)::timestamptz)
ORDER BY t.sort_volume_24h_eth_wad DESC, t.token_address DESC LIMIT sqlc.arg(page_size)::integer;

-- name: ListCandlesAggregated :many
-- Each row is keyed by its UTC-aligned group start (not the first stored source bucket), so
-- sparse groups report a stable timestamp and the next-page cursor can skip a whole group.
SELECT c.group_start AS bucket_start_time,
       (array_agg(c.open_price_wad ORDER BY c.bucket_start_time ASC))[1] AS open_price_wad,
       max(c.high_price_wad) AS high_price_wad,
       min(c.low_price_wad) AS low_price_wad,
       (array_agg(c.close_price_wad ORDER BY c.bucket_start_time DESC))[1] AS close_price_wad,
       sum(c.gross_eth_volume)::numeric AS gross_eth_volume,
       sum(c.token_volume)::numeric AS token_volume,
       sum(c.trade_count)::BIGINT AS trade_count
FROM (
    SELECT source.bucket_start_time, source.open_price_wad, source.high_price_wad,
           source.low_price_wad, source.close_price_wad, source.gross_eth_volume,
           source.token_volume, source.trade_count,
           CASE WHEN sqlc.arg(target_interval)::text = '6h'
                THEN date_trunc('day', source.bucket_start_time, 'UTC')
                     + floor(extract(hour FROM source.bucket_start_time AT TIME ZONE 'UTC') / 6) * interval '6 hours'
                ELSE date_trunc('day', source.bucket_start_time, 'UTC') END AS group_start
    FROM candles AS source
    WHERE source.chain_id = sqlc.arg(chain_id)
      AND source.token_address = sqlc.arg(token_address)
      AND source.interval = sqlc.arg(source_interval)
      AND source.bucket_start_time >= sqlc.arg(from_time)
      AND source.bucket_start_time < sqlc.arg(to_time)
) AS c
GROUP BY c.group_start
ORDER BY c.group_start ASC
LIMIT sqlc.arg(page_size)::integer;

-- name: TokenExists :one
SELECT EXISTS (
    SELECT 1
    FROM tokens
    WHERE chain_id = sqlc.arg(chain_id)
      AND token_address = sqlc.arg(token_address)
);

-- name: GetTokenDetail :one
SELECT t.token_address, t.curve_address, t.lp_pair, t.weth, t.creator,
       t.protocol_treasury, t.engine_version, t.name, t.symbol, t.phase,
       t.launch_block_number, t.launch_block_time, t.total_supply,
       t.initial_virtual_eth, t.initial_virtual_token, t.curve_tokens,
       t.lp_tokens, t.graduation_eth, t.trade_fee_bps, t.protocol_share_bps,
       COALESCE(r.reserve_source, 'curve')::text AS reserve_source,
       COALESCE(r.eth_reserve, t.initial_virtual_eth) AS eth_reserve,
       COALESCE(r.token_reserve, t.initial_virtual_token) AS token_reserve,
       CASE WHEN t.phase = 'graduated' THEN t.graduation_eth
            ELSE GREATEST(COALESCE(r.eth_reserve, t.initial_virtual_eth) - t.initial_virtual_eth, 0::numeric)
       END::numeric(78, 0) AS real_curve_eth,
       CASE WHEN t.phase = 'graduated' THEN 10000
            WHEN t.graduation_eth = 0 THEN 0
            ELSE LEAST(10000, floor(GREATEST(COALESCE(r.eth_reserve, t.initial_virtual_eth) - t.initial_virtual_eth, 0::numeric) * 10000 / t.graduation_eth))
       END::integer AS graduation_progress_bps,
       COALESCE(r.source_block_number, t.launch_block_number) AS reserve_block_number,
       COALESCE(r.source_block_hash, t.launch_block_hash) AS reserve_block_hash,
       COALESCE(r.source_block_time, t.launch_block_time) AS reserve_block_time,
       m.description, m.image_url, m.x_url, m.telegram_url,
       COALESCE(s.spot_price_eth_wad, 0::numeric) AS spot_price_eth_wad,
       COALESCE(s.market_cap_eth_wad, 0::numeric) AS market_cap_eth_wad,
       COALESCE(s.fdv_eth_wad, 0::numeric) AS fdv_eth_wad,
       COALESCE(s.liquidity_eth_wad, 0::numeric) AS liquidity_eth_wad,
       COALESCE(s.ath_price_eth_wad, 0::numeric) AS ath_price_eth_wad,
       COALESCE(s.ath_at, t.launch_block_time) AS ath_at,
       COALESCE(s.volume_24h_eth_wad, 0::numeric) AS volume_24h_eth_wad,
       COALESCE(s.price_change_24h_bps, 0)::bigint AS price_change_24h_bps,
       COALESCE(s.holder_count, 0)::bigint AS holder_count
FROM tokens AS t
LEFT JOIN token_reserves AS r USING (chain_id, token_address)
LEFT JOIN token_metadata AS m
  ON m.chain_id = t.chain_id AND m.token_address = t.token_address
 AND m.launch_tx_hash = t.launch_tx_hash AND m.launch_log_index = t.launch_log_index
LEFT JOIN token_stats AS s ON s.chain_id = t.chain_id AND s.token_address = t.token_address
WHERE t.chain_id = sqlc.arg(chain_id)
  AND t.token_address = sqlc.arg(token_address);

-- The engine-2 facts of a token. The fee recipient is the creator until the latest
-- FeeRecipientAccepted; the pending recipient is the latest proposal unless a later acceptance
-- consumed it or it was a cancellation (address zero).
-- name: GetTokenV2Facts :one
SELECT COALESCE(t.anti_snipe_start_bps, 0)::INTEGER AS anti_snipe_start_bps,
       COALESCE(t.anti_snipe_duration_seconds, 0)::BIGINT AS anti_snipe_duration_seconds,
       COALESCE(t.lp_creator_share_bps, 0)::INTEGER AS lp_creator_share_bps,
       pool.pool_id::BYTEA AS pool_id, pool.position_token_id::NUMERIC AS position_token_id, pool.hook::BYTEA AS pool_hook,
       COALESCE(accepted.next_recipient, t.creator)::BYTEA AS fee_recipient,
       CASE WHEN proposal.next_recipient <> decode(repeat('00', 20), 'hex')
             AND (accepted.block_number IS NULL
                  OR (accepted.block_number, accepted.transaction_index, accepted.log_index)
                     < (proposal.block_number, proposal.transaction_index, proposal.log_index))
            THEN proposal.next_recipient
       END::BYTEA AS pending_fee_recipient
FROM tokens AS t
LEFT JOIN v4_pools AS pool ON pool.chain_id = t.chain_id AND pool.token_address = t.token_address
LEFT JOIN LATERAL (
    SELECT acceptance.next_recipient, acceptance.block_number, acceptance.transaction_index, acceptance.log_index
    FROM fee_recipient_acceptances AS acceptance
    WHERE acceptance.chain_id = t.chain_id AND acceptance.token_address = t.token_address
    ORDER BY acceptance.block_number DESC, acceptance.transaction_index DESC, acceptance.log_index DESC
    LIMIT 1
) AS accepted ON TRUE
LEFT JOIN LATERAL (
    SELECT candidate.next_recipient, candidate.block_number, candidate.transaction_index, candidate.log_index
    FROM fee_recipient_proposals AS candidate
    WHERE candidate.chain_id = t.chain_id AND candidate.token_address = t.token_address
    ORDER BY candidate.block_number DESC, candidate.transaction_index DESC, candidate.log_index DESC
    LIMIT 1
) AS proposal ON TRUE
WHERE t.chain_id = sqlc.arg(chain_id)
  AND t.token_address = sqlc.arg(token_address)
  AND t.engine_version = 2;

-- name: ListLockerOwed :many
SELECT account, locker_eth_owed, locker_token_owed
FROM creator_fee_balances
WHERE chain_id = sqlc.arg(chain_id)
  AND token_address = sqlc.arg(token_address)
  AND (locker_eth_owed > 0 OR locker_token_owed > 0)
ORDER BY account;

-- name: GetTokenQuoteState :one
SELECT t.phase, t.total_supply, t.curve_tokens, t.lp_tokens, t.graduation_eth,
       t.initial_virtual_eth, t.initial_virtual_token, t.trade_fee_bps,
       t.protocol_share_bps,
       COALESCE(r.eth_reserve, t.initial_virtual_eth) AS eth_reserve,
       COALESCE(r.token_reserve, t.initial_virtual_token) AS token_reserve,
       COALESCE(r.source_block_number, t.launch_block_number) AS reserve_block_number,
       COALESCE(r.source_block_hash, t.launch_block_hash) AS reserve_block_hash,
       t.engine_version, t.launch_block_time,
       COALESCE(t.anti_snipe_start_bps, 0)::INTEGER AS anti_snipe_start_bps,
       COALESCE(t.anti_snipe_duration_seconds, 0)::BIGINT AS anti_snipe_duration_seconds
FROM tokens AS t
LEFT JOIN token_reserves AS r USING (chain_id, token_address)
WHERE t.chain_id = sqlc.arg(chain_id)
  AND t.token_address = sqlc.arg(token_address);

-- name: ListStoredCandles :many
SELECT bucket_start_time, open_price_wad, high_price_wad, low_price_wad,
       close_price_wad, gross_eth_volume, token_volume, trade_count
FROM candles
WHERE chain_id = sqlc.arg(chain_id)
  AND token_address = sqlc.arg(token_address)
  AND interval = sqlc.arg(interval)
  AND bucket_start_time >= sqlc.arg(from_time)
  AND bucket_start_time < sqlc.arg(to_time)
  AND (sqlc.narg(after_time)::timestamptz IS NULL OR bucket_start_time > sqlc.narg(after_time)::timestamptz)
ORDER BY bucket_start_time ASC
LIMIT sqlc.arg(page_size)::integer;

-- name: ListMarketTrades :many
SELECT source, COALESCE(encode(trader, 'hex'), '')::text AS trader_hex,
       side_buy, execution_price_wad, spot_price_wad,
       gross_eth_volume, token_volume, block_number, transaction_index,
       tx_hash, log_index, block_time, finality
FROM market_trades
WHERE chain_id = sqlc.arg(chain_id)
  AND token_address = sqlc.arg(token_address)
  AND (sqlc.narg(after_block)::bigint IS NULL OR
       (block_number, transaction_index, log_index) <
       (sqlc.narg(after_block)::bigint, sqlc.narg(after_transaction_index)::integer, sqlc.narg(after_log_index)::integer))
ORDER BY block_number DESC, transaction_index DESC, log_index DESC
LIMIT sqlc.arg(page_size)::integer;

-- name: MarketTradePageFloor :one
-- The key of the page_size-th newest event before the cursor across the token's base tables
-- (curve trades, its V2 pair's swaps, its V4 pool's swaps), each read newest first from its own
-- index. No row means fewer events than one page. The market_trades view keeps a subset of these
-- rows, so the view's page lies at or above this key unless the view dropped one of them.
SELECT candidate.block_number, candidate.transaction_index, candidate.log_index
FROM (
    (SELECT trade.block_number, trade.transaction_index, trade.log_index
     FROM trades AS trade
     WHERE trade.chain_id = sqlc.arg(chain_id) AND trade.token_address = sqlc.arg(token_address)
       AND (trade.block_number, trade.transaction_index, trade.log_index) <
           (sqlc.arg(before_block)::bigint, sqlc.arg(before_transaction_index)::integer, sqlc.arg(before_log_index)::integer)
     ORDER BY trade.block_number DESC, trade.transaction_index DESC, trade.log_index DESC
     LIMIT sqlc.arg(page_size)::integer)
    UNION ALL
    (SELECT swap.block_number, swap.transaction_index, swap.log_index
     FROM pool_swaps AS swap
     WHERE swap.chain_id = sqlc.arg(chain_id)
       AND swap.pair_address = (SELECT token.lp_pair FROM tokens AS token
                                WHERE token.chain_id = sqlc.arg(chain_id) AND token.token_address = sqlc.arg(token_address))
       AND (swap.block_number, swap.transaction_index, swap.log_index) <
           (sqlc.arg(before_block)::bigint, sqlc.arg(before_transaction_index)::integer, sqlc.arg(before_log_index)::integer)
     ORDER BY swap.block_number DESC, swap.transaction_index DESC, swap.log_index DESC
     LIMIT sqlc.arg(page_size)::integer)
    UNION ALL
    (SELECT swap.block_number, swap.transaction_index, swap.log_index
     FROM v4_swaps AS swap
     WHERE swap.chain_id = sqlc.arg(chain_id)
       AND swap.pool_id = (SELECT graduation.pool_id FROM graduations AS graduation
                           WHERE graduation.chain_id = sqlc.arg(chain_id) AND graduation.token_address = sqlc.arg(token_address)
                             AND graduation.pool_id IS NOT NULL
                           ORDER BY graduation.block_number DESC, graduation.transaction_index DESC, graduation.log_index DESC
                           LIMIT 1)
       AND (swap.block_number, swap.transaction_index, swap.log_index) <
           (sqlc.arg(before_block)::bigint, sqlc.arg(before_transaction_index)::integer, sqlc.arg(before_log_index)::integer)
     ORDER BY swap.block_number DESC, swap.transaction_index DESC, swap.log_index DESC
     LIMIT sqlc.arg(page_size)::integer)
) AS candidate
ORDER BY candidate.block_number DESC, candidate.transaction_index DESC, candidate.log_index DESC
OFFSET sqlc.arg(page_size)::integer - 1
LIMIT 1;

-- name: ListMarketTradesFrom :many
-- ListMarketTrades bounded below by MarketTradePageFloor: the bound becomes an index condition in
-- every branch of the view, so a page reads about page_size rows per source instead of the
-- token's whole history.
SELECT source, COALESCE(encode(trader, 'hex'), '')::text AS trader_hex,
       side_buy, execution_price_wad, spot_price_wad,
       gross_eth_volume, token_volume, block_number, transaction_index,
       tx_hash, log_index, block_time, finality
FROM market_trades
WHERE chain_id = sqlc.arg(chain_id)
  AND token_address = sqlc.arg(token_address)
  AND (block_number, transaction_index, log_index) <
      (sqlc.arg(before_block)::bigint, sqlc.arg(before_transaction_index)::integer, sqlc.arg(before_log_index)::integer)
  AND (block_number, transaction_index, log_index) >=
      (sqlc.arg(floor_block)::bigint, sqlc.arg(floor_transaction_index)::integer, sqlc.arg(floor_log_index)::integer)
ORDER BY block_number DESC, transaction_index DESC, log_index DESC
LIMIT sqlc.arg(page_size)::integer;

-- name: ListTokenHolders :many
SELECT h.holder_address, h.balance, h.first_acquired_block_number
FROM holder_balances AS h
JOIN tokens AS t USING (chain_id, token_address)
LEFT JOIN v4_pool_managers AS manager ON manager.chain_id = t.chain_id
WHERE h.chain_id = sqlc.arg(chain_id)
  AND h.token_address = sqlc.arg(token_address)
  AND h.balance > 0
  AND NOT token_holder_excluded(t.engine_version, h.holder_address, t.curve_address, t.lp_pair,
                                t.locker, manager.pool_manager)
  AND (sqlc.narg(after_balance)::numeric IS NULL OR
       (h.balance, h.holder_address) < (sqlc.narg(after_balance)::numeric, sqlc.narg(after_address)::bytea))
ORDER BY h.balance DESC, h.holder_address DESC
LIMIT sqlc.arg(page_size)::integer;

-- name: GetProtocolStats :one
SELECT volume_24h_eth_wad, volume_all_time_eth_wad, launches_24h,
       launches_all_time, trades_24h, trades_all_time, graduations_24h,
       graduations_all_time, updated_at
FROM protocol_stats
WHERE chain_id = sqlc.arg(chain_id);

-- name: ListProtocolDaily :many
SELECT day, volume_eth_wad, launches_count, trades_count, graduations_count
FROM protocol_daily
WHERE chain_id = sqlc.arg(chain_id)
  AND day >= sqlc.arg(from_day)
  AND day <= sqlc.arg(to_day)
ORDER BY day ASC
LIMIT sqlc.arg(page_size)::integer;

-- name: ListTrendingTokens :many
WITH legs AS (
    SELECT trade.token_address, trade.eth_gross AS volume, trade.block_time,
           CASE WHEN trade.is_buy THEN trade.trader END AS buyer
    FROM trades AS trade
    JOIN indexed_blocks AS block
      ON block.chain_id = trade.chain_id AND block.block_number = trade.block_number
    WHERE trade.chain_id = sqlc.arg(chain_id)
      AND trade.block_time >= sqlc.arg(window_start)::timestamptz
    UNION ALL
    SELECT token.token_address,
           CASE WHEN token.token_is_token0 THEN swap.amount1_in + swap.amount1_out
                ELSE swap.amount0_in + swap.amount0_out END AS volume,
           swap.block_time,
           CASE WHEN (CASE WHEN token.token_is_token0 THEN swap.amount0_out > 0 ELSE swap.amount1_out > 0 END)
                THEN swap.to_address END AS buyer
    FROM pool_swaps AS swap
    JOIN tokens AS token
      ON token.chain_id = swap.chain_id AND token.lp_pair = swap.pair_address
    JOIN indexed_blocks AS block
      ON block.chain_id = swap.chain_id AND block.block_number = swap.block_number
    WHERE swap.chain_id = sqlc.arg(chain_id)
      AND swap.block_time >= sqlc.arg(window_start)::timestamptz
), ranked AS (
    SELECT token_address,
           sum(volume)::numeric(78, 0) AS window_volume_eth_wad,
           count(DISTINCT buyer)::BIGINT AS distinct_buyers,
           max(block_time)::timestamptz AS last_trade_time
    FROM legs
    GROUP BY token_address
    HAVING sum(volume) > 0 AND count(DISTINCT buyer) > 0
)
SELECT t.token_address, t.name, t.symbol, t.phase,
       t.launch_block_number, t.launch_block_time, t.total_supply,
       COALESCE(s.market_cap_eth_wad, 0::numeric) AS market_cap_eth_wad,
       COALESCE(s.volume_24h_eth_wad, 0::numeric) AS volume_24h_eth_wad,
       COALESCE(s.holder_count, 0)::BIGINT AS holder_count,
       m.description, m.image_url, m.x_url, m.telegram_url,
       t.launch_block_hash,
       ranked.window_volume_eth_wad, ranked.distinct_buyers, ranked.last_trade_time
FROM ranked
JOIN tokens AS t
  ON t.chain_id = sqlc.arg(chain_id) AND t.token_address = ranked.token_address
LEFT JOIN token_metadata AS m
  ON m.chain_id = t.chain_id AND m.token_address = t.token_address
 AND m.launch_tx_hash = t.launch_tx_hash AND m.launch_log_index = t.launch_log_index
LEFT JOIN token_stats AS s
  ON s.chain_id = t.chain_id AND s.token_address = t.token_address
ORDER BY ranked.window_volume_eth_wad * ranked.distinct_buyers DESC,
         ranked.last_trade_time DESC, t.token_address
LIMIT sqlc.arg(row_limit)::integer;

-- name: GetKingOfTheHill :one
WITH curve_tokens AS (
    SELECT t.token_address, t.name, t.symbol, t.phase,
           t.launch_block_number, t.launch_block_time, t.total_supply,
           COALESCE(s.market_cap_eth_wad, 0::numeric) AS market_cap_eth_wad,
           COALESCE(s.volume_24h_eth_wad, 0::numeric) AS volume_24h_eth_wad,
           COALESCE(s.holder_count, 0)::BIGINT AS holder_count,
           m.description, m.image_url, m.x_url, m.telegram_url,
           t.launch_block_hash,
           p.graduation_progress_bps,
           GREATEST(t.graduation_eth - p.real_curve_eth, 0::numeric)::numeric(78, 0) AS remaining_eth_wad
    FROM tokens AS t
    LEFT JOIN token_reserves AS r USING (chain_id, token_address)
    LEFT JOIN token_metadata AS m
      ON m.chain_id = t.chain_id AND m.token_address = t.token_address
     AND m.launch_tx_hash = t.launch_tx_hash AND m.launch_log_index = t.launch_log_index
    LEFT JOIN token_stats AS s
      ON s.chain_id = t.chain_id AND s.token_address = t.token_address
    CROSS JOIN LATERAL (
        SELECT GREATEST(COALESCE(r.eth_reserve, t.initial_virtual_eth) - t.initial_virtual_eth, 0::numeric)::numeric(78, 0) AS real_curve_eth,
               CASE WHEN t.graduation_eth = 0 THEN 0
                    ELSE LEAST(10000, floor(GREATEST(COALESCE(r.eth_reserve, t.initial_virtual_eth) - t.initial_virtual_eth, 0::numeric) * 10000 / t.graduation_eth))
               END::integer AS graduation_progress_bps
    ) AS p
    WHERE t.chain_id = sqlc.arg(chain_id) AND t.phase = 'curve'
)
SELECT * FROM curve_tokens
ORDER BY graduation_progress_bps DESC, launch_block_time DESC, token_address
LIMIT 1;

-- name: ListMarketTradesAfter :many
WITH curve AS (
    SELECT trade.token_address, trade.block_number, trade.block_time, trade.transaction_index,
           trade.tx_hash, trade.log_index, 'curve'::TEXT AS source, trade.is_buy AS side_buy,
           trade.trader, trade.eth_gross AS gross_eth_volume, trade.token_amount AS token_volume,
           block.finality_status AS finality
    FROM trades AS trade
    JOIN indexed_blocks AS block
      ON block.chain_id = trade.chain_id AND block.block_number = trade.block_number
    WHERE trade.chain_id = sqlc.arg(chain_id)
      AND (trade.block_number, trade.transaction_index, trade.log_index) >
          (sqlc.arg(after_block)::bigint, sqlc.arg(after_transaction_index)::integer, sqlc.arg(after_log_index)::integer)
    ORDER BY trade.block_number ASC, trade.transaction_index ASC, trade.log_index ASC
    LIMIT sqlc.arg(page_size)::integer
), dex AS (
    -- Mirrors the pool branch of the market_trades view; ordered and limited per base table.
    SELECT token.token_address, swap.block_number, swap.block_time, swap.transaction_index,
           swap.tx_hash, swap.log_index, 'dex'::TEXT AS source,
           CASE WHEN token.token_is_token0 THEN swap.amount0_out > 0 ELSE swap.amount1_out > 0 END AS side_buy,
           swap.tx_from AS trader, legs.eth_volume AS gross_eth_volume, legs.token_volume,
           block.finality_status AS finality
    FROM pool_swaps AS swap
    JOIN tokens AS token
      ON token.chain_id = swap.chain_id AND token.lp_pair = swap.pair_address
    JOIN indexed_blocks AS block
      ON block.chain_id = swap.chain_id AND block.block_number = swap.block_number
    JOIN LATERAL (
        SELECT
            CASE WHEN token.token_is_token0 THEN swap.amount1_in + swap.amount1_out
                 ELSE swap.amount0_in + swap.amount0_out END AS eth_volume,
            CASE WHEN token.token_is_token0 THEN swap.amount0_in + swap.amount0_out
                 ELSE swap.amount1_in + swap.amount1_out END AS token_volume
        FROM pool_syncs AS sync
        WHERE sync.chain_id = swap.chain_id
          AND sync.pair_address = swap.pair_address
          AND sync.tx_hash = swap.tx_hash
          AND sync.log_index < swap.log_index
        ORDER BY sync.log_index DESC
        LIMIT 1
    ) AS legs ON TRUE
    WHERE swap.chain_id = sqlc.arg(chain_id)
      AND (swap.block_number, swap.transaction_index, swap.log_index) >
          (sqlc.arg(after_block)::bigint, sqlc.arg(after_transaction_index)::integer, sqlc.arg(after_log_index)::integer)
    ORDER BY swap.block_number ASC, swap.transaction_index ASC, swap.log_index ASC
    LIMIT sqlc.arg(page_size)::integer
), page AS (
    SELECT * FROM curve
    UNION ALL
    SELECT * FROM dex
    ORDER BY block_number ASC, transaction_index ASC, log_index ASC
    LIMIT sqlc.arg(page_size)::integer
)
SELECT p.token_address, l.name, l.symbol, p.source,
       COALESCE(encode(p.trader, 'hex'), '')::text AS trader_hex,
       p.side_buy, p.gross_eth_volume, p.token_volume, p.block_number, p.transaction_index,
       p.tx_hash, p.log_index, p.block_time, p.finality
FROM page AS p
JOIN token_launches AS l ON l.chain_id = sqlc.arg(chain_id) AND l.token_address = p.token_address
ORDER BY p.block_number ASC, p.transaction_index ASC, p.log_index ASC;

-- name: ListRecentMarketTrades :many
WITH curve AS (
    SELECT trade.token_address, trade.block_number, trade.block_time, trade.transaction_index,
           trade.tx_hash, trade.log_index, 'curve'::TEXT AS source, trade.is_buy AS side_buy,
           trade.trader, trade.eth_gross AS gross_eth_volume, trade.token_amount AS token_volume,
           block.finality_status AS finality
    FROM trades AS trade
    JOIN indexed_blocks AS block
      ON block.chain_id = trade.chain_id AND block.block_number = trade.block_number
    WHERE trade.chain_id = sqlc.arg(chain_id)
    ORDER BY trade.block_number DESC, trade.transaction_index DESC, trade.log_index DESC
    LIMIT sqlc.arg(page_size)::integer
), dex AS (
    -- Mirrors the pool branch of the market_trades view; ordered and limited per base table.
    SELECT token.token_address, swap.block_number, swap.block_time, swap.transaction_index,
           swap.tx_hash, swap.log_index, 'dex'::TEXT AS source,
           CASE WHEN token.token_is_token0 THEN swap.amount0_out > 0 ELSE swap.amount1_out > 0 END AS side_buy,
           swap.tx_from AS trader, legs.eth_volume AS gross_eth_volume, legs.token_volume,
           block.finality_status AS finality
    FROM pool_swaps AS swap
    JOIN tokens AS token
      ON token.chain_id = swap.chain_id AND token.lp_pair = swap.pair_address
    JOIN indexed_blocks AS block
      ON block.chain_id = swap.chain_id AND block.block_number = swap.block_number
    JOIN LATERAL (
        SELECT
            CASE WHEN token.token_is_token0 THEN swap.amount1_in + swap.amount1_out
                 ELSE swap.amount0_in + swap.amount0_out END AS eth_volume,
            CASE WHEN token.token_is_token0 THEN swap.amount0_in + swap.amount0_out
                 ELSE swap.amount1_in + swap.amount1_out END AS token_volume
        FROM pool_syncs AS sync
        WHERE sync.chain_id = swap.chain_id
          AND sync.pair_address = swap.pair_address
          AND sync.tx_hash = swap.tx_hash
          AND sync.log_index < swap.log_index
        ORDER BY sync.log_index DESC
        LIMIT 1
    ) AS legs ON TRUE
    WHERE swap.chain_id = sqlc.arg(chain_id)
    ORDER BY swap.block_number DESC, swap.transaction_index DESC, swap.log_index DESC
    LIMIT sqlc.arg(page_size)::integer
), page AS (
    SELECT * FROM curve
    UNION ALL
    SELECT * FROM dex
    ORDER BY block_number DESC, transaction_index DESC, log_index DESC
    LIMIT sqlc.arg(page_size)::integer
)
SELECT p.token_address, l.name, l.symbol, p.source,
       COALESCE(encode(p.trader, 'hex'), '')::text AS trader_hex,
       p.side_buy, p.gross_eth_volume, p.token_volume, p.block_number, p.transaction_index,
       p.tx_hash, p.log_index, p.block_time, p.finality
FROM page AS p
JOIN token_launches AS l ON l.chain_id = sqlc.arg(chain_id) AND l.token_address = p.token_address
ORDER BY p.block_number DESC, p.transaction_index DESC, p.log_index DESC;

-- name: ListGraduationsAfter :many
SELECT g.token_address, g.lp_pair, l.name, l.symbol, g.tx_hash, g.block_number, g.transaction_index, g.log_index
FROM graduations AS g
JOIN token_launches AS l ON l.chain_id = g.chain_id AND l.token_address = g.token_address
WHERE g.chain_id = sqlc.arg(chain_id)
  AND (g.block_number, g.transaction_index, g.log_index) >
      (sqlc.arg(after_block)::bigint, sqlc.arg(after_transaction_index)::integer, sqlc.arg(after_log_index)::integer)
ORDER BY g.block_number ASC, g.transaction_index ASC, g.log_index ASC
LIMIT sqlc.arg(page_size)::integer;

-- name: ListLaunchesAfter :many
SELECT token_address, creator, name, symbol, tx_hash, block_number, transaction_index, log_index
FROM token_launches
WHERE chain_id = sqlc.arg(chain_id)
  AND (block_number, transaction_index, log_index) >
      (sqlc.arg(after_block)::bigint, sqlc.arg(after_transaction_index)::integer, sqlc.arg(after_log_index)::integer)
ORDER BY block_number ASC, transaction_index ASC, log_index ASC
LIMIT sqlc.arg(page_size)::integer;

-- name: GetNewestFeedCursors :one
WITH curve AS (
    SELECT trade.token_address, trade.block_number, trade.block_time, trade.transaction_index,
           trade.tx_hash, trade.log_index, 'curve'::TEXT AS source, trade.is_buy AS side_buy,
           trade.trader, trade.eth_gross AS gross_eth_volume, trade.token_amount AS token_volume,
           block.finality_status AS finality
    FROM trades AS trade
    JOIN indexed_blocks AS block
      ON block.chain_id = trade.chain_id AND block.block_number = trade.block_number
    WHERE trade.chain_id = sqlc.arg(chain_id)
    ORDER BY trade.block_number DESC, trade.transaction_index DESC, trade.log_index DESC
    LIMIT 1
), dex AS (
    -- Mirrors the pool branch of the market_trades view; ordered and limited per base table.
    SELECT token.token_address, swap.block_number, swap.block_time, swap.transaction_index,
           swap.tx_hash, swap.log_index, 'dex'::TEXT AS source,
           CASE WHEN token.token_is_token0 THEN swap.amount0_out > 0 ELSE swap.amount1_out > 0 END AS side_buy,
           swap.tx_from AS trader, legs.eth_volume AS gross_eth_volume, legs.token_volume,
           block.finality_status AS finality
    FROM pool_swaps AS swap
    JOIN tokens AS token
      ON token.chain_id = swap.chain_id AND token.lp_pair = swap.pair_address
    JOIN indexed_blocks AS block
      ON block.chain_id = swap.chain_id AND block.block_number = swap.block_number
    JOIN LATERAL (
        SELECT
            CASE WHEN token.token_is_token0 THEN swap.amount1_in + swap.amount1_out
                 ELSE swap.amount0_in + swap.amount0_out END AS eth_volume,
            CASE WHEN token.token_is_token0 THEN swap.amount0_in + swap.amount0_out
                 ELSE swap.amount1_in + swap.amount1_out END AS token_volume
        FROM pool_syncs AS sync
        WHERE sync.chain_id = swap.chain_id
          AND sync.pair_address = swap.pair_address
          AND sync.tx_hash = swap.tx_hash
          AND sync.log_index < swap.log_index
        ORDER BY sync.log_index DESC
        LIMIT 1
    ) AS legs ON TRUE
    WHERE swap.chain_id = sqlc.arg(chain_id)
    ORDER BY swap.block_number DESC, swap.transaction_index DESC, swap.log_index DESC
    LIMIT 1
), page AS (
    SELECT * FROM curve
    UNION ALL
    SELECT * FROM dex
    ORDER BY block_number DESC, transaction_index DESC, log_index DESC
    LIMIT 1
)
SELECT COALESCE(t.block_number, 0)::bigint AS trade_block,
       COALESCE(t.transaction_index, 0)::integer AS trade_transaction_index,
       COALESCE(t.log_index, 0)::integer AS trade_log_index,
       COALESCE(g.block_number, 0)::bigint AS graduation_block,
       COALESCE(g.transaction_index, 0)::integer AS graduation_transaction_index,
       COALESCE(g.log_index, 0)::integer AS graduation_log_index,
       COALESCE(l.block_number, 0)::bigint AS launch_block,
       COALESCE(l.transaction_index, 0)::integer AS launch_transaction_index,
       COALESCE(l.log_index, 0)::integer AS launch_log_index
FROM (SELECT 1) AS base
LEFT JOIN page AS t ON true
LEFT JOIN LATERAL (
    SELECT g.block_number, g.transaction_index, g.log_index FROM graduations AS g
    WHERE g.chain_id = sqlc.arg(chain_id)
    ORDER BY g.block_number DESC, g.transaction_index DESC, g.log_index DESC LIMIT 1
) AS g ON true
LEFT JOIN LATERAL (
    SELECT l.block_number, l.transaction_index, l.log_index FROM token_launches AS l
    WHERE l.chain_id = sqlc.arg(chain_id)
    ORDER BY l.block_number DESC, l.transaction_index DESC, l.log_index DESC LIMIT 1
) AS l ON true;

-- name: GetSyncWatermarks :one
SELECT COALESCE(safe_number, 0)::bigint AS safe_number, COALESCE(finalized_number, 0)::bigint AS finalized_number
FROM sync_state
WHERE chain_id = sqlc.arg(chain_id) AND deployment_id = sqlc.arg(deployment_id);

-- Wallet page reads. A wallet with no activity has no wallet_totals row; the reader maps that to zeros.
-- name: GetWalletTotals :one
SELECT realized_pnl, tokens_traded, held_count, first_block_number, last_block_number
FROM wallet_totals
WHERE chain_id = sqlc.arg(chain_id) AND wallet = sqlc.arg(wallet);

-- R5: attribution is chain-wide. Backed by the partial index pool_swaps_tx_from_null_idx.
-- name: WalletAttributionComplete :one
SELECT NOT EXISTS (
    SELECT 1 FROM pool_swaps WHERE chain_id = sqlc.arg(chain_id) AND tx_from IS NULL
) AS complete;

-- The only username a wallet read may show: from public_wallet_usernames (published wallet of an
-- unbanned user with a username).
-- name: GetWalletUsername :one
SELECT username::text AS username
FROM public_wallet_usernames
WHERE chain_id = sqlc.arg(chain_id) AND deployment_id = sqlc.arg(deployment_id) AND wallet = sqlc.arg(wallet);

-- name: CountWalletCreated :one
SELECT count(*)::bigint FROM tokens
WHERE chain_id = sqlc.arg(chain_id) AND creator = sqlc.arg(wallet);

-- Single source of the read-time position value (also used by ListWalletOpenPositions):
--   current_value = div(balance * eth_reserve, NULLIF(token_reserve, 0)), coalesced to 0; a
--   graduated engine-2 token ('v4' reserve) uses div(balance * v4_spot_price_wad, 1e18) instead.
-- div() is exact truncating division; a missing token_reserves row or a zero token_reserve gives 0.
-- unrealized PnL = current_value - remaining_cost, computed by the reader.
-- name: GetWalletValuation :one
SELECT COALESCE(sum(valued.current_value), 0)::numeric AS portfolio_value,
       COALESCE(sum(valued.remaining_cost), 0)::numeric AS open_cost
FROM (
    SELECT p.remaining_cost,
           COALESCE(CASE WHEN r.reserve_source = 'v4'
                         THEN div(p.balance * v4_spot_price_wad(r.sqrt_price_x96), 1000000000000000000)
                         ELSE div(p.balance * r.eth_reserve, NULLIF(r.token_reserve, 0))
                    END, 0)::numeric AS current_value
    FROM wallet_positions AS p
    JOIN tokens AS t ON t.chain_id = p.chain_id AND t.token_address = p.token_address
    LEFT JOIN token_reserves AS r ON r.chain_id = p.chain_id AND r.token_address = p.token_address
    WHERE p.chain_id = sqlc.arg(chain_id) AND p.wallet = sqlc.arg(wallet) AND p.balance > 0
) AS valued;

-- Open positions in one page, largest value first. current_value is the single-source expression
-- documented on GetWalletValuation; keep the two identical. Not keyset-paginated: the order is a live
-- value, so the reader asks for MaxOpenPositions+1 rows and reports truncation.
-- name: ListWalletOpenPositions :many
SELECT valued.token_address, valued.name, valued.symbol, valued.phase, valued.balance, valued.remaining_cost,
       valued.realized_pnl, valued.transferred_in, valued.trade_count, valued.last_block_number, valued.current_value
FROM (
    SELECT p.token_address, t.name, t.symbol, t.phase, p.balance, p.remaining_cost, p.realized_pnl,
           p.transferred_in, p.trade_count, p.last_block_number,
           COALESCE(CASE WHEN r.reserve_source = 'v4'
                         THEN div(p.balance * v4_spot_price_wad(r.sqrt_price_x96), 1000000000000000000)
                         ELSE div(p.balance * r.eth_reserve, NULLIF(r.token_reserve, 0))
                    END, 0)::numeric AS current_value
    FROM wallet_positions AS p
    JOIN tokens AS t ON t.chain_id = p.chain_id AND t.token_address = p.token_address
    LEFT JOIN token_reserves AS r ON r.chain_id = p.chain_id AND r.token_address = p.token_address
    WHERE p.chain_id = sqlc.arg(chain_id) AND p.wallet = sqlc.arg(wallet) AND p.balance > 0
) AS valued
ORDER BY valued.current_value DESC, valued.token_address
LIMIT sqlc.arg(page_size)::integer;

-- Closed positions (balance 0) carry no value, newest activity first.
-- name: ListWalletClosedPositions :many
SELECT p.token_address, t.name, t.symbol, t.phase, p.balance, p.remaining_cost, p.realized_pnl,
       p.transferred_in, p.trade_count, p.last_block_number
FROM wallet_positions AS p
JOIN tokens AS t ON t.chain_id = p.chain_id AND t.token_address = p.token_address
WHERE p.chain_id = sqlc.arg(chain_id) AND p.wallet = sqlc.arg(wallet) AND p.balance = 0
  AND (sqlc.narg(after_block)::bigint IS NULL OR
       p.last_block_number < sqlc.narg(after_block)::bigint OR
       (p.last_block_number = sqlc.narg(after_block)::bigint AND p.token_address > sqlc.narg(after_address)::bytea))
ORDER BY p.last_block_number DESC, p.token_address
LIMIT sqlc.arg(page_size)::integer;

-- name: ListWalletTrades :many
SELECT m.token_address, t.name, t.symbol, m.source, m.side_buy, m.execution_price_wad, m.spot_price_wad,
       m.gross_eth_volume, m.token_volume, m.block_number, m.transaction_index,
       m.tx_hash, m.log_index, m.block_time, m.finality
FROM market_trades AS m
JOIN tokens AS t ON t.chain_id = m.chain_id AND t.token_address = m.token_address
WHERE m.chain_id = sqlc.arg(chain_id)
  AND m.trader = sqlc.arg(wallet)::bytea
  AND (sqlc.narg(after_block)::bigint IS NULL OR
       (m.block_number, m.transaction_index, m.log_index) <
       (sqlc.narg(after_block)::bigint, sqlc.narg(after_transaction_index)::integer, sqlc.narg(after_log_index)::integer))
ORDER BY m.block_number DESC, m.transaction_index DESC, m.log_index DESC
LIMIT sqlc.arg(page_size)::integer;

-- name: ListWalletCreated :many
SELECT t.token_address, t.name, t.symbol, t.phase, t.launch_block_number,
       COALESCE(s.market_cap_eth_wad, 0::numeric) AS market_cap_eth_wad
FROM tokens AS t
LEFT JOIN token_stats AS s ON s.chain_id = t.chain_id AND s.token_address = t.token_address
WHERE t.chain_id = sqlc.arg(chain_id) AND t.creator = sqlc.arg(wallet)
  AND (sqlc.narg(after_block)::bigint IS NULL OR
       t.launch_block_number < sqlc.narg(after_block)::bigint OR
       (t.launch_block_number = sqlc.narg(after_block)::bigint AND t.token_address < sqlc.narg(after_address)::bytea))
ORDER BY t.launch_block_number DESC, t.token_address DESC
LIMIT sqlc.arg(page_size)::integer;

-- R4 at read time: the zero address, every curve or pair address, every engine-2 locker and the
-- chain's V4 PoolManager are never ranked. The factory is never marked dirty, so it never has
-- totals.
-- name: ListLeaderboard :many
-- Only wallets with a positive realized PnL rank; fewer than page_size rows is normal. Ties are
-- broken by wallet address, so the order is deterministic.
SELECT w.wallet, w.realized_pnl, w.tokens_traded, pu.username
FROM wallet_totals AS w
LEFT JOIN public_wallet_usernames AS pu
  ON pu.chain_id = w.chain_id AND pu.deployment_id = sqlc.arg(deployment_id)::text AND pu.wallet = w.wallet
WHERE w.chain_id = sqlc.arg(chain_id)
  AND w.realized_pnl > 0
  AND w.wallet <> decode(repeat('00', 20), 'hex')
  AND NOT EXISTS (SELECT 1 FROM tokens AS t WHERE t.chain_id = w.chain_id AND t.curve_address = w.wallet)
  AND NOT EXISTS (SELECT 1 FROM tokens AS t WHERE t.chain_id = w.chain_id AND t.lp_pair = w.wallet)
  AND NOT EXISTS (SELECT 1 FROM tokens AS t WHERE t.chain_id = w.chain_id AND t.locker = w.wallet)
  AND NOT EXISTS (SELECT 1 FROM v4_pool_managers AS m WHERE m.chain_id = w.chain_id AND m.pool_manager = w.wallet)
ORDER BY w.realized_pnl DESC, w.wallet
LIMIT sqlc.arg(page_size)::integer;

-- name: ListLeaderboardWindow :many
-- Rolling-window variant of ListLeaderboard: sums the realized PnL of legs whose block time is
-- within window_days of the transaction's clock (now()). Same exclusions, positive-only rule and
-- tie-break. tokens_traded counts distinct tokens with a realized leg in the window.
WITH windowed AS (
    SELECT l.wallet, sum(l.realized_pnl) AS realized_pnl, count(DISTINCT l.token_address) AS tokens_traded
    FROM wallet_realized_legs AS l
    WHERE l.chain_id = sqlc.arg(chain_id)
      AND l.block_time >= now() - make_interval(days => sqlc.arg(window_days)::integer)
    GROUP BY l.wallet
)
SELECT w.wallet::bytea AS wallet, w.realized_pnl::numeric AS realized_pnl,
       w.tokens_traded::bigint AS tokens_traded, pu.username
FROM windowed AS w
LEFT JOIN public_wallet_usernames AS pu
  ON pu.chain_id = sqlc.arg(chain_id) AND pu.deployment_id = sqlc.arg(deployment_id)::text AND pu.wallet = w.wallet
WHERE w.realized_pnl > 0
  AND w.wallet <> decode(repeat('00', 20), 'hex')
  AND NOT EXISTS (SELECT 1 FROM tokens AS t WHERE t.chain_id = sqlc.arg(chain_id) AND t.curve_address = w.wallet)
  AND NOT EXISTS (SELECT 1 FROM tokens AS t WHERE t.chain_id = sqlc.arg(chain_id) AND t.lp_pair = w.wallet)
  AND NOT EXISTS (SELECT 1 FROM tokens AS t WHERE t.chain_id = sqlc.arg(chain_id) AND t.locker = w.wallet)
  AND NOT EXISTS (SELECT 1 FROM v4_pool_managers AS m WHERE m.chain_id = sqlc.arg(chain_id) AND m.pool_manager = w.wallet)
ORDER BY w.realized_pnl DESC, w.wallet
LIMIT sqlc.arg(page_size)::integer;

-- Early-buyer clusters (docs/specs/2026-11-12-early-buyer-clusters-design.md). The token's
-- creator, and whether the snapshot block is still inside the early window. A pruned or missing
-- snapshot block falls back to the current time.
-- name: GetEarlyBuyerToken :one
SELECT t.creator,
       COALESCE((SELECT b.block_time FROM indexed_blocks AS b
                 WHERE b.chain_id = t.chain_id
                   AND b.block_number = sqlc.arg(snapshot_block)::bigint
                   AND b.block_hash = sqlc.arg(snapshot_hash)::bytea
                 LIMIT 1), now())
         < t.launch_block_time + make_interval(secs => sqlc.arg(window_seconds)::integer) AS window_open
FROM tokens AS t
WHERE t.chain_id = sqlc.arg(chain_id) AND t.token_address = sqlc.arg(token_address);

-- Curve deliveries (a Transfer from the curve is a buy output) within the early window of the
-- launch block's time, infrastructure recipients excluded.
-- name: ListEarlyCurveDeliveries :many
SELECT tr.tx_hash, tr.to_address
FROM tokens AS t
JOIN transfers AS tr ON tr.chain_id = t.chain_id AND tr.token_address = t.token_address
LEFT JOIN v4_pool_managers AS manager ON manager.chain_id = t.chain_id
WHERE t.chain_id = sqlc.arg(chain_id) AND t.token_address = sqlc.arg(token_address)
  AND tr.from_address = t.curve_address
  AND tr.block_number >= t.launch_block_number
  AND tr.block_time <= t.launch_block_time + make_interval(secs => sqlc.arg(window_seconds)::integer)
  AND NOT token_holder_excluded(t.engine_version, tr.to_address, t.curve_address, t.lp_pair,
                                t.locker, manager.pool_manager)
ORDER BY tr.block_number, tr.transaction_index, tr.log_index;

-- Transfers of the token touching one of the given wallets, between two non-infrastructure
-- wallets.
-- name: ListTokenTransfersTouching :many
SELECT tr.from_address, tr.to_address
FROM tokens AS t
JOIN transfers AS tr ON tr.chain_id = t.chain_id AND tr.token_address = t.token_address
LEFT JOIN v4_pool_managers AS manager ON manager.chain_id = t.chain_id
WHERE t.chain_id = sqlc.arg(chain_id) AND t.token_address = sqlc.arg(token_address)
  AND (tr.from_address = ANY(sqlc.arg(wallets)::bytea[]) OR tr.to_address = ANY(sqlc.arg(wallets)::bytea[]))
  AND tr.from_address <> tr.to_address
  AND NOT token_holder_excluded(t.engine_version, tr.from_address, t.curve_address, t.lp_pair,
                                t.locker, manager.pool_manager)
  AND NOT token_holder_excluded(t.engine_version, tr.to_address, t.curve_address, t.lp_pair,
                                t.locker, manager.pool_manager);

-- name: ListHolderBalancesFor :many
SELECT holder_address, balance
FROM holder_balances
WHERE chain_id = sqlc.arg(chain_id) AND token_address = sqlc.arg(token_address)
  AND holder_address = ANY(sqlc.arg(wallets)::bytea[]) AND balance > 0;

-- name: ListProtocolRevenueByEngine :many
-- ETH the contracts credit to the protocol, per engine, earned basis
-- (docs/specs/2026-11-12-protocol-revenue-design.md). The LP share mirrors
-- LpFeeLocker._credit: the creator gets floor(eth * bps / 10000), the protocol the rest.
WITH engines AS (
    SELECT DISTINCT engine_version FROM token_launches WHERE chain_id = sqlc.arg(chain_id)::bigint
), launch_fees AS (
    SELECT engine_version, SUM(launch_fee_paid) AS amount
    FROM token_launches WHERE chain_id = sqlc.arg(chain_id)::bigint GROUP BY engine_version
), trade_fees AS (
    SELECT l.engine_version, SUM(t.protocol_fee) AS fee, SUM(t.snipe_tax) AS tax
    FROM trades t
    JOIN token_launches l ON l.chain_id = t.chain_id AND l.token_address = t.token_address
    WHERE t.chain_id = sqlc.arg(chain_id)::bigint GROUP BY l.engine_version
), remainders AS (
    SELECT l.engine_version, SUM(g.eth_remainder) AS amount
    FROM graduations g
    JOIN token_launches l ON l.chain_id = g.chain_id AND l.token_address = g.token_address
    WHERE g.chain_id = sqlc.arg(chain_id)::bigint GROUP BY l.engine_version
), lp_fees AS (
    SELECT l.engine_version,
           SUM(c.eth_amount - div(c.eth_amount * COALESCE(l.lp_creator_share_bps, 0), 10000)) AS amount
    FROM lp_fee_collections c
    JOIN token_launches l ON l.chain_id = c.chain_id AND l.token_address = c.token_address
    WHERE c.chain_id = sqlc.arg(chain_id)::bigint GROUP BY l.engine_version
)
SELECT e.engine_version::integer AS engine_version,
       COALESCE(lf.amount, 0)::text AS launch_fees,
       COALESCE(tf.fee, 0)::text AS trade_fees,
       COALESCE(tf.tax, 0)::text AS snipe_tax,
       COALESCE(r.amount, 0)::text AS graduation_remainder,
       COALESCE(lp.amount, 0)::text AS lp_fees
FROM engines e
LEFT JOIN launch_fees lf ON lf.engine_version = e.engine_version
LEFT JOIN trade_fees tf ON tf.engine_version = e.engine_version
LEFT JOIN remainders r ON r.engine_version = e.engine_version
LEFT JOIN lp_fees lp ON lp.engine_version = e.engine_version
ORDER BY e.engine_version;

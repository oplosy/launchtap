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
ORDER BY t.launch_block_number ASC, t.token_address ASC LIMIT sqlc.arg(page_size)::integer;

-- name: ListTokenCardsMarketCap :many
SELECT t.token_address, t.name, t.symbol, t.phase, t.launch_block_number, t.launch_block_time, t.total_supply,
       COALESCE(s.market_cap_eth_wad, 0::numeric) AS market_cap_eth_wad, COALESCE(s.volume_24h_eth_wad, 0::numeric) AS volume_24h_eth_wad,
       COALESCE(s.holder_count, 0)::BIGINT AS holder_count, m.description, m.image_url, m.x_url, m.telegram_url, t.launch_block_hash
FROM tokens AS t
LEFT JOIN token_metadata AS m ON m.chain_id = t.chain_id AND m.token_address = t.token_address
    AND m.launch_tx_hash = t.launch_tx_hash AND m.launch_log_index = t.launch_log_index
LEFT JOIN token_stats AS s ON s.chain_id = t.chain_id AND s.token_address = t.token_address
WHERE t.chain_id = sqlc.arg(chain_id) AND t.phase = sqlc.arg(phase)
  AND (sqlc.arg(search)::text = '' OR lower(t.name) LIKE lower(sqlc.arg(search)::text) || '%' OR lower(t.symbol) LIKE lower(sqlc.arg(search)::text) || '%' OR encode(t.token_address, 'hex') = lower(CASE WHEN left(sqlc.arg(search)::text,2)='0x' THEN substr(sqlc.arg(search)::text,3) ELSE sqlc.arg(search)::text END))
  AND (sqlc.narg(after_metric)::numeric IS NULL OR (COALESCE(s.market_cap_eth_wad,0::numeric), t.token_address) < (sqlc.narg(after_metric)::numeric, sqlc.narg(after_address)::bytea))
ORDER BY COALESCE(s.market_cap_eth_wad,0::numeric) DESC, t.token_address DESC LIMIT sqlc.arg(page_size)::integer;

-- name: ListTokenCardsVolume :many
SELECT t.token_address, t.name, t.symbol, t.phase, t.launch_block_number, t.launch_block_time, t.total_supply,
       COALESCE(s.market_cap_eth_wad, 0::numeric) AS market_cap_eth_wad, COALESCE(s.volume_24h_eth_wad, 0::numeric) AS volume_24h_eth_wad,
       COALESCE(s.holder_count, 0)::BIGINT AS holder_count, m.description, m.image_url, m.x_url, m.telegram_url, t.launch_block_hash
FROM tokens AS t
LEFT JOIN token_metadata AS m ON m.chain_id = t.chain_id AND m.token_address = t.token_address
    AND m.launch_tx_hash = t.launch_tx_hash AND m.launch_log_index = t.launch_log_index
LEFT JOIN token_stats AS s ON s.chain_id = t.chain_id AND s.token_address = t.token_address
WHERE t.chain_id = sqlc.arg(chain_id) AND t.phase = sqlc.arg(phase)
  AND (sqlc.arg(search)::text = '' OR lower(t.name) LIKE lower(sqlc.arg(search)::text) || '%' OR lower(t.symbol) LIKE lower(sqlc.arg(search)::text) || '%' OR encode(t.token_address, 'hex') = lower(CASE WHEN left(sqlc.arg(search)::text,2)='0x' THEN substr(sqlc.arg(search)::text,3) ELSE sqlc.arg(search)::text END))
  AND (sqlc.narg(after_metric)::numeric IS NULL OR (COALESCE(s.volume_24h_eth_wad,0::numeric), t.token_address) < (sqlc.narg(after_metric)::numeric, sqlc.narg(after_address)::bytea))
ORDER BY COALESCE(s.volume_24h_eth_wad,0::numeric) DESC, t.token_address DESC LIMIT sqlc.arg(page_size)::integer;

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

-- name: GetTokenQuoteState :one
SELECT t.phase, t.total_supply, t.curve_tokens, t.lp_tokens, t.graduation_eth,
       t.initial_virtual_eth, t.initial_virtual_token, t.trade_fee_bps,
       t.protocol_share_bps,
       COALESCE(r.eth_reserve, t.initial_virtual_eth) AS eth_reserve,
       COALESCE(r.token_reserve, t.initial_virtual_token) AS token_reserve,
       COALESCE(r.source_block_number, t.launch_block_number) AS reserve_block_number,
       COALESCE(r.source_block_hash, t.launch_block_hash) AS reserve_block_hash
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

-- name: ListTokenHolders :many
SELECT h.holder_address, h.balance, h.first_acquired_block_number
FROM holder_balances AS h
JOIN tokens AS t USING (chain_id, token_address)
WHERE h.chain_id = sqlc.arg(chain_id)
  AND h.token_address = sqlc.arg(token_address)
  AND h.balance > 0
  AND h.holder_address <> decode(repeat('00', 20), 'hex')
  AND h.holder_address <> decode(repeat('00', 18) || 'dead', 'hex')
  AND h.holder_address <> t.curve_address
  AND h.holder_address <> t.lp_pair
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

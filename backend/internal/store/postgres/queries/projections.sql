-- name: RebuildTokenProjections :exec
SELECT rebuild_token_projections($1, $2);

-- name: HasCanonicalLaunch :one
SELECT EXISTS(SELECT 1 FROM token_launches WHERE chain_id=$1 AND token_address=$2);

-- name: ClearOrphanProjections :exec
WITH reserves AS (DELETE FROM token_reserves WHERE token_reserves.chain_id=$1 AND token_reserves.token_address=$2),
holders AS (DELETE FROM holder_balances WHERE holder_balances.chain_id=$1 AND holder_balances.token_address=$2),
buckets AS (DELETE FROM candles WHERE candles.chain_id=$1 AND candles.token_address=$2),
stats AS (DELETE FROM token_stats WHERE token_stats.chain_id=$1 AND token_stats.token_address=$2),
dirty AS (DELETE FROM aggregation_dirty WHERE aggregation_dirty.chain_id=$1 AND aggregation_dirty.token_address=$2)
DELETE FROM tokens WHERE tokens.chain_id=$1 AND tokens.token_address=$2;

-- name: DeleteTokenStats :execrows
DELETE FROM token_stats WHERE chain_id=$1 AND token_address=$2;

-- name: ApplyTokenLaunchProjection :exec
INSERT INTO tokens (
    chain_id, token_address, curve_address, lp_pair, weth, creator, protocol_treasury,
    engine_version, name, symbol, total_supply, initial_virtual_eth, initial_virtual_token,
    curve_tokens, lp_tokens, graduation_eth, trade_fee_bps, protocol_share_bps,
    launch_block_number, launch_block_hash, launch_block_time, launch_tx_hash, launch_log_index
) SELECT launch.chain_id, launch.token_address, launch.curve_address, launch.lp_pair, launch.weth, launch.creator, launch.protocol_treasury,
    launch.engine_version, launch.name, launch.symbol, launch.total_supply, launch.virtual_eth, launch.virtual_token, launch.curve_tokens,
    launch.lp_tokens, launch.graduation_eth, launch.trade_fee_bps, launch.protocol_share_bps, launch.block_number, launch.block_hash,
    launch.block_time, launch.tx_hash, launch.log_index
FROM token_launches AS launch WHERE launch.chain_id = $1 AND launch.tx_hash = $2 AND launch.log_index = $3
ON CONFLICT (chain_id, token_address) DO NOTHING;

-- name: ApplyGraduationProjection :exec
UPDATE tokens SET phase = 'graduated', graduation_block_number = graduation.block_number,
    graduation_block_hash = graduation.block_hash, graduation_block_time = graduation.block_time,
    graduation_tx_hash = graduation.tx_hash, graduation_log_index = graduation.log_index
FROM graduations AS graduation
WHERE graduation.chain_id = $1 AND graduation.tx_hash = $2 AND graduation.log_index = $3
  AND tokens.chain_id = graduation.chain_id AND tokens.token_address = graduation.token_address;

-- name: ApplyTradeReserveProjection :exec
INSERT INTO token_reserves (chain_id, token_address, reserve_source, eth_reserve, token_reserve, source_block_number, source_block_hash, source_block_time, source_tx_hash, source_log_index)
SELECT trade.chain_id, trade.token_address, 'curve', trade.new_eth_reserve, trade.new_token_reserve, trade.block_number, trade.block_hash, trade.block_time, trade.tx_hash, trade.log_index
FROM trades AS trade WHERE trade.chain_id = $1 AND trade.tx_hash = $2 AND trade.log_index = $3
ON CONFLICT (chain_id, token_address) DO UPDATE SET reserve_source=EXCLUDED.reserve_source, eth_reserve=EXCLUDED.eth_reserve, token_reserve=EXCLUDED.token_reserve, source_block_number=EXCLUDED.source_block_number, source_block_hash=EXCLUDED.source_block_hash, source_block_time=EXCLUDED.source_block_time, source_tx_hash=EXCLUDED.source_tx_hash, source_log_index=EXCLUDED.source_log_index
WHERE (EXCLUDED.source_block_number, (SELECT transaction_index FROM trades WHERE chain_id=$1 AND tx_hash=$2 AND log_index=$3), EXCLUDED.source_log_index) > (token_reserves.source_block_number, COALESCE(CASE token_reserves.reserve_source WHEN 'curve' THEN (SELECT transaction_index FROM trades WHERE chain_id=token_reserves.chain_id AND tx_hash=token_reserves.source_tx_hash AND log_index=token_reserves.source_log_index) ELSE (SELECT transaction_index FROM pool_syncs WHERE chain_id=token_reserves.chain_id AND tx_hash=token_reserves.source_tx_hash AND log_index=token_reserves.source_log_index) END, -1), token_reserves.source_log_index);

-- name: ApplyPoolSyncReserveProjection :exec
INSERT INTO token_reserves (chain_id, token_address, reserve_source, eth_reserve, token_reserve, source_block_number, source_block_hash, source_block_time, source_tx_hash, source_log_index)
SELECT sync.chain_id, token.token_address, 'pair', CASE WHEN token.token_is_token0 THEN sync.reserve1 ELSE sync.reserve0 END, CASE WHEN token.token_is_token0 THEN sync.reserve0 ELSE sync.reserve1 END, sync.block_number, sync.block_hash, sync.block_time, sync.tx_hash, sync.log_index
FROM pool_syncs AS sync JOIN tokens AS token ON token.chain_id=sync.chain_id AND token.lp_pair=sync.pair_address AND token.phase='graduated'
WHERE sync.chain_id=$1 AND sync.tx_hash=$2 AND sync.log_index=$3
ON CONFLICT (chain_id, token_address) DO UPDATE SET reserve_source=EXCLUDED.reserve_source, eth_reserve=EXCLUDED.eth_reserve, token_reserve=EXCLUDED.token_reserve, source_block_number=EXCLUDED.source_block_number, source_block_hash=EXCLUDED.source_block_hash, source_block_time=EXCLUDED.source_block_time, source_tx_hash=EXCLUDED.source_tx_hash, source_log_index=EXCLUDED.source_log_index
WHERE (EXCLUDED.source_block_number, (SELECT transaction_index FROM pool_syncs WHERE chain_id=$1 AND tx_hash=$2 AND log_index=$3), EXCLUDED.source_log_index) > (token_reserves.source_block_number, COALESCE(CASE token_reserves.reserve_source WHEN 'curve' THEN (SELECT transaction_index FROM trades WHERE chain_id=token_reserves.chain_id AND tx_hash=token_reserves.source_tx_hash AND log_index=token_reserves.source_log_index) ELSE (SELECT transaction_index FROM pool_syncs WHERE chain_id=token_reserves.chain_id AND tx_hash=token_reserves.source_tx_hash AND log_index=token_reserves.source_log_index) END, -1), token_reserves.source_log_index);

-- The pair's opening Sync is emitted by pair.mint before the curve emits Graduated, so the
-- per-Sync projection above sees the token still in the curve phase. Graduation therefore
-- re-applies the pair's latest Sync, matching rebuild_token_projections.
-- name: ApplyGraduationPoolSyncReserveProjection :exec
INSERT INTO token_reserves (chain_id, token_address, reserve_source, eth_reserve, token_reserve, source_block_number, source_block_hash, source_block_time, source_tx_hash, source_log_index)
SELECT sync.chain_id, token.token_address, 'pair', CASE WHEN token.token_is_token0 THEN sync.reserve1 ELSE sync.reserve0 END, CASE WHEN token.token_is_token0 THEN sync.reserve0 ELSE sync.reserve1 END, sync.block_number, sync.block_hash, sync.block_time, sync.tx_hash, sync.log_index
FROM graduations AS graduation
JOIN tokens AS token ON token.chain_id=graduation.chain_id AND token.token_address=graduation.token_address AND token.phase='graduated'
JOIN LATERAL (
    SELECT candidate.chain_id, candidate.reserve0, candidate.reserve1, candidate.block_number, candidate.block_hash, candidate.block_time, candidate.tx_hash, candidate.log_index
    FROM pool_syncs AS candidate
    WHERE candidate.chain_id=token.chain_id AND candidate.pair_address=token.lp_pair
    ORDER BY candidate.block_number DESC, candidate.transaction_index DESC, candidate.log_index DESC
    LIMIT 1
) AS sync ON TRUE
WHERE graduation.chain_id=$1 AND graduation.tx_hash=$2 AND graduation.log_index=$3
ON CONFLICT (chain_id, token_address) DO UPDATE SET reserve_source=EXCLUDED.reserve_source, eth_reserve=EXCLUDED.eth_reserve, token_reserve=EXCLUDED.token_reserve, source_block_number=EXCLUDED.source_block_number, source_block_hash=EXCLUDED.source_block_hash, source_block_time=EXCLUDED.source_block_time, source_tx_hash=EXCLUDED.source_tx_hash, source_log_index=EXCLUDED.source_log_index
WHERE (EXCLUDED.source_block_number, (SELECT transaction_index FROM pool_syncs WHERE chain_id=EXCLUDED.chain_id AND tx_hash=EXCLUDED.source_tx_hash AND log_index=EXCLUDED.source_log_index), EXCLUDED.source_log_index) > (token_reserves.source_block_number, COALESCE(CASE token_reserves.reserve_source WHEN 'curve' THEN (SELECT transaction_index FROM trades WHERE chain_id=token_reserves.chain_id AND tx_hash=token_reserves.source_tx_hash AND log_index=token_reserves.source_log_index) ELSE (SELECT transaction_index FROM pool_syncs WHERE chain_id=token_reserves.chain_id AND tx_hash=token_reserves.source_tx_hash AND log_index=token_reserves.source_log_index) END, -1), token_reserves.source_log_index);

-- name: ApplyTransferProjection :one
WITH deltas AS (
    SELECT holder, sum(delta) AS delta FROM (
        SELECT $3::bytea AS holder, -$4::numeric AS delta
        WHERE $3::bytea <> decode(repeat('00',20),'hex')
        UNION ALL SELECT $5::bytea AS holder, $4::numeric AS delta
    ) AS legs GROUP BY holder
), valid AS (
    SELECT NOT EXISTS (
        SELECT 1 FROM deltas LEFT JOIN holder_balances AS existing
        ON existing.chain_id=$1 AND existing.token_address=$2 AND existing.holder_address=deltas.holder
        WHERE coalesce(existing.balance,0)+deltas.delta < 0
    ) AS ok
), applied AS (
    INSERT INTO holder_balances (chain_id, token_address, holder_address, balance, first_acquired_block_number)
    SELECT $1 AS chain_id, $2 AS token_address, deltas.holder AS holder_address, coalesce(existing.balance,0)+deltas.delta AS balance,
        CASE WHEN coalesce(existing.balance,0)+deltas.delta=0 THEN NULL
             WHEN coalesce(existing.balance,0)=0 THEN $6 ELSE existing.first_acquired_block_number END AS first_acquired_block_number
    FROM deltas LEFT JOIN holder_balances AS existing
        ON existing.chain_id=$1 AND existing.token_address=$2 AND existing.holder_address=deltas.holder
    WHERE (SELECT ok FROM valid)
    ON CONFLICT (chain_id, token_address, holder_address) DO UPDATE SET
        balance = holder_balances.balance + (SELECT delta FROM deltas WHERE holder=EXCLUDED.holder_address),
        first_acquired_block_number = CASE
            WHEN holder_balances.balance + (SELECT delta FROM deltas WHERE holder=EXCLUDED.holder_address)=0 THEN NULL
            WHEN holder_balances.balance=0 THEN $6 ELSE holder_balances.first_acquired_block_number END
    RETURNING 1
)
SELECT (SELECT ok FROM valid) AND EXISTS (SELECT 1 FROM applied) AS applied;

-- name: ApplyMarketTradeCandles :exec
WITH bucketed AS (
    SELECT market.*, '1m'::text AS bucket_interval, date_trunc('minute', market.block_time, 'UTC') AS bucket_start_time
    FROM market_trades AS market WHERE market.chain_id=$1 AND market.tx_hash=$2 AND market.log_index=$3 AND market.execution_price_wad IS NOT NULL
    UNION ALL
    SELECT market.*, '5m'::text, date_trunc('hour', market.block_time, 'UTC') + floor(extract(minute FROM market.block_time AT TIME ZONE 'UTC') / 5) * interval '5 minutes'
    FROM market_trades AS market WHERE market.chain_id=$1 AND market.tx_hash=$2 AND market.log_index=$3 AND market.execution_price_wad IS NOT NULL
    UNION ALL
    SELECT market.*, '1h'::text, date_trunc('hour', market.block_time, 'UTC')
    FROM market_trades AS market WHERE market.chain_id=$1 AND market.tx_hash=$2 AND market.log_index=$3 AND market.execution_price_wad IS NOT NULL
    UNION ALL
    SELECT market.*, '1d'::text, date_trunc('day', market.block_time, 'UTC')
    FROM market_trades AS market WHERE market.chain_id=$1 AND market.tx_hash=$2 AND market.log_index=$3 AND market.execution_price_wad IS NOT NULL
)
INSERT INTO candles (chain_id, token_address, interval, bucket_start_time, open_price_wad, high_price_wad, low_price_wad, close_price_wad, gross_eth_volume, token_volume, trade_count)
SELECT chain_id, token_address, bucket_interval, bucket_start_time, execution_price_wad, execution_price_wad, execution_price_wad, execution_price_wad, gross_eth_volume, token_volume, 1 FROM bucketed
ON CONFLICT (chain_id, token_address, interval, bucket_start_time) DO UPDATE SET high_price_wad=greatest(candles.high_price_wad,EXCLUDED.high_price_wad), low_price_wad=least(candles.low_price_wad,EXCLUDED.low_price_wad), close_price_wad=EXCLUDED.close_price_wad, gross_eth_volume=candles.gross_eth_volume+EXCLUDED.gross_eth_volume, token_volume=candles.token_volume+EXCLUDED.token_volume, trade_count=candles.trade_count+1;

-- name: MarkTokenDirty :exec
INSERT INTO aggregation_dirty (chain_id, token_address, generation) VALUES ($1, $2, nextval('aggregation_dirty_generation_seq'))
ON CONFLICT (chain_id, token_address) DO UPDATE SET generation=nextval('aggregation_dirty_generation_seq');

-- name: MarkPairTokenDirty :exec
INSERT INTO aggregation_dirty (chain_id, token_address, generation)
SELECT token.chain_id, token.token_address, nextval('aggregation_dirty_generation_seq') FROM tokens AS token WHERE token.chain_id=$1 AND token.lp_pair=$2
ON CONFLICT (chain_id, token_address) DO UPDATE SET generation=nextval('aggregation_dirty_generation_seq');

-- name: ClaimAggregationDirty :many
WITH candidate AS (
    SELECT chain_id, token_address
    FROM aggregation_dirty
    WHERE claimed_generation IS NULL
       OR claimed_generation < generation
       OR (claimed_at IS NOT NULL AND claimed_at < now() - interval '30 seconds')
    ORDER BY generation, chain_id, token_address
    LIMIT sqlc.arg(batch_size)::integer
    FOR UPDATE SKIP LOCKED
)
UPDATE aggregation_dirty AS dirty
SET claimed_generation = dirty.generation,
    claimed_at = now(),
    claimed_by = sqlc.arg(worker_id)
FROM candidate
WHERE dirty.chain_id = candidate.chain_id
  AND dirty.token_address = candidate.token_address
RETURNING dirty.chain_id, dirty.token_address, dirty.claimed_generation;

-- name: CompleteAggregationDirty :execrows
DELETE FROM aggregation_dirty
WHERE chain_id = sqlc.arg(chain_id)
  AND token_address = sqlc.arg(token_address)
  AND generation = sqlc.arg(claimed_generation)
  AND claimed_generation = sqlc.arg(claimed_generation)
  AND claimed_by = sqlc.arg(worker_id);

-- name: RecomputeTokenStats :exec
WITH clock AS (
    SELECT now() AS now_at
), prices AS (
    SELECT
        t.chain_id,
        t.token_address,
        t.initial_virtual_eth * 1000000000000000000 / NULLIF(t.initial_virtual_token, 0) AS launch_price,
        t.launch_block_time,
        COALESCE(reserve.eth_reserve * 1000000000000000000 / NULLIF(reserve.token_reserve, 0), 0) AS spot_price,
        COALESCE(reserve.eth_reserve, 0) AS liquidity,
        t.total_supply,
        t.curve_address,
        t.lp_pair
    FROM tokens AS t
    LEFT JOIN token_reserves AS reserve
      ON reserve.chain_id = t.chain_id AND reserve.token_address = t.token_address
    WHERE t.chain_id = $1 AND t.token_address = $2
-- Every trade is bucketed into all four candle intervals; ATH, baseline, latest, and volume
-- read only the finest ('1m') series so each trade is counted once.
), candles_ath AS (
    SELECT c.chain_id, c.token_address, c.high_price_wad, c.bucket_start_time
    FROM candles AS c
    WHERE c.chain_id = $1 AND c.token_address = $2 AND c.interval = '1m'
    ORDER BY c.high_price_wad DESC, c.bucket_start_time ASC
    LIMIT 1
), baseline AS (
    SELECT c.close_price_wad
    FROM candles AS c CROSS JOIN clock
    WHERE c.chain_id = $1 AND c.token_address = $2 AND c.interval = '1m'
      AND c.bucket_start_time <= clock.now_at - interval '24 hours'
    ORDER BY c.bucket_start_time DESC
    LIMIT 1
), latest AS (
    SELECT c.close_price_wad
    FROM candles AS c
    WHERE c.chain_id = $1 AND c.token_address = $2 AND c.interval = '1m'
    ORDER BY c.bucket_start_time DESC
    LIMIT 1
), rolling AS (
    SELECT COALESCE(sum(c.gross_eth_volume), 0) AS volume_24h
    FROM candles AS c CROSS JOIN clock
    WHERE c.chain_id = $1 AND c.token_address = $2 AND c.interval = '1m'
      AND c.bucket_start_time >= clock.now_at - interval '24 hours'
), holders AS (
    SELECT
        p.chain_id,
        p.token_address,
        COALESCE(sum(h.balance) FILTER (
            WHERE h.holder_address IN (
                p.curve_address,
                decode('0000000000000000000000000000000000000000', 'hex'),
                decode('000000000000000000000000000000000000dead', 'hex')
            )
        ), 0) AS non_circulating,
        count(*) FILTER (
            WHERE h.balance > 0
              AND h.holder_address NOT IN (
                  p.curve_address,
                  p.lp_pair,
                  decode('0000000000000000000000000000000000000000', 'hex'),
                  decode('000000000000000000000000000000000000dead', 'hex')
              )
        )::INTEGER AS holder_count
    FROM prices AS p
    LEFT JOIN holder_balances AS h
      ON h.chain_id = p.chain_id AND h.token_address = p.token_address
    GROUP BY p.chain_id, p.token_address
), computed AS (
    SELECT
        p.*,
        h.non_circulating,
        h.holder_count,
        rolling.volume_24h,
        baseline.close_price_wad AS baseline_price,
        latest.close_price_wad AS latest_price,
        COALESCE(candles_ath.high_price_wad, 0) AS candle_ath,
        candles_ath.bucket_start_time AS candle_ath_at,
        previous.ath_price_eth_wad AS previous_ath,
        previous.ath_at AS previous_ath_at
    FROM prices AS p
    JOIN holders AS h USING (chain_id, token_address)
    CROSS JOIN rolling
    LEFT JOIN candles_ath USING (chain_id, token_address)
    LEFT JOIN baseline ON TRUE
    LEFT JOIN latest ON TRUE
    LEFT JOIN token_stats AS previous
      ON previous.chain_id = p.chain_id AND previous.token_address = p.token_address
)
INSERT INTO token_stats (
 chain_id,token_address,spot_price_eth_wad,market_cap_eth_wad,fdv_eth_wad,
 liquidity_eth_wad,ath_price_eth_wad,ath_at,volume_24h_eth_wad,
 price_change_24h_bps,holder_count,updated_at
)
SELECT chain_id, token_address,
 spot_price,
 spot_price * GREATEST(total_supply - non_circulating, 0) / 1000000000000000000,
 spot_price * total_supply / 1000000000000000000,
 liquidity,
 GREATEST(COALESCE(previous_ath, 0), COALESCE(launch_price, 0), candle_ath),
 CASE
   WHEN previous_ath IS NOT NULL AND previous_ath >= GREATEST(COALESCE(launch_price, 0), candle_ath) THEN previous_ath_at
   WHEN candle_ath > COALESCE(launch_price, 0) THEN candle_ath_at
   ELSE launch_block_time
 END,
 volume_24h,
 CASE
   WHEN baseline_price > 0 AND latest_price IS NOT NULL
     THEN trunc((latest_price - baseline_price) * 10000 / baseline_price)::BIGINT
   ELSE 0
 END,
 holder_count,
 now()
FROM computed
ON CONFLICT (chain_id,token_address) DO UPDATE SET
 spot_price_eth_wad=EXCLUDED.spot_price_eth_wad,market_cap_eth_wad=EXCLUDED.market_cap_eth_wad,
 fdv_eth_wad=EXCLUDED.fdv_eth_wad,liquidity_eth_wad=EXCLUDED.liquidity_eth_wad,
 ath_price_eth_wad=EXCLUDED.ath_price_eth_wad,ath_at=EXCLUDED.ath_at,
 volume_24h_eth_wad=EXCLUDED.volume_24h_eth_wad,price_change_24h_bps=EXCLUDED.price_change_24h_bps,
 holder_count=EXCLUDED.holder_count,updated_at=EXCLUDED.updated_at;

-- name: ClearProtocolDaily :exec
DELETE FROM protocol_daily WHERE chain_id=$1;

-- Trades are market_trades (curve trades plus DEX swaps), the same source as volume. Days are
-- UTC calendar days regardless of the session TimeZone.
-- name: RecomputeProtocolDaily :exec
WITH daily AS (
 SELECT (block_time AT TIME ZONE 'UTC')::date AS day, sum(gross_eth_volume) AS volume, 0::bigint AS launches, count(*)::bigint AS trades, 0::bigint AS graduations
 FROM market_trades WHERE chain_id=$1 GROUP BY 1
 UNION ALL SELECT (block_time AT TIME ZONE 'UTC')::date,0,count(*)::bigint,0,0 FROM token_launches WHERE chain_id=$1 GROUP BY 1
 UNION ALL SELECT (block_time AT TIME ZONE 'UTC')::date,0,0,0,count(*)::bigint FROM graduations WHERE chain_id=$1 GROUP BY 1
), daily_rollup AS (
 SELECT day,coalesce(sum(volume),0) volume,coalesce(sum(launches),0)::integer launches,coalesce(sum(trades),0)::integer trades,coalesce(sum(graduations),0)::integer graduations
 FROM daily GROUP BY day
)
INSERT INTO protocol_daily (chain_id,day,volume_eth_wad,launches_count,trades_count,graduations_count)
SELECT $1,day,volume,launches,trades,graduations FROM daily_rollup
ON CONFLICT (chain_id,day) DO UPDATE SET volume_eth_wad=excluded.volume_eth_wad,launches_count=excluded.launches_count,trades_count=excluded.trades_count,graduations_count=excluded.graduations_count;

-- name: RecomputeProtocolStats :exec
INSERT INTO protocol_stats (
 chain_id,volume_24h_eth_wad,volume_all_time_eth_wad,launches_24h,launches_all_time,
 trades_24h,trades_all_time,graduations_24h,graduations_all_time,updated_at
)
SELECT $1,
 COALESCE((SELECT sum(gross_eth_volume) FROM market_trades WHERE chain_id=$1 AND block_time>=now()-interval '24 hours'),0),
 COALESCE((SELECT sum(gross_eth_volume) FROM market_trades WHERE chain_id=$1),0),
 (SELECT count(*) FROM token_launches WHERE chain_id=$1 AND block_time>=now()-interval '24 hours'),
 (SELECT count(*) FROM token_launches WHERE chain_id=$1),
 (SELECT count(*) FROM market_trades WHERE chain_id=$1 AND block_time>=now()-interval '24 hours'),
 (SELECT count(*) FROM market_trades WHERE chain_id=$1),
 (SELECT count(*) FROM graduations WHERE chain_id=$1 AND block_time>=now()-interval '24 hours'),
 (SELECT count(*) FROM graduations WHERE chain_id=$1),now()
ON CONFLICT (chain_id) DO UPDATE SET
 volume_24h_eth_wad=EXCLUDED.volume_24h_eth_wad,volume_all_time_eth_wad=EXCLUDED.volume_all_time_eth_wad,
 launches_24h=EXCLUDED.launches_24h,launches_all_time=EXCLUDED.launches_all_time,
 trades_24h=EXCLUDED.trades_24h,trades_all_time=EXCLUDED.trades_all_time,
 graduations_24h=EXCLUDED.graduations_24h,graduations_all_time=EXCLUDED.graduations_all_time,updated_at=EXCLUDED.updated_at;

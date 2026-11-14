-- name: RebuildTokenProjections :exec
SELECT rebuild_token_projections($1, $2);

-- name: HasCanonicalLaunch :one
SELECT EXISTS(SELECT 1 FROM token_launches WHERE chain_id=$1 AND token_address=$2);

-- name: ClearOrphanProjections :exec
WITH reserves AS (DELETE FROM token_reserves WHERE token_reserves.chain_id=$1 AND token_reserves.token_address=$2),
holders AS (DELETE FROM holder_balances WHERE holder_balances.chain_id=$1 AND holder_balances.token_address=$2),
buckets AS (DELETE FROM candles WHERE candles.chain_id=$1 AND candles.token_address=$2),
stats AS (DELETE FROM token_stats WHERE token_stats.chain_id=$1 AND token_stats.token_address=$2),
dirty AS (DELETE FROM aggregation_dirty WHERE aggregation_dirty.chain_id=$1 AND aggregation_dirty.token_address=$2),
pools AS (DELETE FROM v4_pools WHERE v4_pools.chain_id=$1 AND v4_pools.token_address=$2),
fees AS (DELETE FROM creator_fee_balances WHERE creator_fee_balances.chain_id=$1 AND creator_fee_balances.token_address=$2)
DELETE FROM tokens WHERE tokens.chain_id=$1 AND tokens.token_address=$2;

-- name: DeleteTokenStats :execrows
DELETE FROM token_stats WHERE chain_id=$1 AND token_address=$2;

-- name: ApplyTokenLaunchProjection :exec
INSERT INTO tokens (
    chain_id, token_address, curve_address, lp_pair, weth, creator, protocol_treasury,
    engine_version, name, symbol, total_supply, initial_virtual_eth, initial_virtual_token,
    curve_tokens, lp_tokens, graduation_eth, trade_fee_bps, protocol_share_bps,
    launch_block_number, launch_block_hash, launch_block_time, launch_tx_hash, launch_log_index,
    hook, locker, anti_snipe_start_bps, anti_snipe_duration_seconds, lp_creator_share_bps
) SELECT launch.chain_id, launch.token_address, launch.curve_address, launch.lp_pair, launch.weth, launch.creator, launch.protocol_treasury,
    launch.engine_version, launch.name, launch.symbol, launch.total_supply, launch.virtual_eth, launch.virtual_token, launch.curve_tokens,
    launch.lp_tokens, launch.graduation_eth, launch.trade_fee_bps, launch.protocol_share_bps, launch.block_number, launch.block_hash,
    launch.block_time, launch.tx_hash, launch.log_index,
    launch.hook, launch.locker, launch.anti_snipe_start_bps, launch.anti_snipe_duration_seconds, launch.lp_creator_share_bps
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

-- The V4 pool registry entry of an engine-2 graduation; rebuild_token_projections writes the same row.
-- name: ApplyV4PoolProjection :exec
INSERT INTO v4_pools (chain_id, pool_id, token_address, position_token_id, hook)
SELECT graduation.chain_id, graduation.pool_id, graduation.token_address, graduation.position_token_id, launch.hook
FROM graduations AS graduation
JOIN token_launches AS launch ON launch.chain_id=graduation.chain_id AND launch.token_address=graduation.token_address
WHERE graduation.chain_id=$1 AND graduation.tx_hash=$2 AND graduation.log_index=$3 AND graduation.pool_id IS NOT NULL
ON CONFLICT (chain_id, pool_id) DO NOTHING;

-- The pool price after an engine-2 graduation or V4 swap. It replaces the curve row and any earlier
-- pool price, so an out-of-order replay never moves the price backwards.
-- name: ApplyV4ReserveProjection :exec
INSERT INTO token_reserves (chain_id, token_address, reserve_source, eth_reserve, token_reserve, sqrt_price_x96, source_block_number, source_block_hash, source_block_time, source_tx_hash, source_log_index)
SELECT price.chain_id, price.token_address, 'v4', NULL, NULL, price.sqrt_price_x96, price.block_number, price.block_hash, price.block_time, price.tx_hash, price.log_index
FROM v4_price_events AS price WHERE price.chain_id=$1 AND price.tx_hash=$2 AND price.log_index=$3
ON CONFLICT (chain_id, token_address) DO UPDATE SET reserve_source=EXCLUDED.reserve_source, eth_reserve=NULL, token_reserve=NULL, sqrt_price_x96=EXCLUDED.sqrt_price_x96, source_block_number=EXCLUDED.source_block_number, source_block_hash=EXCLUDED.source_block_hash, source_block_time=EXCLUDED.source_block_time, source_tx_hash=EXCLUDED.source_tx_hash, source_log_index=EXCLUDED.source_log_index
WHERE token_reserves.reserve_source <> 'v4'
   OR (EXCLUDED.source_block_number, (SELECT price.transaction_index FROM v4_price_events AS price WHERE price.chain_id=$1 AND price.tx_hash=$2 AND price.log_index=$3), EXCLUDED.source_log_index) > (token_reserves.source_block_number, COALESCE((SELECT price.transaction_index FROM v4_price_events AS price WHERE price.chain_id=token_reserves.chain_id AND price.tx_hash=token_reserves.source_tx_hash AND price.log_index=token_reserves.source_log_index), -1), token_reserves.source_log_index);

-- Opens a zero balance row for every account one event moves. ApplyCreatorFeeDeltas then adds
-- the deltas with an UPDATE: an upsert would check a claim's negative proposed row against the
-- non-negative CHECKs before resolving the conflict.
-- name: EnsureCreatorFeeAccounts :exec
INSERT INTO creator_fee_balances (chain_id, token_address, account, curve_eth_owed, locker_eth_owed, locker_token_owed)
SELECT DISTINCT delta.chain_id, delta.token_address, delta.account, 0, 0, 0
FROM creator_fee_deltas AS delta WHERE delta.chain_id=$1 AND delta.tx_hash=$2 AND delta.log_index=$3
ON CONFLICT (chain_id, token_address, account) DO NOTHING;

-- Applies one event's owed-balance deltas (creator_fee_deltas). Callers apply it only when the
-- ledger row was newly inserted, so a replay never double-counts.
-- name: ApplyCreatorFeeDeltas :exec
UPDATE creator_fee_balances AS balance SET
    curve_eth_owed=balance.curve_eth_owed+delta.curve_eth_delta,
    locker_eth_owed=balance.locker_eth_owed+delta.locker_eth_delta,
    locker_token_owed=balance.locker_token_owed+delta.locker_token_delta
FROM (
    SELECT event.chain_id, event.token_address, event.account, sum(event.curve_eth_delta) AS curve_eth_delta,
           sum(event.locker_eth_delta) AS locker_eth_delta, sum(event.locker_token_delta) AS locker_token_delta
    FROM creator_fee_deltas AS event WHERE event.chain_id=$1 AND event.tx_hash=$2 AND event.log_index=$3
    GROUP BY event.chain_id, event.token_address, event.account
) AS delta
WHERE balance.chain_id=delta.chain_id AND balance.token_address=delta.token_address AND balance.account=delta.account;

-- name: MarkV4PoolTokenDirty :exec
INSERT INTO aggregation_dirty (chain_id, token_address, generation)
SELECT graduation.chain_id, graduation.token_address, nextval('aggregation_dirty_generation_seq') FROM graduations AS graduation WHERE graduation.chain_id=$1 AND graduation.pool_id=$2
ON CONFLICT (chain_id, token_address) DO UPDATE SET generation=nextval('aggregation_dirty_generation_seq');

-- name: RegisterV4PoolManager :one
WITH inserted AS (
    INSERT INTO v4_pool_managers (chain_id, pool_manager) VALUES ($1, $2)
    ON CONFLICT (chain_id) DO NOTHING
    RETURNING pool_manager
)
SELECT pool_manager FROM inserted
UNION ALL
SELECT existing.pool_manager FROM v4_pool_managers AS existing WHERE existing.chain_id=$1 AND NOT EXISTS (SELECT 1 FROM inserted);

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

-- name: MarkPositionDirty :exec
-- Excluded wallets (R4): the zero address, the factory, and any token's curve or pair.
INSERT INTO position_dirty (chain_id, wallet, token_address, generation)
SELECT sqlc.arg(chain_id)::bigint, sqlc.arg(wallet)::bytea, sqlc.arg(token_address)::bytea, nextval('position_dirty_generation_seq')
WHERE sqlc.arg(wallet)::bytea <> decode(repeat('00', 20), 'hex')
  AND sqlc.arg(wallet)::bytea <> sqlc.arg(factory)::bytea
  AND NOT EXISTS (
    SELECT 1 FROM tokens AS excluded
    WHERE excluded.chain_id = sqlc.arg(chain_id)::bigint AND excluded.curve_address = sqlc.arg(wallet)::bytea
  )
  AND NOT EXISTS (
    SELECT 1 FROM tokens AS excluded
    WHERE excluded.chain_id = sqlc.arg(chain_id)::bigint AND excluded.lp_pair = sqlc.arg(wallet)::bytea
  )
ON CONFLICT (chain_id, wallet, token_address) DO UPDATE SET generation = nextval('position_dirty_generation_seq');

-- name: MarkTransferPositionsDirty :exec
INSERT INTO position_dirty (chain_id, wallet, token_address, generation)
SELECT sides.chain_id, sides.wallet, sides.token_address, nextval('position_dirty_generation_seq')
FROM (
    SELECT DISTINCT sqlc.arg(chain_id)::bigint AS chain_id, side.wallet, sqlc.arg(token_address)::bytea AS token_address
    FROM (VALUES (sqlc.arg(from_address)::bytea), (sqlc.arg(to_address)::bytea)) AS side(wallet)
    WHERE side.wallet <> decode(repeat('00', 20), 'hex')
      AND side.wallet <> sqlc.arg(factory)::bytea
      AND NOT EXISTS (
        SELECT 1 FROM tokens AS excluded
        WHERE excluded.chain_id = sqlc.arg(chain_id)::bigint AND excluded.curve_address = side.wallet
      )
      AND NOT EXISTS (
        SELECT 1 FROM tokens AS excluded
        WHERE excluded.chain_id = sqlc.arg(chain_id)::bigint AND excluded.lp_pair = side.wallet
      )
) AS sides
ON CONFLICT (chain_id, wallet, token_address) DO UPDATE SET generation = nextval('position_dirty_generation_seq');

-- name: MarkPoolSwapPositionDirty :exec
-- A NULL tx_from (sender not yet known) is a no-op.
INSERT INTO position_dirty (chain_id, wallet, token_address, generation)
SELECT token.chain_id, sqlc.arg(tx_from)::bytea, token.token_address, nextval('position_dirty_generation_seq')
FROM tokens AS token
WHERE token.chain_id = sqlc.arg(chain_id)::bigint AND token.lp_pair = sqlc.arg(pair)::bytea
  AND sqlc.arg(tx_from)::bytea IS NOT NULL
  AND sqlc.arg(tx_from)::bytea <> decode(repeat('00', 20), 'hex')
  AND sqlc.arg(tx_from)::bytea <> sqlc.arg(factory)::bytea
  AND NOT EXISTS (
    SELECT 1 FROM tokens AS excluded
    WHERE excluded.chain_id = sqlc.arg(chain_id)::bigint AND excluded.curve_address = sqlc.arg(tx_from)::bytea
  )
  AND NOT EXISTS (
    SELECT 1 FROM tokens AS excluded
    WHERE excluded.chain_id = sqlc.arg(chain_id)::bigint AND excluded.lp_pair = sqlc.arg(tx_from)::bytea
  )
ON CONFLICT (chain_id, wallet, token_address) DO UPDATE SET generation = nextval('position_dirty_generation_seq');

-- name: MarkV4SwapPositionDirty :exec
-- The V4 side of MarkPoolSwapPositionDirty: marks (tx_from, graduated token of the pool) dirty.
-- A NULL tx_from (sender not yet known) is a no-op; the zero address, the factory, the curve, the
-- locker and the PoolManager never get a position (token_holder_excluded).
INSERT INTO position_dirty (chain_id, wallet, token_address, generation)
SELECT graduation.chain_id, sqlc.arg(tx_from)::bytea, graduation.token_address, nextval('position_dirty_generation_seq')
FROM graduations AS graduation
JOIN tokens AS token ON token.chain_id = graduation.chain_id AND token.token_address = graduation.token_address
LEFT JOIN v4_pool_managers AS manager ON manager.chain_id = graduation.chain_id
WHERE graduation.chain_id = sqlc.arg(chain_id)::bigint AND graduation.pool_id = sqlc.arg(pool_id)::bytea
  AND sqlc.arg(tx_from)::bytea IS NOT NULL
  AND sqlc.arg(tx_from)::bytea <> sqlc.arg(factory)::bytea
  AND NOT token_holder_excluded(token.engine_version, sqlc.arg(tx_from)::bytea, token.curve_address,
                                token.lp_pair, token.locker, manager.pool_manager)
ON CONFLICT (chain_id, wallet, token_address) DO UPDATE SET generation = nextval('position_dirty_generation_seq');

-- name: ClearLaunchPositionDirty :exec
-- The launch mint Transfer(0 -> curve) is ingested before the tokens row exists (the token
-- constructor logs it before TokenLaunched), so the exclusion lookup could not see the curve.
-- Drop the curve and pair marks once the row is written (R4).
DELETE FROM position_dirty
WHERE chain_id = sqlc.arg(chain_id)::bigint
  AND token_address = sqlc.arg(token_address)::bytea
  AND wallet IN (sqlc.arg(curve)::bytea, sqlc.arg(lp_pair)::bytea);

-- name: MarkPositionPairsDirty :exec
-- Reorg re-mark: pairs were already filtered at collection time, so no exclusion lookup here
-- (the token row of a pair may no longer exist).
INSERT INTO position_dirty (chain_id, wallet, token_address, generation)
SELECT sqlc.arg(chain_id)::bigint, pair.wallet, pair.token_address, nextval('position_dirty_generation_seq')
FROM (
    SELECT DISTINCT w.wallet, t.token_address
    FROM unnest(sqlc.arg(wallets)::bytea[]) WITH ORDINALITY AS w(wallet, ord)
    JOIN unnest(sqlc.arg(token_addresses)::bytea[]) WITH ORDINALITY AS t(token_address, ord) ON t.ord = w.ord
) AS pair
ON CONFLICT (chain_id, wallet, token_address) DO UPDATE SET generation = nextval('position_dirty_generation_seq');

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

-- name: ClaimPositionDirty :many
WITH candidate AS (
    SELECT chain_id, wallet, token_address
    FROM position_dirty
    WHERE chain_id = sqlc.arg(chain_id)::bigint
      AND (claimed_generation IS NULL
       OR claimed_generation < generation
       OR (claimed_at IS NOT NULL AND claimed_at < now() - interval '30 seconds'))
    ORDER BY generation, chain_id, wallet, token_address
    LIMIT sqlc.arg(batch_size)::integer
    FOR UPDATE SKIP LOCKED
)
UPDATE position_dirty AS dirty
SET claimed_generation = dirty.generation,
    claimed_at = now(),
    claimed_by = sqlc.arg(worker_id)
FROM candidate
WHERE dirty.chain_id = candidate.chain_id
  AND dirty.wallet = candidate.wallet
  AND dirty.token_address = candidate.token_address
RETURNING dirty.chain_id, dirty.wallet, dirty.token_address, dirty.claimed_generation;

-- name: CompletePositionDirty :execrows
DELETE FROM position_dirty
WHERE chain_id = sqlc.arg(chain_id)
  AND wallet = sqlc.arg(wallet)
  AND token_address = sqlc.arg(token_address)
  AND generation = sqlc.arg(claimed_generation)
  AND claimed_generation = sqlc.arg(claimed_generation)
  AND claimed_by = sqlc.arg(worker_id);

-- One leg per transaction that touched (wallet, token), ordered by (block, tx index) (R1):
-- token_delta is the wallet's net transfers in the tx (a self-transfer nets to 0); ETH comes only
-- from trades attributed to the wallet: curve trades by trader, pool swaps by tx_from on the
-- token's own pair, valued on the pair's WETH side, and V4 swaps by tx_from on the token's
-- graduated pool, valued on the swapper's native-ETH delta (amount0 < 0 paid, > 0 received).
-- name: ListPositionLegs :many
WITH token AS (
    SELECT t.lp_pair, t.token_is_token0
    FROM tokens AS t
    WHERE t.chain_id = sqlc.arg(chain_id)::bigint AND t.token_address = sqlc.arg(token_address)::bytea
), transfer_legs AS (
    SELECT moves.tx_hash, moves.block_number, moves.transaction_index, sum(moves.delta) AS token_delta
    FROM (
        SELECT tr.tx_hash, tr.block_number, tr.transaction_index, tr.value AS delta
        FROM transfers AS tr
        WHERE tr.chain_id = sqlc.arg(chain_id)::bigint AND tr.to_address = sqlc.arg(wallet)::bytea
          AND tr.token_address = sqlc.arg(token_address)::bytea
        UNION ALL
        SELECT tr.tx_hash, tr.block_number, tr.transaction_index, -tr.value
        FROM transfers AS tr
        WHERE tr.chain_id = sqlc.arg(chain_id)::bigint AND tr.from_address = sqlc.arg(wallet)::bytea
          AND tr.token_address = sqlc.arg(token_address)::bytea
    ) AS moves
    GROUP BY moves.tx_hash, moves.block_number, moves.transaction_index
), trade_legs AS (
    SELECT trade.tx_hash, trade.block_number, trade.transaction_index,
        sum(CASE WHEN trade.is_buy THEN trade.eth_gross ELSE 0 END) AS eth_paid,
        sum(CASE WHEN trade.is_buy THEN 0 ELSE trade.eth_gross - trade.protocol_fee - trade.creator_fee END) AS eth_received
    FROM trades AS trade
    WHERE trade.chain_id = sqlc.arg(chain_id)::bigint AND trade.trader = sqlc.arg(wallet)::bytea
      AND trade.token_address = sqlc.arg(token_address)::bytea
    GROUP BY trade.tx_hash, trade.block_number, trade.transaction_index
), swap_legs AS (
    SELECT swap.tx_hash, swap.block_number, swap.transaction_index,
        sum(CASE WHEN token.token_is_token0 THEN swap.amount1_in ELSE swap.amount0_in END) AS eth_paid,
        sum(CASE WHEN token.token_is_token0 THEN swap.amount1_out ELSE swap.amount0_out END) AS eth_received
    FROM pool_swaps AS swap
    JOIN token ON swap.pair_address = token.lp_pair
    WHERE swap.chain_id = sqlc.arg(chain_id)::bigint AND swap.tx_from = sqlc.arg(wallet)::bytea
    GROUP BY swap.tx_hash, swap.block_number, swap.transaction_index
), v4_swap_legs AS (
    SELECT swap.tx_hash, swap.block_number, swap.transaction_index,
        sum(CASE WHEN swap.amount0_delta < 0 THEN -swap.amount0_delta ELSE 0 END) AS eth_paid,
        sum(CASE WHEN swap.amount0_delta > 0 THEN swap.amount0_delta ELSE 0 END) AS eth_received
    FROM v4_swaps AS swap
    JOIN graduations AS graduation
      ON graduation.chain_id = swap.chain_id AND graduation.pool_id = swap.pool_id
    WHERE swap.chain_id = sqlc.arg(chain_id)::bigint AND swap.tx_from = sqlc.arg(wallet)::bytea
      AND graduation.token_address = sqlc.arg(token_address)::bytea
    GROUP BY swap.tx_hash, swap.block_number, swap.transaction_index
), legs AS (
    SELECT
        COALESCE(transfer_legs.block_number, trade_legs.block_number, swap_legs.block_number, v4_swap_legs.block_number) AS block_number,
        COALESCE(transfer_legs.transaction_index, trade_legs.transaction_index, swap_legs.transaction_index, v4_swap_legs.transaction_index) AS transaction_index,
        COALESCE(transfer_legs.token_delta, 0) AS token_delta,
        COALESCE(trade_legs.eth_paid, 0) + COALESCE(swap_legs.eth_paid, 0) + COALESCE(v4_swap_legs.eth_paid, 0) AS eth_paid,
        COALESCE(trade_legs.eth_received, 0) + COALESCE(swap_legs.eth_received, 0) + COALESCE(v4_swap_legs.eth_received, 0) AS eth_received
    FROM transfer_legs
    FULL OUTER JOIN trade_legs ON trade_legs.tx_hash = transfer_legs.tx_hash
    FULL OUTER JOIN swap_legs ON swap_legs.tx_hash = COALESCE(transfer_legs.tx_hash, trade_legs.tx_hash)
    FULL OUTER JOIN v4_swap_legs
      ON v4_swap_legs.tx_hash = COALESCE(transfer_legs.tx_hash, trade_legs.tx_hash, swap_legs.tx_hash)
)
-- Every leg's block is an indexed canonical block (ledger rows reference indexed_blocks), so the
-- inner join drops nothing; it supplies the block time the windowed leaderboard filters on.
SELECT
    legs.block_number::bigint AS leg_block_number,
    legs.transaction_index::integer AS leg_transaction_index,
    legs.token_delta::numeric AS leg_token_delta,
    legs.eth_paid::numeric AS leg_eth_paid,
    legs.eth_received::numeric AS leg_eth_received,
    block.block_time AS leg_block_time
FROM legs
JOIN indexed_blocks AS block
  ON block.chain_id = sqlc.arg(chain_id)::bigint AND block.block_number = legs.block_number
ORDER BY leg_block_number, leg_transaction_index;

-- Serializes the wallet_totals recompute of one wallet across concurrent position computes, so
-- each recompute reads every committed position of the wallet.
-- name: LockWalletTotals :exec
SELECT pg_advisory_xact_lock(hashtextextended('wallet_totals:' || sqlc.arg(chain_id)::bigint::text || ':' || encode(sqlc.arg(wallet)::bytea, 'hex'), 0));

-- name: UpsertWalletPosition :exec
INSERT INTO wallet_positions (
    chain_id, wallet, token_address, balance, remaining_cost, realized_pnl, transferred_in,
    eth_paid, eth_received, trade_count, first_block_number, last_block_number
) VALUES (
    sqlc.arg(chain_id), sqlc.arg(wallet), sqlc.arg(token_address), sqlc.arg(balance), sqlc.arg(remaining_cost),
    sqlc.arg(realized_pnl), sqlc.arg(transferred_in), sqlc.arg(eth_paid), sqlc.arg(eth_received),
    sqlc.arg(trade_count), sqlc.arg(first_block_number), sqlc.arg(last_block_number)
)
ON CONFLICT (chain_id, wallet, token_address) DO UPDATE SET
    balance = EXCLUDED.balance, remaining_cost = EXCLUDED.remaining_cost, realized_pnl = EXCLUDED.realized_pnl,
    transferred_in = EXCLUDED.transferred_in, eth_paid = EXCLUDED.eth_paid, eth_received = EXCLUDED.eth_received,
    trade_count = EXCLUDED.trade_count, first_block_number = EXCLUDED.first_block_number,
    last_block_number = EXCLUDED.last_block_number;

-- name: DeleteWalletPosition :exec
DELETE FROM wallet_positions WHERE chain_id = $1 AND wallet = $2 AND token_address = $3;

-- name: DeleteWalletRealizedLegs :exec
DELETE FROM wallet_realized_legs WHERE chain_id = $1 AND wallet = $2 AND token_address = $3;

-- name: InsertWalletRealizedLeg :exec
INSERT INTO wallet_realized_legs (chain_id, wallet, token_address, block_number, transaction_index, block_time, realized_pnl)
VALUES (sqlc.arg(chain_id), sqlc.arg(wallet), sqlc.arg(token_address), sqlc.arg(block_number),
    sqlc.arg(transaction_index), sqlc.arg(block_time), sqlc.arg(realized_pnl));

-- Rebuilds one wallet's totals from its positions; the row is deleted when none remain.
-- name: RecomputeWalletTotals :exec
WITH aggregate AS (
    SELECT p.chain_id, p.wallet, sum(p.realized_pnl) AS realized_pnl,
        count(*) FILTER (WHERE p.trade_count > 0) AS tokens_traded,
        count(*) FILTER (WHERE p.balance > 0) AS held_count,
        min(p.first_block_number) AS first_block_number,
        max(p.last_block_number) AS last_block_number
    FROM wallet_positions AS p
    WHERE p.chain_id = sqlc.arg(chain_id)::bigint AND p.wallet = sqlc.arg(wallet)::bytea
    GROUP BY p.chain_id, p.wallet
), removed AS (
    DELETE FROM wallet_totals AS totals
    WHERE totals.chain_id = sqlc.arg(chain_id)::bigint AND totals.wallet = sqlc.arg(wallet)::bytea
      AND NOT EXISTS (SELECT 1 FROM aggregate)
)
INSERT INTO wallet_totals (chain_id, wallet, realized_pnl, tokens_traded, held_count, first_block_number, last_block_number)
SELECT chain_id, wallet, realized_pnl, tokens_traded, held_count, first_block_number, last_block_number FROM aggregate
ON CONFLICT (chain_id, wallet) DO UPDATE SET
    realized_pnl = EXCLUDED.realized_pnl, tokens_traded = EXCLUDED.tokens_traded, held_count = EXCLUDED.held_count,
    first_block_number = EXCLUDED.first_block_number, last_block_number = EXCLUDED.last_block_number;

-- name: RecomputeTokensStats :exec
-- Recomputes token_stats for every listed token of one chain in one statement (pre-flight
-- 2026-11-13-batch-token-stats-recompute). Unknown tokens are skipped; duplicates are collapsed
-- because ON CONFLICT cannot update the same row twice.
WITH clock AS (
    SELECT now() AS now_at
), targets AS (
    SELECT DISTINCT target.token_address
    FROM unnest(sqlc.arg(token_addresses)::bytea[]) AS target(token_address)
), prices AS (
    SELECT
        t.chain_id,
        t.token_address,
        t.initial_virtual_eth * 1000000000000000000 / NULLIF(t.initial_virtual_token, 0) AS launch_price,
        t.launch_block_time,
        -- Before the first trade there is no reserve row; the curve still quotes from its
        -- initial virtual reserves, matching GetTokenDetail and GetTokenQuoteState. A graduated
        -- engine-2 token prices from its V4 pool and reports no liquidity figure.
        CASE WHEN reserve.reserve_source = 'v4'
            THEN COALESCE(v4_spot_price_wad(reserve.sqrt_price_x96), 0)
            ELSE COALESCE(COALESCE(reserve.eth_reserve, t.initial_virtual_eth) * 1000000000000000000
                / NULLIF(COALESCE(reserve.token_reserve, t.initial_virtual_token), 0), 0)
        END AS spot_price,
        CASE WHEN reserve.reserve_source = 'v4' THEN NULL
            ELSE COALESCE(reserve.eth_reserve, t.initial_virtual_eth, 0)
        END AS liquidity,
        t.total_supply,
        t.curve_address,
        t.lp_pair,
        t.engine_version,
        t.locker,
        manager.pool_manager
    FROM targets
    JOIN tokens AS t
      ON t.chain_id = sqlc.arg(chain_id)::bigint AND t.token_address = targets.token_address
    LEFT JOIN token_reserves AS reserve
      ON reserve.chain_id = t.chain_id AND reserve.token_address = t.token_address
    LEFT JOIN v4_pool_managers AS manager ON manager.chain_id = t.chain_id
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
              AND NOT token_holder_excluded(p.engine_version, h.holder_address, p.curve_address,
                                            p.lp_pair, p.locker, p.pool_manager)
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
    CROSS JOIN clock
    -- Every trade is bucketed into all four candle intervals; ATH, baseline, latest, and volume
    -- read only the finest ('1m') series so each trade is counted once.
    LEFT JOIN LATERAL (
        SELECT c.high_price_wad, c.bucket_start_time
        FROM candles AS c
        WHERE c.chain_id = p.chain_id AND c.token_address = p.token_address AND c.interval = '1m'
        ORDER BY c.high_price_wad DESC, c.bucket_start_time ASC
        LIMIT 1
    ) AS candles_ath ON TRUE
    LEFT JOIN LATERAL (
        SELECT c.close_price_wad
        FROM candles AS c
        WHERE c.chain_id = p.chain_id AND c.token_address = p.token_address AND c.interval = '1m'
          AND c.bucket_start_time <= clock.now_at - interval '24 hours'
        ORDER BY c.bucket_start_time DESC
        LIMIT 1
    ) AS baseline ON TRUE
    LEFT JOIN LATERAL (
        SELECT c.close_price_wad
        FROM candles AS c
        WHERE c.chain_id = p.chain_id AND c.token_address = p.token_address AND c.interval = '1m'
        ORDER BY c.bucket_start_time DESC
        LIMIT 1
    ) AS latest ON TRUE
    CROSS JOIN LATERAL (
        SELECT COALESCE(sum(c.gross_eth_volume), 0) AS volume_24h
        FROM candles AS c
        WHERE c.chain_id = p.chain_id AND c.token_address = p.token_address AND c.interval = '1m'
          AND c.bucket_start_time >= clock.now_at - interval '24 hours'
    ) AS rolling
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
-- A fixed row order keeps concurrent multi-token upserts from locking rows in opposite orders.
ORDER BY chain_id, token_address
ON CONFLICT (chain_id,token_address) DO UPDATE SET
 spot_price_eth_wad=EXCLUDED.spot_price_eth_wad,market_cap_eth_wad=EXCLUDED.market_cap_eth_wad,
 fdv_eth_wad=EXCLUDED.fdv_eth_wad,liquidity_eth_wad=EXCLUDED.liquidity_eth_wad,
 ath_price_eth_wad=EXCLUDED.ath_price_eth_wad,ath_at=EXCLUDED.ath_at,
 volume_24h_eth_wad=EXCLUDED.volume_24h_eth_wad,price_change_24h_bps=EXCLUDED.price_change_24h_bps,
 holder_count=EXCLUDED.holder_count,updated_at=EXCLUDED.updated_at;

-- Serializes protocol aggregate refreshes of one chain, so a refresh reads the watermark only
-- after any concurrent refresh (including a reorg's full rebuild) has committed.
-- name: LockProtocolAggregates :exec
SELECT pg_advisory_xact_lock(hashtextextended('protocol_aggregates:' || sqlc.arg(chain_id)::bigint::text, 0));

-- -1 stands for "none": no watermark yet, or no indexed block.
-- name: GetProtocolAggregateBounds :one
SELECT COALESCE((SELECT stats.daily_through_block FROM protocol_stats AS stats WHERE stats.chain_id = $1), -1)::BIGINT AS daily_through_block,
       COALESCE((SELECT max(block.block_number) FROM indexed_blocks AS block WHERE block.chain_id = $1), -1)::BIGINT AS head_block;

-- The earliest UTC day of the blocks indexed after the watermark; NULL when there are none.
-- name: GetProtocolDailyRefreshDay :one
SELECT (min(block_time) AT TIME ZONE 'UTC')::date AS from_day
FROM indexed_blocks
WHERE chain_id = sqlc.arg(chain_id)
  AND block_number > sqlc.arg(after_block)::bigint
  AND block_number <= sqlc.arg(through_block)::bigint;

-- from_day '-infinity' clears every day of the chain.
-- name: ClearProtocolDaily :exec
DELETE FROM protocol_daily WHERE chain_id = sqlc.arg(chain_id) AND day >= sqlc.arg(from_day)::date;

-- Trades are market_trades (curve trades plus DEX swaps), the same source as volume. Days are
-- UTC calendar days regardless of the session TimeZone. Only days >= from_day are rebuilt;
-- from_day '-infinity' rebuilds the whole history.
-- name: RecomputeProtocolDaily :exec
WITH bounds AS (
 SELECT (sqlc.arg(from_day)::date::timestamp AT TIME ZONE 'UTC') AS from_time
), daily AS (
 SELECT (block_time AT TIME ZONE 'UTC')::date AS day, sum(gross_eth_volume) AS volume, 0::bigint AS launches, count(*)::bigint AS trades, 0::bigint AS graduations
 FROM market_trades WHERE chain_id=sqlc.arg(chain_id) AND block_time >= (SELECT from_time FROM bounds) GROUP BY 1
 UNION ALL SELECT (block_time AT TIME ZONE 'UTC')::date,0,count(*)::bigint,0,0 FROM token_launches WHERE chain_id=sqlc.arg(chain_id) AND block_time >= (SELECT from_time FROM bounds) GROUP BY 1
 UNION ALL SELECT (block_time AT TIME ZONE 'UTC')::date,0,0,0,count(*)::bigint FROM graduations WHERE chain_id=sqlc.arg(chain_id) AND block_time >= (SELECT from_time FROM bounds) GROUP BY 1
), daily_rollup AS (
 SELECT day,coalesce(sum(volume),0) volume,coalesce(sum(launches),0)::integer launches,coalesce(sum(trades),0)::integer trades,coalesce(sum(graduations),0)::integer graduations
 FROM daily GROUP BY day
)
INSERT INTO protocol_daily (chain_id,day,volume_eth_wad,launches_count,trades_count,graduations_count)
SELECT sqlc.arg(chain_id),day,volume,launches,trades,graduations FROM daily_rollup
ON CONFLICT (chain_id,day) DO UPDATE SET volume_eth_wad=excluded.volume_eth_wad,launches_count=excluded.launches_count,trades_count=excluded.trades_count,graduations_count=excluded.graduations_count;

-- The all-time columns are the sums of protocol_daily, which the caller has just brought up to
-- daily_through_block; the 24h columns read the sources by block_time range.
-- name: RecomputeProtocolStats :exec
INSERT INTO protocol_stats (
 chain_id,volume_24h_eth_wad,volume_all_time_eth_wad,launches_24h,launches_all_time,
 trades_24h,trades_all_time,graduations_24h,graduations_all_time,daily_through_block,updated_at
)
SELECT sqlc.arg(chain_id),
 COALESCE(recent.volume,0),
 COALESCE(daily.volume,0),
 (SELECT count(*) FROM token_launches WHERE chain_id=sqlc.arg(chain_id) AND block_time>=now()-interval '24 hours'),
 COALESCE(daily.launches,0)::integer,
 recent.trades,
 COALESCE(daily.trades,0)::bigint,
 (SELECT count(*) FROM graduations WHERE chain_id=sqlc.arg(chain_id) AND block_time>=now()-interval '24 hours'),
 COALESCE(daily.graduations,0)::integer,
 sqlc.narg(daily_through_block)::bigint,
 now()
FROM (
 SELECT sum(volume_eth_wad) AS volume, sum(launches_count) AS launches,
        sum(trades_count) AS trades, sum(graduations_count) AS graduations
 FROM protocol_daily WHERE chain_id=sqlc.arg(chain_id)
) AS daily
CROSS JOIN (
 -- One pass over the 24h market_trades window for both volume and count.
 SELECT sum(gross_eth_volume) AS volume, count(*) AS trades
 FROM market_trades WHERE chain_id=sqlc.arg(chain_id) AND block_time>=now()-interval '24 hours'
) AS recent
ON CONFLICT (chain_id) DO UPDATE SET
 volume_24h_eth_wad=EXCLUDED.volume_24h_eth_wad,volume_all_time_eth_wad=EXCLUDED.volume_all_time_eth_wad,
 launches_24h=EXCLUDED.launches_24h,launches_all_time=EXCLUDED.launches_all_time,
 trades_24h=EXCLUDED.trades_24h,trades_all_time=EXCLUDED.trades_all_time,
 graduations_24h=EXCLUDED.graduations_24h,graduations_all_time=EXCLUDED.graduations_all_time,
 daily_through_block=EXCLUDED.daily_through_block,updated_at=EXCLUDED.updated_at;

-- Marks every token with a canonical trade or DEX swap in [window_from, window_to) dirty.
-- name: MarkTokensDirtyInWindow :execrows
INSERT INTO aggregation_dirty (chain_id, token_address, generation)
SELECT touched.chain_id, touched.token_address, nextval('aggregation_dirty_generation_seq')
FROM (
    SELECT DISTINCT trade.chain_id, trade.token_address
    FROM trades AS trade
    JOIN indexed_blocks AS block
      ON block.chain_id = trade.chain_id AND block.block_number = trade.block_number
    WHERE trade.chain_id = sqlc.arg(chain_id)
      AND trade.block_time >= sqlc.arg(window_from)::timestamptz
      AND trade.block_time < sqlc.arg(window_to)::timestamptz
    UNION
    SELECT DISTINCT token.chain_id, token.token_address
    FROM pool_swaps AS swap
    JOIN indexed_blocks AS block
      ON block.chain_id = swap.chain_id AND block.block_number = swap.block_number
    JOIN tokens AS token
      ON token.chain_id = swap.chain_id AND token.lp_pair = swap.pair_address
    WHERE swap.chain_id = sqlc.arg(chain_id)
      AND swap.block_time >= sqlc.arg(window_from)::timestamptz
      AND swap.block_time < sqlc.arg(window_to)::timestamptz
) AS touched
ON CONFLICT (chain_id, token_address) DO UPDATE SET generation=nextval('aggregation_dirty_generation_seq');

-- Startup catch-up: marks every token still carrying a non-zero 24h volume dirty so expiries
-- missed while the sweeper was down are recomputed.
-- name: MarkTokensWithVolumeDirty :execrows
INSERT INTO aggregation_dirty (chain_id, token_address, generation)
SELECT token.chain_id, token.token_address, nextval('aggregation_dirty_generation_seq')
FROM tokens AS token
WHERE token.chain_id = sqlc.arg(chain_id)
  AND token.sort_volume_24h_eth_wad > 0
ON CONFLICT (chain_id, token_address) DO UPDATE SET generation=nextval('aggregation_dirty_generation_seq');

-- The database clock is the one the stats recompute uses for its 24h window.
-- name: DatabaseNow :one
SELECT now()::timestamptz AS now;

-- name: ListUnattributedPoolSwaps :many
-- Backfill cursor: distinct transactions that still have a v1 pool swap or a V4 swap without a
-- sender, in blocks at or below through_block, in (block_number, tx_hash) order, strictly after
-- the keyset position (after_block, after_hash). UNION merges a transaction present in both.
SELECT unattributed.tx_hash, unattributed.block_number
FROM (
    SELECT swap.tx_hash, swap.block_number
    FROM pool_swaps AS swap
    WHERE swap.chain_id = sqlc.arg(chain_id)::bigint
      AND swap.tx_from IS NULL
      AND swap.block_number <= sqlc.arg(through_block)::bigint
      AND (swap.block_number, swap.tx_hash) > (sqlc.arg(after_block)::bigint, sqlc.arg(after_hash)::bytea)
    UNION
    SELECT swap.tx_hash, swap.block_number
    FROM v4_swaps AS swap
    WHERE swap.chain_id = sqlc.arg(chain_id)::bigint
      AND swap.tx_from IS NULL
      AND swap.block_number <= sqlc.arg(through_block)::bigint
      AND (swap.block_number, swap.tx_hash) > (sqlc.arg(after_block)::bigint, sqlc.arg(after_hash)::bytea)
) AS unattributed
ORDER BY unattributed.block_number, unattributed.tx_hash
LIMIT sqlc.arg(row_limit)::int;

-- name: CountUnattributedPoolSwaps :one
-- v1 pool swaps and V4 swaps still without a sender at or below through_block (the indexer's
-- safe head). The v1 side is served by the partial index pool_swaps_tx_from_null_idx; the V4
-- side by v4_swaps_tx_from_null_idx.
SELECT (
    (SELECT count(*) FROM pool_swaps AS swap
     WHERE swap.chain_id = sqlc.arg(chain_id)::bigint AND swap.tx_from IS NULL
       AND swap.block_number <= sqlc.arg(through_block)::bigint)
  + (SELECT count(*) FROM v4_swaps AS swap
     WHERE swap.chain_id = sqlc.arg(chain_id)::bigint AND swap.tx_from IS NULL
       AND swap.block_number <= sqlc.arg(through_block)::bigint)
)::bigint AS pending;

-- name: SetPoolSwapTxFrom :many
-- Fills the sender of every still-unattributed swap in one transaction and returns the pairs
-- that need a position re-mark.
UPDATE pool_swaps SET tx_from = sqlc.arg(tx_from)::bytea
WHERE chain_id = sqlc.arg(chain_id)::bigint
  AND tx_hash = sqlc.arg(tx_hash)::bytea
  AND tx_from IS NULL
RETURNING pair_address;

-- name: SetV4SwapTxFrom :many
-- The V4 side of SetPoolSwapTxFrom: fills the sender of every still-unattributed V4 swap of the
-- transaction and returns the pools whose graduated token needs an aggregate re-mark.
UPDATE v4_swaps SET tx_from = sqlc.arg(tx_from)::bytea
WHERE chain_id = sqlc.arg(chain_id)::bigint
  AND tx_hash = sqlc.arg(tx_hash)::bytea
  AND tx_from IS NULL
RETURNING pool_id;

-- name: ListSeedTokens :many
-- Keyset page of the chain's launched tokens for the chunked position seed. Every ledger row
-- (trades, transfers) references token_launches, so these pages cover the whole ledger.
SELECT token_address
FROM token_launches
WHERE chain_id = sqlc.arg(chain_id)::bigint
  AND token_address > sqlc.arg(after_token)::bytea
ORDER BY token_address
LIMIT sqlc.arg(row_limit)::int;

-- name: SeedTokenPositionDirty :one
-- One chunk of the seed for ledger history that predates the position queue: every distinct
-- (wallet, token) pair of one token from curve trades by trader, transfers from/to, and
-- attributed pool swaps on the token's own pair, with the R4 exclusions of
-- ListPositionPairsAbove. Idempotent: a pair already queued only gets a new generation. With
-- dry_run nothing is written; pair_count is the number of pairs either way and marked the number
-- of rows inserted or re-generated.
WITH pairs AS (
    SELECT DISTINCT source.wallet, source.token_address FROM (
        SELECT trade.trader AS wallet, trade.token_address FROM trades AS trade
        WHERE trade.chain_id = sqlc.arg(chain_id)::bigint AND trade.token_address = sqlc.arg(token_address)::bytea
        UNION
        SELECT swap.tx_from, token.token_address FROM pool_swaps AS swap
        JOIN tokens AS token ON token.chain_id = swap.chain_id AND token.lp_pair = swap.pair_address
        WHERE swap.chain_id = sqlc.arg(chain_id)::bigint AND swap.tx_from IS NOT NULL
          AND token.token_address = sqlc.arg(token_address)::bytea
        UNION
        SELECT swap.tx_from, graduation.token_address FROM v4_swaps AS swap
        JOIN graduations AS graduation ON graduation.chain_id = swap.chain_id AND graduation.pool_id = swap.pool_id
        WHERE swap.chain_id = sqlc.arg(chain_id)::bigint AND swap.tx_from IS NOT NULL
          AND graduation.token_address = sqlc.arg(token_address)::bytea
        UNION
        SELECT transfer.from_address, transfer.token_address FROM transfers AS transfer
        WHERE transfer.chain_id = sqlc.arg(chain_id)::bigint AND transfer.token_address = sqlc.arg(token_address)::bytea
        UNION
        SELECT transfer.to_address, transfer.token_address FROM transfers AS transfer
        WHERE transfer.chain_id = sqlc.arg(chain_id)::bigint AND transfer.token_address = sqlc.arg(token_address)::bytea
    ) AS source
    WHERE source.wallet <> decode(repeat('00', 20), 'hex')
      AND source.wallet <> sqlc.arg(factory)::bytea
      AND NOT EXISTS (
        SELECT 1 FROM tokens AS excluded
        WHERE excluded.chain_id = sqlc.arg(chain_id)::bigint AND excluded.curve_address = source.wallet
      )
      AND NOT EXISTS (
        SELECT 1 FROM tokens AS excluded
        WHERE excluded.chain_id = sqlc.arg(chain_id)::bigint AND excluded.lp_pair = source.wallet
      )
), marked AS (
    INSERT INTO position_dirty (chain_id, wallet, token_address, generation)
    SELECT sqlc.arg(chain_id)::bigint, pairs.wallet, pairs.token_address, nextval('position_dirty_generation_seq')
    FROM pairs
    WHERE NOT sqlc.arg(dry_run)::boolean
    ON CONFLICT (chain_id, wallet, token_address) DO UPDATE SET generation = nextval('position_dirty_generation_seq')
    RETURNING 1
)
SELECT (SELECT count(*) FROM pairs)::bigint AS pair_count, (SELECT count(*) FROM marked)::bigint AS marked;

-- Leaderboard window refresh (docs/specs/2026-11-14-windowed-leaderboard-preflight.md). Serializes
-- refreshes of one chain so two refreshers never interleave a window's delete and insert.
-- name: LockLeaderboardWindows :exec
SELECT pg_advisory_xact_lock(hashtextextended('leaderboard_windows:' || sqlc.arg(chain_id)::bigint::text, 0));

-- name: DeleteLeaderboardWindowRanks :exec
DELETE FROM leaderboard_window_ranks
WHERE chain_id = sqlc.arg(chain_id) AND window_days = sqlc.arg(window_days)::integer;

-- The top 100 of ListLeaderboardWindow (read.sql), ranked 1..n: the same window, aggregation,
-- positive-only rule, exclusions and tie-break. TestPrecomputedLeaderboardMatchesLiveQuery keeps
-- the two in step.
-- name: InsertLeaderboardWindowRanks :execrows
INSERT INTO leaderboard_window_ranks (chain_id, window_days, rank, wallet, realized_pnl, tokens_traded)
SELECT sqlc.arg(chain_id), sqlc.arg(window_days)::integer,
       row_number() OVER (ORDER BY ranked.realized_pnl DESC, ranked.wallet),
       ranked.wallet, ranked.realized_pnl, ranked.tokens_traded
FROM (
    SELECT w.wallet, w.realized_pnl, w.tokens_traded
    FROM (
        SELECT l.wallet, sum(l.realized_pnl) AS realized_pnl, count(DISTINCT l.token_address) AS tokens_traded
        FROM wallet_realized_legs AS l
        WHERE l.chain_id = sqlc.arg(chain_id)
          AND l.block_time >= now() - make_interval(days => sqlc.arg(window_days)::integer)
        GROUP BY l.wallet
    ) AS w
    WHERE w.realized_pnl > 0
      AND w.wallet <> decode(repeat('00', 20), 'hex')
      AND NOT EXISTS (SELECT 1 FROM tokens AS t WHERE t.chain_id = sqlc.arg(chain_id) AND t.curve_address = w.wallet)
      AND NOT EXISTS (SELECT 1 FROM tokens AS t WHERE t.chain_id = sqlc.arg(chain_id) AND t.lp_pair = w.wallet)
      AND NOT EXISTS (SELECT 1 FROM tokens AS t WHERE t.chain_id = sqlc.arg(chain_id) AND t.locker = w.wallet)
      AND NOT EXISTS (SELECT 1 FROM v4_pool_managers AS m WHERE m.chain_id = sqlc.arg(chain_id) AND m.pool_manager = w.wallet)
    ORDER BY w.realized_pnl DESC, w.wallet
    LIMIT 100
) AS ranked;

-- Stamps the refresh with the transaction's clock, the same now() the window was computed from.
-- name: UpsertLeaderboardWindowRefresh :exec
INSERT INTO leaderboard_window_refreshes (chain_id, window_days, refreshed_at)
VALUES (sqlc.arg(chain_id), sqlc.arg(window_days)::integer, now())
ON CONFLICT (chain_id, window_days) DO UPDATE SET refreshed_at = EXCLUDED.refreshed_at;

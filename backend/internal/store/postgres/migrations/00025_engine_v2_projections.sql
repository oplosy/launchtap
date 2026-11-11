-- +goose Up
-- Engine-aware projections for engine-2 tokens (Plan v2-B Task 8): the V4 pool price, V4 swaps
-- in market_trades, the per-account creator fee ledger, and holder exclusions per engine.

-- The chain's Uniswap V4 PoolManager, registered by the indexer at startup from the reviewed
-- manifest (one deployment per chain, pre-flight B2). Holder views exclude it for engine-2
-- tokens. It is configuration, not chain data, so reorgs never touch it.
CREATE TABLE v4_pool_managers (
    chain_id BIGINT NOT NULL,
    pool_manager BYTEA NOT NULL,
    CONSTRAINT v4_pool_managers_pkey PRIMARY KEY (chain_id),
    CONSTRAINT v4_pool_managers_chain_id_positive CHECK (chain_id > 0),
    CONSTRAINT v4_pool_managers_pool_manager_length CHECK (octet_length(pool_manager) = 20)
);

-- A graduated engine-2 token prices from its V4 pool, so its reserve row carries the pool's
-- sqrtPriceX96 instead of reserves.
ALTER TABLE token_reserves
    ALTER COLUMN eth_reserve DROP NOT NULL,
    ALTER COLUMN token_reserve DROP NOT NULL,
    ADD COLUMN sqrt_price_x96 NUMERIC(78,0),
    DROP CONSTRAINT token_reserves_source_valid,
    ADD CONSTRAINT token_reserves_source_valid CHECK (reserve_source IN ('curve', 'pair', 'v4')),
    ADD CONSTRAINT token_reserves_sqrt_price_x96_positive CHECK (sqrt_price_x96 > 0),
    ADD CONSTRAINT token_reserves_source_shape CHECK (
        CASE reserve_source
            WHEN 'v4' THEN sqrt_price_x96 IS NOT NULL AND eth_reserve IS NULL AND token_reserve IS NULL
            ELSE sqrt_price_x96 IS NULL AND eth_reserve IS NOT NULL AND token_reserve IS NOT NULL
        END
    );

-- A graduated engine-2 token reports no liquidity figure rather than full-range math
-- (pre-flight R5); the locker position is exposed separately.
ALTER TABLE token_stats ALTER COLUMN liquidity_eth_wad DROP NOT NULL;

-- What each account is owed per engine-2 token, mirroring the on-chain views: the curve's
-- creatorFeesOwed(account) and the locker's owed(token, account). The locker splits every
-- collection between the fee recipient and the protocol treasury, so the treasury has a row too.
CREATE TABLE creator_fee_balances (
    chain_id BIGINT NOT NULL,
    token_address BYTEA NOT NULL,
    account BYTEA NOT NULL,
    curve_eth_owed NUMERIC(78,0) NOT NULL,
    locker_eth_owed NUMERIC(78,0) NOT NULL,
    locker_token_owed NUMERIC(78,0) NOT NULL,
    CONSTRAINT creator_fee_balances_pkey PRIMARY KEY (chain_id, token_address, account),
    CONSTRAINT creator_fee_balances_chain_id_positive CHECK (chain_id > 0),
    CONSTRAINT creator_fee_balances_token_address_length CHECK (octet_length(token_address) = 20),
    CONSTRAINT creator_fee_balances_account_length CHECK (octet_length(account) = 20),
    CONSTRAINT creator_fee_balances_curve_eth_owed_nonnegative CHECK (curve_eth_owed >= 0),
    CONSTRAINT creator_fee_balances_locker_eth_owed_nonnegative CHECK (locker_eth_owed >= 0),
    CONSTRAINT creator_fee_balances_locker_token_owed_nonnegative CHECK (locker_token_owed >= 0),
    CONSTRAINT creator_fee_balances_token_fk FOREIGN KEY (chain_id, token_address)
        REFERENCES tokens (chain_id, token_address)
        DEFERRABLE INITIALLY DEFERRED
);

-- +goose StatementBegin
-- ETH per token, 1e18-scaled, of a pool whose currency0 is native ETH and currency1 the token:
-- 1e18 * 2^192 / sqrtPriceX96^2, truncated.
CREATE FUNCTION v4_spot_price_wad(sqrt_price_x96 NUMERIC) RETURNS NUMERIC
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE AS $$
    SELECT div(1000000000000000000 * 6277101735386680763835789423207666416102355444464034512896,
               NULLIF(sqrt_price_x96 * sqrt_price_x96, 0))
$$;
-- +goose StatementEnd

-- +goose StatementBegin
-- Addresses whose balance is not a holder position. Engine 1 excludes the burned-LP address and
-- the pair; engine 2 excludes the locker and the PoolManager instead, because its LP NFT is
-- locked, not burned.
CREATE FUNCTION token_holder_excluded(
    engine_version INTEGER, holder BYTEA, curve BYTEA, lp_pair BYTEA, locker BYTEA, pool_manager BYTEA
) RETURNS BOOLEAN
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
    SELECT holder = decode(repeat('00', 20), 'hex')
        OR holder = curve
        OR CASE WHEN engine_version = 1
                THEN holder = decode(repeat('00', 18) || 'dead', 'hex') OR holder IS NOT DISTINCT FROM lp_pair
                ELSE holder IS NOT DISTINCT FROM locker OR holder IS NOT DISTINCT FROM pool_manager
           END
$$;
-- +goose StatementEnd

-- +goose StatementBegin
-- The creator fee recipient in force strictly before a log: the creator, replaced at each
-- FeeRecipientAccepted.
CREATE FUNCTION creator_fee_recipient_at(
    at_chain_id BIGINT, at_token BYTEA, at_block BIGINT, at_transaction INTEGER, at_log INTEGER
) RETURNS BYTEA
LANGUAGE sql STABLE PARALLEL SAFE AS $$
    SELECT COALESCE(
        (SELECT acceptance.next_recipient
         FROM public.fee_recipient_acceptances AS acceptance
         WHERE acceptance.chain_id = at_chain_id
           AND acceptance.token_address = at_token
           AND (acceptance.block_number, acceptance.transaction_index, acceptance.log_index)
               < (at_block, at_transaction, at_log)
         ORDER BY acceptance.block_number DESC, acceptance.transaction_index DESC,
                  acceptance.log_index DESC
         LIMIT 1),
        (SELECT launch.creator
         FROM public.token_launches AS launch
         WHERE launch.chain_id = at_chain_id AND launch.token_address = at_token))
$$;
-- +goose StatementEnd

-- Every engine-2 event that moves an owed balance, as signed per-account deltas. The rebuild
-- sums it per token; ingestion applies the rows of one event, so both read one definition.
-- The locker creator share is floor(amount * lpCreatorShareBps / 10000); the treasury gets
-- the rest, as LpFeeLocker._credit does.
CREATE VIEW creator_fee_deltas AS
SELECT trade.chain_id, trade.token_address,
       creator_fee_recipient_at(trade.chain_id, trade.token_address, trade.block_number,
                                trade.transaction_index, trade.log_index) AS account,
       trade.tx_hash, trade.log_index,
       trade.creator_fee AS curve_eth_delta, 0::NUMERIC AS locker_eth_delta,
       0::NUMERIC AS locker_token_delta
FROM trades AS trade
JOIN token_launches AS launch
  ON launch.chain_id = trade.chain_id AND launch.token_address = trade.token_address
 AND launch.engine_version = 2

UNION ALL

SELECT collection.chain_id, collection.token_address,
       creator_fee_recipient_at(collection.chain_id, collection.token_address,
                                collection.block_number, collection.transaction_index,
                                collection.log_index),
       collection.tx_hash, collection.log_index, 0::NUMERIC,
       div(collection.eth_amount * launch.lp_creator_share_bps, 10000),
       div(collection.token_amount * launch.lp_creator_share_bps, 10000)
FROM lp_fee_collections AS collection
JOIN token_launches AS launch
  ON launch.chain_id = collection.chain_id AND launch.token_address = collection.token_address

UNION ALL

SELECT collection.chain_id, collection.token_address, launch.protocol_treasury,
       collection.tx_hash, collection.log_index, 0::NUMERIC,
       collection.eth_amount - div(collection.eth_amount * launch.lp_creator_share_bps, 10000),
       collection.token_amount - div(collection.token_amount * launch.lp_creator_share_bps, 10000)
FROM lp_fee_collections AS collection
JOIN token_launches AS launch
  ON launch.chain_id = collection.chain_id AND launch.token_address = collection.token_address

UNION ALL

SELECT claim.chain_id, claim.token_address, claim.creator, claim.tx_hash, claim.log_index,
       -claim.amount, 0::NUMERIC, 0::NUMERIC
FROM creator_fee_claims AS claim
JOIN token_launches AS launch
  ON launch.chain_id = claim.chain_id AND launch.token_address = claim.token_address
 AND launch.engine_version = 2

UNION ALL

SELECT claim.chain_id, claim.token_address, claim.recipient, claim.tx_hash, claim.log_index,
       0::NUMERIC, -claim.eth_amount, -claim.token_amount
FROM lp_fee_claims AS claim;

-- The pool price events of engine-2 tokens: the graduation's opening price, then every swap.
-- Swaps resolve their token through the ledger graduation, not a projection.
CREATE VIEW v4_price_events AS
SELECT graduation.chain_id, graduation.token_address, graduation.block_number,
       graduation.block_hash, graduation.block_time, graduation.transaction_index,
       graduation.tx_hash, graduation.log_index, graduation.sqrt_price_x96
FROM graduations AS graduation
WHERE graduation.pool_id IS NOT NULL

UNION ALL

SELECT swap.chain_id, graduation.token_address, swap.block_number, swap.block_hash,
       swap.block_time, swap.transaction_index, swap.tx_hash, swap.log_index, swap.sqrt_price_x96
FROM v4_swaps AS swap
JOIN graduations AS graduation
  ON graduation.chain_id = swap.chain_id AND graduation.pool_id = swap.pool_id;

CREATE OR REPLACE VIEW market_trades AS
SELECT
    trade.chain_id,
    trade.token_address,
    trade.block_number,
    trade.block_time,
    trade.transaction_index,
    trade.tx_hash,
    trade.log_index,
    'curve'::TEXT AS source,
    trade.is_buy AS side_buy,
    trade.trader,
    trunc(trade.eth_gross * 1000000000000000000::NUMERIC / NULLIF(trade.token_amount, 0))::NUMERIC(78,0)
        AS execution_price_wad,
    trunc(trade.new_eth_reserve * 1000000000000000000::NUMERIC / NULLIF(trade.new_token_reserve, 0))::NUMERIC(78,0)
        AS spot_price_wad,
    trade.eth_gross AS gross_eth_volume,
    trade.token_amount AS token_volume,
    block.finality_status AS finality
FROM trades AS trade
JOIN indexed_blocks AS block
  ON block.chain_id = trade.chain_id
 AND block.block_number = trade.block_number

UNION ALL

SELECT
    swap.chain_id,
    token.token_address,
    swap.block_number,
    swap.block_time,
    swap.transaction_index,
    swap.tx_hash,
    swap.log_index,
    'dex'::TEXT AS source,
    CASE WHEN token.token_is_token0 THEN swap.amount0_out > 0 ELSE swap.amount1_out > 0 END AS side_buy,
    swap.tx_from AS trader,
    trunc(legs.eth_volume * 1000000000000000000::NUMERIC / NULLIF(legs.token_volume, 0))::NUMERIC(78,0)
        AS execution_price_wad,
    trunc(legs.sync_eth_reserve * 1000000000000000000::NUMERIC / NULLIF(legs.sync_token_reserve, 0))::NUMERIC(78,0)
        AS spot_price_wad,
    legs.eth_volume AS gross_eth_volume,
    legs.token_volume,
    block.finality_status AS finality
FROM pool_swaps AS swap
JOIN tokens AS token
  ON token.chain_id = swap.chain_id
 AND token.lp_pair = swap.pair_address
JOIN indexed_blocks AS block
  ON block.chain_id = swap.chain_id
 AND block.block_number = swap.block_number
JOIN LATERAL (
    SELECT
        CASE
            WHEN token.token_is_token0 THEN swap.amount1_in + swap.amount1_out
            ELSE swap.amount0_in + swap.amount0_out
        END AS eth_volume,
        CASE
            WHEN token.token_is_token0 THEN swap.amount0_in + swap.amount0_out
            ELSE swap.amount1_in + swap.amount1_out
        END AS token_volume,
        CASE WHEN token.token_is_token0 THEN sync.reserve1 ELSE sync.reserve0 END AS sync_eth_reserve,
        CASE WHEN token.token_is_token0 THEN sync.reserve0 ELSE sync.reserve1 END AS sync_token_reserve
    FROM pool_syncs AS sync
    WHERE sync.chain_id = swap.chain_id
      AND sync.pair_address = swap.pair_address
      AND sync.tx_hash = swap.tx_hash
      AND sync.log_index < swap.log_index
    ORDER BY sync.log_index DESC
    LIMIT 1
) AS legs ON TRUE

UNION ALL

-- Engine-2 V4 swaps. The deltas are the swapper's: amount0 < 0 means ETH was paid into the
-- pool, a buy. Volumes are gross of the LP fee, like the curve's ethGross (pre-flight R3,
-- confirmed by the v2 fixture swaps).
SELECT
    swap.chain_id,
    graduation.token_address,
    swap.block_number,
    swap.block_time,
    swap.transaction_index,
    swap.tx_hash,
    swap.log_index,
    'dex'::TEXT AS source,
    swap.amount0_delta < 0 AS side_buy,
    swap.tx_from AS trader,
    trunc(abs(swap.amount0_delta) * 1000000000000000000::NUMERIC / NULLIF(abs(swap.amount1_delta), 0))::NUMERIC(78,0)
        AS execution_price_wad,
    v4_spot_price_wad(swap.sqrt_price_x96)::NUMERIC(78,0) AS spot_price_wad,
    abs(swap.amount0_delta)::NUMERIC(78,0) AS gross_eth_volume,
    abs(swap.amount1_delta)::NUMERIC(78,0) AS token_volume,
    block.finality_status AS finality
FROM v4_swaps AS swap
JOIN graduations AS graduation
  ON graduation.chain_id = swap.chain_id
 AND graduation.pool_id = swap.pool_id
JOIN indexed_blocks AS block
  ON block.chain_id = swap.chain_id
 AND block.block_number = swap.block_number;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION rebuild_token_projections(rebuild_chain_id BIGINT, rebuild_token_address BYTEA)
RETURNS void AS $$
BEGIN
    INSERT INTO public.tokens (
        chain_id, token_address, curve_address, lp_pair, weth, creator, protocol_treasury,
        engine_version, name, symbol, total_supply, initial_virtual_eth, initial_virtual_token,
        curve_tokens, lp_tokens, graduation_eth, trade_fee_bps, protocol_share_bps,
        launch_block_number, launch_block_hash, launch_block_time, launch_tx_hash, launch_log_index,
        phase, graduation_block_number, graduation_block_hash, graduation_block_time,
        graduation_tx_hash, graduation_log_index,
        hook, locker, anti_snipe_start_bps, anti_snipe_duration_seconds, lp_creator_share_bps
    )
    SELECT
        launch.chain_id, launch.token_address, launch.curve_address, launch.lp_pair, launch.weth,
        launch.creator, launch.protocol_treasury, launch.engine_version, launch.name, launch.symbol,
        launch.total_supply, launch.virtual_eth, launch.virtual_token, launch.curve_tokens,
        launch.lp_tokens, launch.graduation_eth, launch.trade_fee_bps, launch.protocol_share_bps,
        launch.block_number, launch.block_hash, launch.block_time, launch.tx_hash, launch.log_index,
        CASE WHEN graduation.tx_hash IS NULL THEN 'curve' ELSE 'graduated' END,
        graduation.block_number, graduation.block_hash, graduation.block_time,
        graduation.tx_hash, graduation.log_index,
        launch.hook, launch.locker, launch.anti_snipe_start_bps, launch.anti_snipe_duration_seconds,
        launch.lp_creator_share_bps
    FROM public.token_launches AS launch
    LEFT JOIN LATERAL (
        SELECT candidate.block_number, candidate.block_hash, candidate.block_time,
               candidate.tx_hash, candidate.log_index
        FROM public.graduations AS candidate
        WHERE candidate.chain_id = launch.chain_id
          AND candidate.token_address = launch.token_address
        ORDER BY candidate.block_number DESC, candidate.transaction_index DESC, candidate.log_index DESC
        LIMIT 1
    ) AS graduation ON TRUE
    WHERE launch.chain_id = rebuild_chain_id
      AND launch.token_address = rebuild_token_address
    ON CONFLICT (chain_id, token_address) DO UPDATE SET
        curve_address = EXCLUDED.curve_address,
        lp_pair = EXCLUDED.lp_pair,
        weth = EXCLUDED.weth,
        creator = EXCLUDED.creator,
        protocol_treasury = EXCLUDED.protocol_treasury,
        engine_version = EXCLUDED.engine_version,
        name = EXCLUDED.name,
        symbol = EXCLUDED.symbol,
        total_supply = EXCLUDED.total_supply,
        initial_virtual_eth = EXCLUDED.initial_virtual_eth,
        initial_virtual_token = EXCLUDED.initial_virtual_token,
        curve_tokens = EXCLUDED.curve_tokens,
        lp_tokens = EXCLUDED.lp_tokens,
        graduation_eth = EXCLUDED.graduation_eth,
        trade_fee_bps = EXCLUDED.trade_fee_bps,
        protocol_share_bps = EXCLUDED.protocol_share_bps,
        launch_block_number = EXCLUDED.launch_block_number,
        launch_block_hash = EXCLUDED.launch_block_hash,
        launch_block_time = EXCLUDED.launch_block_time,
        launch_tx_hash = EXCLUDED.launch_tx_hash,
        launch_log_index = EXCLUDED.launch_log_index,
        phase = EXCLUDED.phase,
        graduation_block_number = EXCLUDED.graduation_block_number,
        graduation_block_hash = EXCLUDED.graduation_block_hash,
        graduation_block_time = EXCLUDED.graduation_block_time,
        graduation_tx_hash = EXCLUDED.graduation_tx_hash,
        graduation_log_index = EXCLUDED.graduation_log_index,
        hook = EXCLUDED.hook,
        locker = EXCLUDED.locker,
        anti_snipe_start_bps = EXCLUDED.anti_snipe_start_bps,
        anti_snipe_duration_seconds = EXCLUDED.anti_snipe_duration_seconds,
        lp_creator_share_bps = EXCLUDED.lp_creator_share_bps;

    IF NOT FOUND THEN
        RAISE EXCEPTION 'cannot rebuild unknown token (chain_id=%, token=%)',
            rebuild_chain_id, rebuild_token_address;
    END IF;

    -- The V4 pool registry, from the ledger graduation of an engine-2 token.
    DELETE FROM public.v4_pools
    WHERE chain_id = rebuild_chain_id AND token_address = rebuild_token_address;

    INSERT INTO public.v4_pools (chain_id, pool_id, token_address, position_token_id, hook)
    SELECT graduation.chain_id, graduation.pool_id, graduation.token_address,
           graduation.position_token_id, launch.hook
    FROM public.graduations AS graduation
    JOIN public.token_launches AS launch
      ON launch.chain_id = graduation.chain_id
     AND launch.token_address = graduation.token_address
    WHERE graduation.chain_id = rebuild_chain_id
      AND graduation.token_address = rebuild_token_address
      AND graduation.pool_id IS NOT NULL;

    DELETE FROM public.token_reserves
    WHERE chain_id = rebuild_chain_id AND token_address = rebuild_token_address;

    INSERT INTO public.token_reserves (
        chain_id, token_address, reserve_source, eth_reserve, token_reserve, sqrt_price_x96,
        source_block_number, source_block_hash, source_block_time, source_tx_hash, source_log_index
    )
    SELECT chain_id, token_address, reserve_source, eth_reserve, token_reserve, sqrt_price_x96,
           block_number, block_hash, block_time, tx_hash, log_index
    FROM (
        SELECT trade.chain_id, trade.token_address, 'curve'::TEXT AS reserve_source,
               trade.new_eth_reserve AS eth_reserve, trade.new_token_reserve AS token_reserve,
               NULL::NUMERIC AS sqrt_price_x96,
               trade.block_number, trade.block_hash, trade.block_time, trade.transaction_index,
               trade.tx_hash, trade.log_index
        FROM public.trades AS trade
        WHERE trade.chain_id = rebuild_chain_id
          AND trade.token_address = rebuild_token_address

        UNION ALL

        SELECT sync.chain_id, token.token_address, 'pair'::TEXT,
               CASE WHEN token.token_is_token0 THEN sync.reserve1 ELSE sync.reserve0 END,
               CASE WHEN token.token_is_token0 THEN sync.reserve0 ELSE sync.reserve1 END,
               NULL::NUMERIC,
               sync.block_number, sync.block_hash, sync.block_time, sync.transaction_index,
               sync.tx_hash, sync.log_index
        FROM public.pool_syncs AS sync
        JOIN public.tokens AS token
          ON token.chain_id = sync.chain_id
         AND token.lp_pair = sync.pair_address
         AND token.phase = 'graduated'
        WHERE token.chain_id = rebuild_chain_id
          AND token.token_address = rebuild_token_address

        UNION ALL

        SELECT price.chain_id, price.token_address, 'v4'::TEXT, NULL::NUMERIC, NULL::NUMERIC,
               price.sqrt_price_x96, price.block_number, price.block_hash, price.block_time,
               price.transaction_index, price.tx_hash, price.log_index
        FROM public.v4_price_events AS price
        WHERE price.chain_id = rebuild_chain_id
          AND price.token_address = rebuild_token_address
    ) AS reserve_event
    ORDER BY block_number DESC, transaction_index DESC, log_index DESC
    LIMIT 1;

    DELETE FROM public.holder_balances
    WHERE chain_id = rebuild_chain_id AND token_address = rebuild_token_address;

    INSERT INTO public.holder_balances (
        chain_id, token_address, holder_address, balance, first_acquired_block_number
    )
    WITH raw_deltas AS (
        SELECT transfer.chain_id, transfer.token_address, transfer.from_address AS holder_address,
               transfer.block_number, transfer.transaction_index, transfer.log_index,
               -transfer.value AS delta
        FROM public.transfers AS transfer
        WHERE transfer.chain_id = rebuild_chain_id
          AND transfer.token_address = rebuild_token_address
          AND transfer.from_address <> decode(repeat('00', 20), 'hex')
        UNION ALL
        SELECT transfer.chain_id, transfer.token_address, transfer.to_address,
               transfer.block_number, transfer.transaction_index, transfer.log_index,
               transfer.value
        FROM public.transfers AS transfer
        WHERE transfer.chain_id = rebuild_chain_id
          AND transfer.token_address = rebuild_token_address
    ), event_deltas AS (
        SELECT chain_id, token_address, holder_address, block_number, transaction_index, log_index,
               sum(delta) AS delta
        FROM raw_deltas
        GROUP BY chain_id, token_address, holder_address, block_number, transaction_index, log_index
    ), running AS (
        SELECT event_deltas.*,
               sum(delta) OVER (
                   PARTITION BY chain_id, token_address, holder_address
                   ORDER BY block_number, transaction_index, log_index
                   ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW
               ) AS balance_after
        FROM event_deltas
    )
    SELECT chain_id, token_address, holder_address, max(balance_after) FILTER (WHERE is_last),
           CASE
               WHEN max(balance_after) FILTER (WHERE is_last) = 0 THEN NULL
               ELSE max(block_number) FILTER (WHERE balance_after - delta = 0 AND balance_after > 0)
           END
    FROM (
        SELECT running.*,
               row_number() OVER (
                   PARTITION BY chain_id, token_address, holder_address
                   ORDER BY block_number DESC, transaction_index DESC, log_index DESC
               ) = 1 AS is_last
        FROM running
    ) AS histories
    GROUP BY chain_id, token_address, holder_address;

    DELETE FROM public.candles
    WHERE chain_id = rebuild_chain_id AND token_address = rebuild_token_address;

    INSERT INTO public.candles (
        chain_id, token_address, interval, bucket_start_time,
        open_price_wad, high_price_wad, low_price_wad, close_price_wad,
        gross_eth_volume, token_volume, trade_count
    )
    WITH bucketed AS (
        SELECT market.*,
               interval_definition.interval,
               CASE interval_definition.interval
                   WHEN '1m' THEN date_trunc('minute', market.block_time)
                   WHEN '5m' THEN date_trunc('hour', market.block_time)
                       + floor(extract(minute FROM market.block_time) / 5) * interval '5 minutes'
                   WHEN '1h' THEN date_trunc('hour', market.block_time)
                   WHEN '1d' THEN date_trunc('day', market.block_time)
               END AS bucket_start_time
        FROM public.market_trades AS market
        CROSS JOIN (VALUES ('1m'), ('5m'), ('1h'), ('1d')) AS interval_definition(interval)
        WHERE market.chain_id = rebuild_chain_id
          AND market.token_address = rebuild_token_address
          AND market.execution_price_wad IS NOT NULL
    )
    SELECT chain_id, token_address, interval, bucket_start_time,
           (array_agg(execution_price_wad ORDER BY block_number, transaction_index, log_index))[1],
           max(execution_price_wad), min(execution_price_wad),
           (array_agg(execution_price_wad ORDER BY block_number DESC, transaction_index DESC, log_index DESC))[1],
           sum(gross_eth_volume), sum(token_volume), count(*)::INTEGER
    FROM bucketed
    GROUP BY chain_id, token_address, interval, bucket_start_time;

    DELETE FROM public.creator_fee_balances
    WHERE chain_id = rebuild_chain_id AND token_address = rebuild_token_address;

    INSERT INTO public.creator_fee_balances (
        chain_id, token_address, account, curve_eth_owed, locker_eth_owed, locker_token_owed
    )
    SELECT delta.chain_id, delta.token_address, delta.account, sum(delta.curve_eth_delta),
           sum(delta.locker_eth_delta), sum(delta.locker_token_delta)
    FROM public.creator_fee_deltas AS delta
    WHERE delta.chain_id = rebuild_chain_id
      AND delta.token_address = rebuild_token_address
    GROUP BY delta.chain_id, delta.token_address, delta.account;

    INSERT INTO public.aggregation_dirty (chain_id, token_address, generation)
    VALUES (rebuild_chain_id, rebuild_token_address, nextval('public.aggregation_dirty_generation_seq'))
    ON CONFLICT (chain_id, token_address) DO UPDATE
        SET generation = nextval('public.aggregation_dirty_generation_seq');
END;
$$ LANGUAGE plpgsql SET search_path = pg_catalog SET timezone = 'UTC';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM token_reserves WHERE reserve_source = 'v4')
       OR EXISTS (SELECT 1 FROM creator_fee_balances)
       OR EXISTS (SELECT 1 FROM token_stats WHERE liquidity_eth_wad IS NULL) THEN
        RAISE EXCEPTION 'engine v2 projection rows exist; the engine v1 projections cannot hold them';
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION rebuild_token_projections(rebuild_chain_id BIGINT, rebuild_token_address BYTEA)
RETURNS void AS $$
BEGIN
    INSERT INTO public.tokens (
        chain_id, token_address, curve_address, lp_pair, weth, creator, protocol_treasury,
        engine_version, name, symbol, total_supply, initial_virtual_eth, initial_virtual_token,
        curve_tokens, lp_tokens, graduation_eth, trade_fee_bps, protocol_share_bps,
        launch_block_number, launch_block_hash, launch_block_time, launch_tx_hash, launch_log_index,
        phase, graduation_block_number, graduation_block_hash, graduation_block_time,
        graduation_tx_hash, graduation_log_index
    )
    SELECT
        launch.chain_id, launch.token_address, launch.curve_address, launch.lp_pair, launch.weth,
        launch.creator, launch.protocol_treasury, launch.engine_version, launch.name, launch.symbol,
        launch.total_supply, launch.virtual_eth, launch.virtual_token, launch.curve_tokens,
        launch.lp_tokens, launch.graduation_eth, launch.trade_fee_bps, launch.protocol_share_bps,
        launch.block_number, launch.block_hash, launch.block_time, launch.tx_hash, launch.log_index,
        CASE WHEN graduation.tx_hash IS NULL THEN 'curve' ELSE 'graduated' END,
        graduation.block_number, graduation.block_hash, graduation.block_time,
        graduation.tx_hash, graduation.log_index
    FROM public.token_launches AS launch
    LEFT JOIN LATERAL (
        SELECT candidate.block_number, candidate.block_hash, candidate.block_time,
               candidate.tx_hash, candidate.log_index
        FROM public.graduations AS candidate
        WHERE candidate.chain_id = launch.chain_id
          AND candidate.token_address = launch.token_address
        ORDER BY candidate.block_number DESC, candidate.transaction_index DESC, candidate.log_index DESC
        LIMIT 1
    ) AS graduation ON TRUE
    WHERE launch.chain_id = rebuild_chain_id
      AND launch.token_address = rebuild_token_address
    ON CONFLICT (chain_id, token_address) DO UPDATE SET
        curve_address = EXCLUDED.curve_address,
        lp_pair = EXCLUDED.lp_pair,
        weth = EXCLUDED.weth,
        creator = EXCLUDED.creator,
        protocol_treasury = EXCLUDED.protocol_treasury,
        engine_version = EXCLUDED.engine_version,
        name = EXCLUDED.name,
        symbol = EXCLUDED.symbol,
        total_supply = EXCLUDED.total_supply,
        initial_virtual_eth = EXCLUDED.initial_virtual_eth,
        initial_virtual_token = EXCLUDED.initial_virtual_token,
        curve_tokens = EXCLUDED.curve_tokens,
        lp_tokens = EXCLUDED.lp_tokens,
        graduation_eth = EXCLUDED.graduation_eth,
        trade_fee_bps = EXCLUDED.trade_fee_bps,
        protocol_share_bps = EXCLUDED.protocol_share_bps,
        launch_block_number = EXCLUDED.launch_block_number,
        launch_block_hash = EXCLUDED.launch_block_hash,
        launch_block_time = EXCLUDED.launch_block_time,
        launch_tx_hash = EXCLUDED.launch_tx_hash,
        launch_log_index = EXCLUDED.launch_log_index,
        phase = EXCLUDED.phase,
        graduation_block_number = EXCLUDED.graduation_block_number,
        graduation_block_hash = EXCLUDED.graduation_block_hash,
        graduation_block_time = EXCLUDED.graduation_block_time,
        graduation_tx_hash = EXCLUDED.graduation_tx_hash,
        graduation_log_index = EXCLUDED.graduation_log_index;

    IF NOT FOUND THEN
        RAISE EXCEPTION 'cannot rebuild unknown token (chain_id=%, token=%)',
            rebuild_chain_id, rebuild_token_address;
    END IF;

    DELETE FROM public.token_reserves
    WHERE chain_id = rebuild_chain_id AND token_address = rebuild_token_address;

    INSERT INTO public.token_reserves (
        chain_id, token_address, reserve_source, eth_reserve, token_reserve,
        source_block_number, source_block_hash, source_block_time, source_tx_hash, source_log_index
    )
    SELECT chain_id, token_address, reserve_source, eth_reserve, token_reserve,
           block_number, block_hash, block_time, tx_hash, log_index
    FROM (
        SELECT trade.chain_id, trade.token_address, 'curve'::TEXT AS reserve_source,
               trade.new_eth_reserve AS eth_reserve, trade.new_token_reserve AS token_reserve,
               trade.block_number, trade.block_hash, trade.block_time, trade.transaction_index,
               trade.tx_hash, trade.log_index
        FROM public.trades AS trade
        WHERE trade.chain_id = rebuild_chain_id
          AND trade.token_address = rebuild_token_address

        UNION ALL

        SELECT sync.chain_id, token.token_address, 'pair'::TEXT,
               CASE WHEN token.token_is_token0 THEN sync.reserve1 ELSE sync.reserve0 END,
               CASE WHEN token.token_is_token0 THEN sync.reserve0 ELSE sync.reserve1 END,
               sync.block_number, sync.block_hash, sync.block_time, sync.transaction_index,
               sync.tx_hash, sync.log_index
        FROM public.pool_syncs AS sync
        JOIN public.tokens AS token
          ON token.chain_id = sync.chain_id
         AND token.lp_pair = sync.pair_address
         AND token.phase = 'graduated'
        WHERE token.chain_id = rebuild_chain_id
          AND token.token_address = rebuild_token_address
    ) AS reserve_event
    ORDER BY block_number DESC, transaction_index DESC, log_index DESC
    LIMIT 1;

    DELETE FROM public.holder_balances
    WHERE chain_id = rebuild_chain_id AND token_address = rebuild_token_address;

    INSERT INTO public.holder_balances (
        chain_id, token_address, holder_address, balance, first_acquired_block_number
    )
    WITH raw_deltas AS (
        SELECT transfer.chain_id, transfer.token_address, transfer.from_address AS holder_address,
               transfer.block_number, transfer.transaction_index, transfer.log_index,
               -transfer.value AS delta
        FROM public.transfers AS transfer
        WHERE transfer.chain_id = rebuild_chain_id
          AND transfer.token_address = rebuild_token_address
          AND transfer.from_address <> decode(repeat('00', 20), 'hex')
        UNION ALL
        SELECT transfer.chain_id, transfer.token_address, transfer.to_address,
               transfer.block_number, transfer.transaction_index, transfer.log_index,
               transfer.value
        FROM public.transfers AS transfer
        WHERE transfer.chain_id = rebuild_chain_id
          AND transfer.token_address = rebuild_token_address
    ), event_deltas AS (
        SELECT chain_id, token_address, holder_address, block_number, transaction_index, log_index,
               sum(delta) AS delta
        FROM raw_deltas
        GROUP BY chain_id, token_address, holder_address, block_number, transaction_index, log_index
    ), running AS (
        SELECT event_deltas.*,
               sum(delta) OVER (
                   PARTITION BY chain_id, token_address, holder_address
                   ORDER BY block_number, transaction_index, log_index
                   ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW
               ) AS balance_after
        FROM event_deltas
    )
    SELECT chain_id, token_address, holder_address, max(balance_after) FILTER (WHERE is_last),
           CASE
               WHEN max(balance_after) FILTER (WHERE is_last) = 0 THEN NULL
               ELSE max(block_number) FILTER (WHERE balance_after - delta = 0 AND balance_after > 0)
           END
    FROM (
        SELECT running.*,
               row_number() OVER (
                   PARTITION BY chain_id, token_address, holder_address
                   ORDER BY block_number DESC, transaction_index DESC, log_index DESC
               ) = 1 AS is_last
        FROM running
    ) AS histories
    GROUP BY chain_id, token_address, holder_address;

    DELETE FROM public.candles
    WHERE chain_id = rebuild_chain_id AND token_address = rebuild_token_address;

    INSERT INTO public.candles (
        chain_id, token_address, interval, bucket_start_time,
        open_price_wad, high_price_wad, low_price_wad, close_price_wad,
        gross_eth_volume, token_volume, trade_count
    )
    WITH bucketed AS (
        SELECT market.*,
               interval_definition.interval,
               CASE interval_definition.interval
                   WHEN '1m' THEN date_trunc('minute', market.block_time)
                   WHEN '5m' THEN date_trunc('hour', market.block_time)
                       + floor(extract(minute FROM market.block_time) / 5) * interval '5 minutes'
                   WHEN '1h' THEN date_trunc('hour', market.block_time)
                   WHEN '1d' THEN date_trunc('day', market.block_time)
               END AS bucket_start_time
        FROM public.market_trades AS market
        CROSS JOIN (VALUES ('1m'), ('5m'), ('1h'), ('1d')) AS interval_definition(interval)
        WHERE market.chain_id = rebuild_chain_id
          AND market.token_address = rebuild_token_address
          AND market.execution_price_wad IS NOT NULL
    )
    SELECT chain_id, token_address, interval, bucket_start_time,
           (array_agg(execution_price_wad ORDER BY block_number, transaction_index, log_index))[1],
           max(execution_price_wad), min(execution_price_wad),
           (array_agg(execution_price_wad ORDER BY block_number DESC, transaction_index DESC, log_index DESC))[1],
           sum(gross_eth_volume), sum(token_volume), count(*)::INTEGER
    FROM bucketed
    GROUP BY chain_id, token_address, interval, bucket_start_time;

    INSERT INTO public.aggregation_dirty (chain_id, token_address, generation)
    VALUES (rebuild_chain_id, rebuild_token_address, nextval('public.aggregation_dirty_generation_seq'))
    ON CONFLICT (chain_id, token_address) DO UPDATE
        SET generation = nextval('public.aggregation_dirty_generation_seq');
END;
$$ LANGUAGE plpgsql SET search_path = pg_catalog;
-- +goose StatementEnd

ALTER FUNCTION rebuild_token_projections(BIGINT, BYTEA) SET timezone = 'UTC';

CREATE OR REPLACE VIEW market_trades AS
SELECT
    trade.chain_id,
    trade.token_address,
    trade.block_number,
    trade.block_time,
    trade.transaction_index,
    trade.tx_hash,
    trade.log_index,
    'curve'::TEXT AS source,
    trade.is_buy AS side_buy,
    trade.trader,
    trunc(trade.eth_gross * 1000000000000000000::NUMERIC / NULLIF(trade.token_amount, 0))::NUMERIC(78,0)
        AS execution_price_wad,
    trunc(trade.new_eth_reserve * 1000000000000000000::NUMERIC / NULLIF(trade.new_token_reserve, 0))::NUMERIC(78,0)
        AS spot_price_wad,
    trade.eth_gross AS gross_eth_volume,
    trade.token_amount AS token_volume,
    block.finality_status AS finality
FROM trades AS trade
JOIN indexed_blocks AS block
  ON block.chain_id = trade.chain_id
 AND block.block_number = trade.block_number

UNION ALL

SELECT
    swap.chain_id,
    token.token_address,
    swap.block_number,
    swap.block_time,
    swap.transaction_index,
    swap.tx_hash,
    swap.log_index,
    'dex'::TEXT AS source,
    CASE WHEN token.token_is_token0 THEN swap.amount0_out > 0 ELSE swap.amount1_out > 0 END AS side_buy,
    swap.tx_from AS trader,
    trunc(legs.eth_volume * 1000000000000000000::NUMERIC / NULLIF(legs.token_volume, 0))::NUMERIC(78,0)
        AS execution_price_wad,
    trunc(legs.sync_eth_reserve * 1000000000000000000::NUMERIC / NULLIF(legs.sync_token_reserve, 0))::NUMERIC(78,0)
        AS spot_price_wad,
    legs.eth_volume AS gross_eth_volume,
    legs.token_volume,
    block.finality_status AS finality
FROM pool_swaps AS swap
JOIN tokens AS token
  ON token.chain_id = swap.chain_id
 AND token.lp_pair = swap.pair_address
JOIN indexed_blocks AS block
  ON block.chain_id = swap.chain_id
 AND block.block_number = swap.block_number
JOIN LATERAL (
    SELECT
        CASE
            WHEN token.token_is_token0 THEN swap.amount1_in + swap.amount1_out
            ELSE swap.amount0_in + swap.amount0_out
        END AS eth_volume,
        CASE
            WHEN token.token_is_token0 THEN swap.amount0_in + swap.amount0_out
            ELSE swap.amount1_in + swap.amount1_out
        END AS token_volume,
        CASE WHEN token.token_is_token0 THEN sync.reserve1 ELSE sync.reserve0 END AS sync_eth_reserve,
        CASE WHEN token.token_is_token0 THEN sync.reserve0 ELSE sync.reserve1 END AS sync_token_reserve
    FROM pool_syncs AS sync
    WHERE sync.chain_id = swap.chain_id
      AND sync.pair_address = swap.pair_address
      AND sync.tx_hash = swap.tx_hash
      AND sync.log_index < swap.log_index
    ORDER BY sync.log_index DESC
    LIMIT 1
) AS legs ON TRUE;

DROP VIEW v4_price_events;
DROP VIEW creator_fee_deltas;
DROP FUNCTION creator_fee_recipient_at(BIGINT, BYTEA, BIGINT, INTEGER, INTEGER);
DROP FUNCTION token_holder_excluded(INTEGER, BYTEA, BYTEA, BYTEA, BYTEA, BYTEA);
DROP FUNCTION v4_spot_price_wad(NUMERIC);
DROP TABLE creator_fee_balances;
ALTER TABLE token_stats ALTER COLUMN liquidity_eth_wad SET NOT NULL;
ALTER TABLE token_reserves
    DROP CONSTRAINT token_reserves_source_shape,
    DROP CONSTRAINT token_reserves_sqrt_price_x96_positive,
    DROP CONSTRAINT token_reserves_source_valid,
    ADD CONSTRAINT token_reserves_source_valid CHECK (reserve_source IN ('curve', 'pair')),
    DROP COLUMN sqrt_price_x96,
    ALTER COLUMN eth_reserve SET NOT NULL,
    ALTER COLUMN token_reserve SET NOT NULL;
DROP TABLE v4_pool_managers;

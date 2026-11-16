-- +goose Up
-- AUDIT_REPORT_TR follow-ups (docs/specs/2026-11-16-audit-med-fixes-preflight.md).

-- MED-11: the market price of a curve trade excludes the engine-2 anti-snipe tax. Every view
-- column keeps its name and type.
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
    -- The engine-2 anti-snipe tax is a separate charge, not price: up to 90 % of eth_gross in the
    -- first seconds, which made a 10x candle wick and all-time high (AUDIT_REPORT_TR MED-11).
    trunc((trade.eth_gross - trade.snipe_tax) * 1000000000000000000::NUMERIC / NULLIF(trade.token_amount, 0))::NUMERIC(78,0)
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

-- Rebuild the candles of every token with a taxed trade. The all-time high only ratchets up
-- from the previous token_stats row, so that row is dropped first; rebuild_token_projections
-- marks the token dirty and the stats worker writes it again from the corrected candles.
DELETE FROM token_stats AS stats
USING (SELECT DISTINCT chain_id, token_address FROM trades WHERE snipe_tax > 0) AS taxed
WHERE stats.chain_id = taxed.chain_id AND stats.token_address = taxed.token_address;
SELECT rebuild_token_projections(taxed.chain_id, taxed.token_address)
FROM (SELECT DISTINCT chain_id, token_address FROM trades WHERE snipe_tax > 0) AS taxed;

-- HIGH-13 d: reorg recovery deletes and reads transfers by block; without this index each
-- statement was one skip-scan search per token.
CREATE INDEX transfers_chain_block_idx ON transfers (chain_id, block_number);

-- HIGH-13 e: per-token lookups and the token_launches foreign-key checks on reorg deletes read
-- these tables by token; their only index was the (chain_id, tx_hash, log_index) key.
CREATE INDEX creator_fee_claims_token_idx ON creator_fee_claims (chain_id, token_address);
CREATE INDEX protocol_fee_claims_token_idx ON protocol_fee_claims (chain_id, token_address);
CREATE INDEX refund_credits_token_idx ON refund_credits (chain_id, token_address);
CREATE INDEX refund_claims_token_idx ON refund_claims (chain_id, token_address);
CREATE INDEX fee_recipient_proposals_token_idx ON fee_recipient_proposals (chain_id, token_address);
CREATE INDEX fee_recipient_acceptances_token_idx ON fee_recipient_acceptances (chain_id, token_address);
CREATE INDEX lp_fee_collections_token_idx ON lp_fee_collections (chain_id, token_address);
CREATE INDEX lp_fee_claims_token_idx ON lp_fee_claims (chain_id, token_address);

-- +goose Down
DROP INDEX IF EXISTS creator_fee_claims_token_idx;
DROP INDEX IF EXISTS protocol_fee_claims_token_idx;
DROP INDEX IF EXISTS refund_credits_token_idx;
DROP INDEX IF EXISTS refund_claims_token_idx;
DROP INDEX IF EXISTS fee_recipient_proposals_token_idx;
DROP INDEX IF EXISTS fee_recipient_acceptances_token_idx;
DROP INDEX IF EXISTS lp_fee_collections_token_idx;
DROP INDEX IF EXISTS lp_fee_claims_token_idx;
DROP INDEX IF EXISTS transfers_chain_block_idx;

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

-- +goose Up
-- A pool swap's trader is the transaction sender, not the router-facing Swap.sender. NULL until
-- the sender fetch (or a later backfill) fills it.
ALTER TABLE pool_swaps ADD COLUMN tx_from BYTEA NULL;
ALTER TABLE pool_swaps ADD CONSTRAINT pool_swaps_tx_from_length
    CHECK (tx_from IS NULL OR octet_length(tx_from) = 20);

CREATE SEQUENCE position_dirty_generation_seq;

-- No FK to tokens: a wallet pair must survive (and be recomputed to empty) when a reorg deletes
-- its token row.
CREATE TABLE position_dirty (
    chain_id BIGINT NOT NULL,
    wallet BYTEA NOT NULL,
    token_address BYTEA NOT NULL,
    generation BIGINT NOT NULL,
    claimed_generation BIGINT,
    claimed_at TIMESTAMPTZ,
    claimed_by TEXT,
    CONSTRAINT position_dirty_pkey PRIMARY KEY (chain_id, wallet, token_address),
    CONSTRAINT position_dirty_chain_id_positive CHECK (chain_id > 0),
    CONSTRAINT position_dirty_wallet_length CHECK (octet_length(wallet) = 20),
    CONSTRAINT position_dirty_token_address_length CHECK (octet_length(token_address) = 20),
    CONSTRAINT position_dirty_generation_nonnegative CHECK (generation >= 0),
    CONSTRAINT position_dirty_claimed_generation_nonnegative CHECK (claimed_generation >= 0),
    CONSTRAINT position_dirty_claim_together CHECK (
        (claimed_generation IS NULL AND claimed_at IS NULL AND claimed_by IS NULL)
        OR (claimed_generation IS NOT NULL AND claimed_at IS NOT NULL AND claimed_by IS NOT NULL)
    ),
    CONSTRAINT position_dirty_claim_not_ahead CHECK (
        claimed_generation IS NULL OR claimed_generation <= generation
    )
);

-- realized_pnl is signed, so it is NUMERIC(79,0): the Uint256 codec that every NUMERIC(78,0) column
-- uses rejects negatives, so the column stays a pgtype.Numeric.
CREATE TABLE wallet_positions (
    chain_id BIGINT NOT NULL,
    wallet BYTEA NOT NULL,
    token_address BYTEA NOT NULL,
    balance NUMERIC(78,0) NOT NULL,
    remaining_cost NUMERIC(78,0) NOT NULL,
    realized_pnl NUMERIC(79,0) NOT NULL,
    transferred_in NUMERIC(78,0) NOT NULL,
    eth_paid NUMERIC(78,0) NOT NULL,
    eth_received NUMERIC(78,0) NOT NULL,
    trade_count BIGINT NOT NULL,
    first_block_number BIGINT NOT NULL,
    last_block_number BIGINT NOT NULL,
    CONSTRAINT wallet_positions_pkey PRIMARY KEY (chain_id, wallet, token_address),
    CONSTRAINT wallet_positions_chain_id_positive CHECK (chain_id > 0),
    CONSTRAINT wallet_positions_wallet_length CHECK (octet_length(wallet) = 20),
    CONSTRAINT wallet_positions_token_address_length CHECK (octet_length(token_address) = 20),
    CONSTRAINT wallet_positions_balance_nonnegative CHECK (balance >= 0),
    CONSTRAINT wallet_positions_remaining_cost_nonnegative CHECK (remaining_cost >= 0),
    CONSTRAINT wallet_positions_transferred_in_nonnegative CHECK (transferred_in >= 0),
    CONSTRAINT wallet_positions_eth_paid_nonnegative CHECK (eth_paid >= 0),
    CONSTRAINT wallet_positions_eth_received_nonnegative CHECK (eth_received >= 0),
    CONSTRAINT wallet_positions_trade_count_nonnegative CHECK (trade_count >= 0),
    CONSTRAINT wallet_positions_first_block_nonnegative CHECK (first_block_number >= 0),
    CONSTRAINT wallet_positions_last_block_nonnegative CHECK (last_block_number >= first_block_number)
);

CREATE TABLE wallet_totals (
    chain_id BIGINT NOT NULL,
    wallet BYTEA NOT NULL,
    realized_pnl NUMERIC(79,0) NOT NULL,
    tokens_traded BIGINT NOT NULL,
    held_count BIGINT NOT NULL,
    first_block_number BIGINT NOT NULL,
    last_block_number BIGINT NOT NULL,
    CONSTRAINT wallet_totals_pkey PRIMARY KEY (chain_id, wallet),
    CONSTRAINT wallet_totals_chain_id_positive CHECK (chain_id > 0),
    CONSTRAINT wallet_totals_wallet_length CHECK (octet_length(wallet) = 20),
    CONSTRAINT wallet_totals_tokens_traded_nonnegative CHECK (tokens_traded >= 0),
    CONSTRAINT wallet_totals_held_count_nonnegative CHECK (held_count >= 0),
    CONSTRAINT wallet_totals_first_block_nonnegative CHECK (first_block_number >= 0),
    CONSTRAINT wallet_totals_last_block_nonnegative CHECK (last_block_number >= first_block_number)
);

CREATE INDEX trades_trader_order_idx
    ON trades (chain_id, trader, block_number DESC, transaction_index DESC, log_index DESC);
CREATE INDEX pool_swaps_tx_from_order_idx
    ON pool_swaps (chain_id, tx_from, block_number DESC, transaction_index DESC, log_index DESC);
CREATE INDEX pool_swaps_tx_from_null_idx ON pool_swaps (chain_id) WHERE tx_from IS NULL;
CREATE INDEX transfers_to_token_idx ON transfers (chain_id, to_address, token_address);
CREATE INDEX transfers_from_token_idx ON transfers (chain_id, from_address, token_address);
CREATE INDEX tokens_creator_launch_idx ON tokens (chain_id, creator, launch_block_number DESC);
CREATE INDEX wallet_positions_wallet_idx ON wallet_positions (chain_id, wallet);
CREATE INDEX wallet_totals_leaderboard_idx ON wallet_totals (chain_id, realized_pnl DESC, wallet);

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

-- +goose Down
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
    NULL::BYTEA AS trader,
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

DROP INDEX wallet_totals_leaderboard_idx;
DROP INDEX wallet_positions_wallet_idx;
DROP INDEX tokens_creator_launch_idx;
DROP INDEX transfers_from_token_idx;
DROP INDEX transfers_to_token_idx;
DROP INDEX pool_swaps_tx_from_null_idx;
DROP INDEX pool_swaps_tx_from_order_idx;
DROP INDEX trades_trader_order_idx;
DROP TABLE wallet_totals;
DROP TABLE wallet_positions;
DROP TABLE position_dirty;
DROP SEQUENCE position_dirty_generation_seq;
ALTER TABLE pool_swaps DROP COLUMN tx_from;

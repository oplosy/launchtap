-- +goose Up
-- Engine v2 ledger (Plan v2-B Task 5). A v2 launch has no Uniswap V2 pair or WETH: it graduates
-- into a native-ETH Uniswap V4 pool guarded by the launch hook, with the full-range position held
-- by the LP fee locker. The v1 tables are reused where the event is unchanged; engine CHECKs keep
-- each row's shape tied to its engine so a v1 row can never lose its pair.
ALTER TABLE token_launches
    ALTER COLUMN lp_pair DROP NOT NULL,
    ALTER COLUMN weth DROP NOT NULL,
    ADD COLUMN hook BYTEA,
    ADD COLUMN locker BYTEA,
    ADD COLUMN anti_snipe_start_bps INTEGER,
    ADD COLUMN anti_snipe_duration_seconds BIGINT,
    ADD COLUMN lp_creator_share_bps INTEGER,
    ADD CONSTRAINT token_launches_hook_length CHECK (octet_length(hook) = 20),
    ADD CONSTRAINT token_launches_locker_length CHECK (octet_length(locker) = 20),
    ADD CONSTRAINT token_launches_anti_snipe_start_bps_uint16 CHECK (anti_snipe_start_bps BETWEEN 0 AND 65535),
    ADD CONSTRAINT token_launches_anti_snipe_duration_seconds_uint32
        CHECK (anti_snipe_duration_seconds BETWEEN 0 AND 4294967295),
    ADD CONSTRAINT token_launches_lp_creator_share_bps_uint16 CHECK (lp_creator_share_bps BETWEEN 0 AND 65535),
    ADD CONSTRAINT token_launches_engine_pair CHECK (
        (engine_version = 1 AND lp_pair IS NOT NULL AND weth IS NOT NULL)
        OR (engine_version <> 1 AND lp_pair IS NULL AND weth IS NULL)
    ),
    ADD CONSTRAINT token_launches_engine_v2_fields CHECK (
        (engine_version = 2 AND hook IS NOT NULL AND locker IS NOT NULL AND anti_snipe_start_bps IS NOT NULL AND anti_snipe_duration_seconds IS NOT NULL AND lp_creator_share_bps IS NOT NULL)
        OR (engine_version <> 2 AND hook IS NULL AND locker IS NULL AND anti_snipe_start_bps IS NULL AND anti_snipe_duration_seconds IS NULL AND lp_creator_share_bps IS NULL)
    );

ALTER TABLE tokens
    ALTER COLUMN lp_pair DROP NOT NULL,
    ALTER COLUMN weth DROP NOT NULL,
    ADD COLUMN hook BYTEA,
    ADD COLUMN locker BYTEA,
    ADD COLUMN anti_snipe_start_bps INTEGER,
    ADD COLUMN anti_snipe_duration_seconds BIGINT,
    ADD COLUMN lp_creator_share_bps INTEGER,
    ADD CONSTRAINT tokens_hook_length CHECK (octet_length(hook) = 20),
    ADD CONSTRAINT tokens_locker_length CHECK (octet_length(locker) = 20),
    ADD CONSTRAINT tokens_anti_snipe_start_bps_uint16 CHECK (anti_snipe_start_bps BETWEEN 0 AND 65535),
    ADD CONSTRAINT tokens_anti_snipe_duration_seconds_uint32
        CHECK (anti_snipe_duration_seconds BETWEEN 0 AND 4294967295),
    ADD CONSTRAINT tokens_lp_creator_share_bps_uint16 CHECK (lp_creator_share_bps BETWEEN 0 AND 65535),
    ADD CONSTRAINT tokens_engine_pair CHECK (
        (engine_version = 1 AND lp_pair IS NOT NULL AND weth IS NOT NULL)
        OR (engine_version <> 1 AND lp_pair IS NULL AND weth IS NULL)
    ),
    ADD CONSTRAINT tokens_engine_v2_fields CHECK (
        (engine_version = 2 AND hook IS NOT NULL AND locker IS NOT NULL AND anti_snipe_start_bps IS NOT NULL AND anti_snipe_duration_seconds IS NOT NULL AND lp_creator_share_bps IS NOT NULL)
        OR (engine_version <> 2 AND hook IS NULL AND locker IS NULL AND anti_snipe_start_bps IS NULL AND anti_snipe_duration_seconds IS NULL AND lp_creator_share_bps IS NULL)
    );

-- Native ETH is currency0 of every v2 pool, so a v2 token (no WETH) is never token0.
ALTER TABLE tokens
    ALTER COLUMN token_is_token0 SET EXPRESSION AS (COALESCE(token_address < weth, FALSE));

-- Every v2 buy may pay the anti-snipe tax, included in eth_gross. A sell never pays it.
ALTER TABLE trades
    ADD COLUMN snipe_tax NUMERIC(78,0) NOT NULL DEFAULT 0,
    ADD CONSTRAINT trades_snipe_tax_nonnegative CHECK (snipe_tax >= 0),
    ADD CONSTRAINT trades_snipe_tax_buy_only CHECK (is_buy OR snipe_tax = 0),
    ADD CONSTRAINT trades_snipe_tax_within_gross CHECK (snipe_tax <= eth_gross);

-- A v1 graduation burns pair liquidity; a v2 graduation mints a locked V4 position.
-- graduations_engine_shape keeps each row internally consistent; which shape a row must have
-- depends on its token's engine, which a table CHECK cannot read, so a deferred constraint
-- trigger (below) ties graduations and trades to their launch's engine at commit.
ALTER TABLE graduations
    ALTER COLUMN lp_pair DROP NOT NULL,
    ALTER COLUMN lp_liquidity_burned DROP NOT NULL,
    ADD COLUMN pool_id BYTEA,
    ADD COLUMN position_token_id NUMERIC(78,0),
    ADD COLUMN liquidity NUMERIC(78,0),
    ADD COLUMN sqrt_price_x96 NUMERIC(78,0),
    ADD COLUMN eth_remainder NUMERIC(78,0),
    ADD COLUMN token_remainder NUMERIC(78,0),
    ADD CONSTRAINT graduations_pool_id_length CHECK (octet_length(pool_id) = 32),
    ADD CONSTRAINT graduations_position_token_id_nonnegative CHECK (position_token_id >= 0),
    ADD CONSTRAINT graduations_liquidity_nonnegative CHECK (liquidity >= 0),
    ADD CONSTRAINT graduations_sqrt_price_x96_positive CHECK (sqrt_price_x96 > 0),
    ADD CONSTRAINT graduations_eth_remainder_nonnegative CHECK (eth_remainder >= 0),
    ADD CONSTRAINT graduations_token_remainder_nonnegative CHECK (token_remainder >= 0),
    ADD CONSTRAINT graduations_engine_shape CHECK (
        (lp_pair IS NOT NULL AND lp_liquidity_burned IS NOT NULL
            AND pool_id IS NULL AND position_token_id IS NULL AND liquidity IS NULL AND sqrt_price_x96 IS NULL AND eth_remainder IS NULL AND token_remainder IS NULL)
        OR (lp_pair IS NULL AND lp_liquidity_burned IS NULL
            AND pool_id IS NOT NULL AND position_token_id IS NOT NULL AND liquidity IS NOT NULL AND sqrt_price_x96 IS NOT NULL AND eth_remainder IS NOT NULL AND token_remainder IS NOT NULL)
    ),
    ADD CONSTRAINT graduations_chain_pool_key UNIQUE (chain_id, pool_id);

-- +goose StatementBegin
CREATE FUNCTION graduations_match_launch_engine() RETURNS trigger AS $$
DECLARE
    launch_engine INTEGER;
BEGIN
    SELECT launch.engine_version INTO launch_engine
    FROM public.token_launches AS launch
    WHERE launch.chain_id = NEW.chain_id AND launch.token_address = NEW.token_address;
    -- A missing launch is reported by the deferred token_launch_fk instead.
    -- Only an engine-1 graduation has a pair; every other engine graduates into a V4 pool.
    IF launch_engine IS NOT NULL AND (NEW.lp_pair IS NOT NULL) <> (launch_engine = 1) THEN
        RAISE EXCEPTION
            'graduations row (chain_id=%, token=%, tx=%, log=%) does not match its launch engine %',
            NEW.chain_id, NEW.token_address, NEW.tx_hash, NEW.log_index, launch_engine;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql SET search_path = pg_catalog;
-- +goose StatementEnd

CREATE CONSTRAINT TRIGGER graduations_match_launch_engine_trigger
    AFTER INSERT OR UPDATE ON graduations
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION graduations_match_launch_engine();

-- +goose StatementBegin
CREATE FUNCTION trades_match_launch_engine() RETURNS trigger AS $$
DECLARE
    launch_engine INTEGER;
BEGIN
    SELECT launch.engine_version INTO launch_engine
    FROM public.token_launches AS launch
    WHERE launch.chain_id = NEW.chain_id AND launch.token_address = NEW.token_address;
    -- A missing launch is reported by the deferred token_launch_fk instead.
    -- Only engine 2 levies the anti-snipe tax.
    IF launch_engine IS NOT NULL AND launch_engine <> 2 AND NEW.snipe_tax <> 0 THEN
        RAISE EXCEPTION
            'trades row (chain_id=%, token=%, tx=%, log=%) does not match its launch engine %',
            NEW.chain_id, NEW.token_address, NEW.tx_hash, NEW.log_index, launch_engine;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql SET search_path = pg_catalog;
-- +goose StatementEnd

CREATE CONSTRAINT TRIGGER trades_match_launch_engine_trigger
    AFTER INSERT OR UPDATE ON trades
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION trades_match_launch_engine();

-- v2-only ledger tables. V4 pool events reference their pool's graduation: the pool is created in
-- the graduation transaction and only pools graduated by this deployment are indexed.
CREATE TABLE snipe_tax_claims (
    chain_id BIGINT NOT NULL,
    block_number BIGINT NOT NULL,
    block_hash BYTEA NOT NULL,
    block_time TIMESTAMPTZ NOT NULL,
    transaction_index INTEGER NOT NULL,
    tx_hash BYTEA NOT NULL,
    log_index INTEGER NOT NULL,
    token_address BYTEA NOT NULL,
    treasury BYTEA NOT NULL,
    amount NUMERIC(78,0) NOT NULL,
    CONSTRAINT snipe_tax_claims_pkey PRIMARY KEY (chain_id, tx_hash, log_index),
    CONSTRAINT snipe_tax_claims_chain_id_positive CHECK (chain_id > 0),
    CONSTRAINT snipe_tax_claims_block_number_nonnegative CHECK (block_number >= 0),
    CONSTRAINT snipe_tax_claims_block_hash_length CHECK (octet_length(block_hash) = 32),
    CONSTRAINT snipe_tax_claims_transaction_index_nonnegative CHECK (transaction_index >= 0),
    CONSTRAINT snipe_tax_claims_tx_hash_length CHECK (octet_length(tx_hash) = 32),
    CONSTRAINT snipe_tax_claims_log_index_nonnegative CHECK (log_index >= 0),
    CONSTRAINT snipe_tax_claims_token_address_length CHECK (octet_length(token_address) = 20),
    CONSTRAINT snipe_tax_claims_treasury_length CHECK (octet_length(treasury) = 20),
    CONSTRAINT snipe_tax_claims_amount_nonnegative CHECK (amount >= 0),
    CONSTRAINT snipe_tax_claims_block_fk FOREIGN KEY (chain_id, block_number, block_hash, block_time)
        REFERENCES indexed_blocks (chain_id, block_number, block_hash, block_time)
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT snipe_tax_claims_token_launch_fk FOREIGN KEY (chain_id, token_address)
        REFERENCES token_launches (chain_id, token_address)
        DEFERRABLE INITIALLY DEFERRED
);

CREATE TABLE fee_recipient_proposals (
    chain_id BIGINT NOT NULL,
    block_number BIGINT NOT NULL,
    block_hash BYTEA NOT NULL,
    block_time TIMESTAMPTZ NOT NULL,
    transaction_index INTEGER NOT NULL,
    tx_hash BYTEA NOT NULL,
    log_index INTEGER NOT NULL,
    token_address BYTEA NOT NULL,
    current_recipient BYTEA NOT NULL,
    next_recipient BYTEA NOT NULL,
    CONSTRAINT fee_recipient_proposals_pkey PRIMARY KEY (chain_id, tx_hash, log_index),
    CONSTRAINT fee_recipient_proposals_chain_id_positive CHECK (chain_id > 0),
    CONSTRAINT fee_recipient_proposals_block_number_nonnegative CHECK (block_number >= 0),
    CONSTRAINT fee_recipient_proposals_block_hash_length CHECK (octet_length(block_hash) = 32),
    CONSTRAINT fee_recipient_proposals_transaction_index_nonnegative CHECK (transaction_index >= 0),
    CONSTRAINT fee_recipient_proposals_tx_hash_length CHECK (octet_length(tx_hash) = 32),
    CONSTRAINT fee_recipient_proposals_log_index_nonnegative CHECK (log_index >= 0),
    CONSTRAINT fee_recipient_proposals_token_address_length CHECK (octet_length(token_address) = 20),
    CONSTRAINT fee_recipient_proposals_current_recipient_length CHECK (octet_length(current_recipient) = 20),
    CONSTRAINT fee_recipient_proposals_next_recipient_length CHECK (octet_length(next_recipient) = 20),
    CONSTRAINT fee_recipient_proposals_block_fk FOREIGN KEY (chain_id, block_number, block_hash, block_time)
        REFERENCES indexed_blocks (chain_id, block_number, block_hash, block_time)
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT fee_recipient_proposals_token_launch_fk FOREIGN KEY (chain_id, token_address)
        REFERENCES token_launches (chain_id, token_address)
        DEFERRABLE INITIALLY DEFERRED
);

CREATE TABLE fee_recipient_acceptances (
    chain_id BIGINT NOT NULL,
    block_number BIGINT NOT NULL,
    block_hash BYTEA NOT NULL,
    block_time TIMESTAMPTZ NOT NULL,
    transaction_index INTEGER NOT NULL,
    tx_hash BYTEA NOT NULL,
    log_index INTEGER NOT NULL,
    token_address BYTEA NOT NULL,
    previous_recipient BYTEA NOT NULL,
    next_recipient BYTEA NOT NULL,
    CONSTRAINT fee_recipient_acceptances_pkey PRIMARY KEY (chain_id, tx_hash, log_index),
    CONSTRAINT fee_recipient_acceptances_chain_id_positive CHECK (chain_id > 0),
    CONSTRAINT fee_recipient_acceptances_block_number_nonnegative CHECK (block_number >= 0),
    CONSTRAINT fee_recipient_acceptances_block_hash_length CHECK (octet_length(block_hash) = 32),
    CONSTRAINT fee_recipient_acceptances_transaction_index_nonnegative CHECK (transaction_index >= 0),
    CONSTRAINT fee_recipient_acceptances_tx_hash_length CHECK (octet_length(tx_hash) = 32),
    CONSTRAINT fee_recipient_acceptances_log_index_nonnegative CHECK (log_index >= 0),
    CONSTRAINT fee_recipient_acceptances_token_address_length CHECK (octet_length(token_address) = 20),
    CONSTRAINT fee_recipient_acceptances_previous_recipient_length CHECK (octet_length(previous_recipient) = 20),
    CONSTRAINT fee_recipient_acceptances_next_recipient_length CHECK (octet_length(next_recipient) = 20),
    CONSTRAINT fee_recipient_acceptances_block_fk FOREIGN KEY (chain_id, block_number, block_hash, block_time)
        REFERENCES indexed_blocks (chain_id, block_number, block_hash, block_time)
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT fee_recipient_acceptances_token_launch_fk FOREIGN KEY (chain_id, token_address)
        REFERENCES token_launches (chain_id, token_address)
        DEFERRABLE INITIALLY DEFERRED
);

CREATE TABLE lp_fee_collections (
    chain_id BIGINT NOT NULL,
    block_number BIGINT NOT NULL,
    block_hash BYTEA NOT NULL,
    block_time TIMESTAMPTZ NOT NULL,
    transaction_index INTEGER NOT NULL,
    tx_hash BYTEA NOT NULL,
    log_index INTEGER NOT NULL,
    token_address BYTEA NOT NULL,
    eth_amount NUMERIC(78,0) NOT NULL,
    token_amount NUMERIC(78,0) NOT NULL,
    CONSTRAINT lp_fee_collections_pkey PRIMARY KEY (chain_id, tx_hash, log_index),
    CONSTRAINT lp_fee_collections_chain_id_positive CHECK (chain_id > 0),
    CONSTRAINT lp_fee_collections_block_number_nonnegative CHECK (block_number >= 0),
    CONSTRAINT lp_fee_collections_block_hash_length CHECK (octet_length(block_hash) = 32),
    CONSTRAINT lp_fee_collections_transaction_index_nonnegative CHECK (transaction_index >= 0),
    CONSTRAINT lp_fee_collections_tx_hash_length CHECK (octet_length(tx_hash) = 32),
    CONSTRAINT lp_fee_collections_log_index_nonnegative CHECK (log_index >= 0),
    CONSTRAINT lp_fee_collections_token_address_length CHECK (octet_length(token_address) = 20),
    CONSTRAINT lp_fee_collections_eth_amount_nonnegative CHECK (eth_amount >= 0),
    CONSTRAINT lp_fee_collections_token_amount_nonnegative CHECK (token_amount >= 0),
    CONSTRAINT lp_fee_collections_block_fk FOREIGN KEY (chain_id, block_number, block_hash, block_time)
        REFERENCES indexed_blocks (chain_id, block_number, block_hash, block_time)
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT lp_fee_collections_token_launch_fk FOREIGN KEY (chain_id, token_address)
        REFERENCES token_launches (chain_id, token_address)
        DEFERRABLE INITIALLY DEFERRED
);

CREATE TABLE lp_fee_claims (
    chain_id BIGINT NOT NULL,
    block_number BIGINT NOT NULL,
    block_hash BYTEA NOT NULL,
    block_time TIMESTAMPTZ NOT NULL,
    transaction_index INTEGER NOT NULL,
    tx_hash BYTEA NOT NULL,
    log_index INTEGER NOT NULL,
    token_address BYTEA NOT NULL,
    recipient BYTEA NOT NULL,
    eth_amount NUMERIC(78,0) NOT NULL,
    token_amount NUMERIC(78,0) NOT NULL,
    CONSTRAINT lp_fee_claims_pkey PRIMARY KEY (chain_id, tx_hash, log_index),
    CONSTRAINT lp_fee_claims_chain_id_positive CHECK (chain_id > 0),
    CONSTRAINT lp_fee_claims_block_number_nonnegative CHECK (block_number >= 0),
    CONSTRAINT lp_fee_claims_block_hash_length CHECK (octet_length(block_hash) = 32),
    CONSTRAINT lp_fee_claims_transaction_index_nonnegative CHECK (transaction_index >= 0),
    CONSTRAINT lp_fee_claims_tx_hash_length CHECK (octet_length(tx_hash) = 32),
    CONSTRAINT lp_fee_claims_log_index_nonnegative CHECK (log_index >= 0),
    CONSTRAINT lp_fee_claims_token_address_length CHECK (octet_length(token_address) = 20),
    CONSTRAINT lp_fee_claims_recipient_length CHECK (octet_length(recipient) = 20),
    CONSTRAINT lp_fee_claims_eth_amount_nonnegative CHECK (eth_amount >= 0),
    CONSTRAINT lp_fee_claims_token_amount_nonnegative CHECK (token_amount >= 0),
    CONSTRAINT lp_fee_claims_block_fk FOREIGN KEY (chain_id, block_number, block_hash, block_time)
        REFERENCES indexed_blocks (chain_id, block_number, block_hash, block_time)
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT lp_fee_claims_token_launch_fk FOREIGN KEY (chain_id, token_address)
        REFERENCES token_launches (chain_id, token_address)
        DEFERRABLE INITIALLY DEFERRED
);

CREATE TABLE v4_pool_initializations (
    chain_id BIGINT NOT NULL,
    block_number BIGINT NOT NULL,
    block_hash BYTEA NOT NULL,
    block_time TIMESTAMPTZ NOT NULL,
    transaction_index INTEGER NOT NULL,
    tx_hash BYTEA NOT NULL,
    log_index INTEGER NOT NULL,
    pool_id BYTEA NOT NULL,
    currency0 BYTEA NOT NULL,
    currency1 BYTEA NOT NULL,
    fee INTEGER NOT NULL,
    tick_spacing INTEGER NOT NULL,
    hooks BYTEA NOT NULL,
    sqrt_price_x96 NUMERIC(78,0) NOT NULL,
    tick INTEGER NOT NULL,
    CONSTRAINT v4_pool_initializations_pkey PRIMARY KEY (chain_id, tx_hash, log_index),
    CONSTRAINT v4_pool_initializations_chain_id_positive CHECK (chain_id > 0),
    CONSTRAINT v4_pool_initializations_block_number_nonnegative CHECK (block_number >= 0),
    CONSTRAINT v4_pool_initializations_block_hash_length CHECK (octet_length(block_hash) = 32),
    CONSTRAINT v4_pool_initializations_transaction_index_nonnegative CHECK (transaction_index >= 0),
    CONSTRAINT v4_pool_initializations_tx_hash_length CHECK (octet_length(tx_hash) = 32),
    CONSTRAINT v4_pool_initializations_log_index_nonnegative CHECK (log_index >= 0),
    CONSTRAINT v4_pool_initializations_pool_id_length CHECK (octet_length(pool_id) = 32),
    CONSTRAINT v4_pool_initializations_currency0_length CHECK (octet_length(currency0) = 20),
    CONSTRAINT v4_pool_initializations_currency1_length CHECK (octet_length(currency1) = 20),
    CONSTRAINT v4_pool_initializations_fee_uint24 CHECK (fee BETWEEN 0 AND 16777215),
    CONSTRAINT v4_pool_initializations_tick_spacing_int24 CHECK (tick_spacing BETWEEN -8388608 AND 8388607),
    CONSTRAINT v4_pool_initializations_hooks_length CHECK (octet_length(hooks) = 20),
    CONSTRAINT v4_pool_initializations_sqrt_price_x96_positive CHECK (sqrt_price_x96 > 0),
    CONSTRAINT v4_pool_initializations_tick_int24 CHECK (tick BETWEEN -8388608 AND 8388607),
    CONSTRAINT v4_pool_initializations_block_fk FOREIGN KEY (chain_id, block_number, block_hash, block_time)
        REFERENCES indexed_blocks (chain_id, block_number, block_hash, block_time)
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT v4_pool_initializations_graduation_fk FOREIGN KEY (chain_id, pool_id)
        REFERENCES graduations (chain_id, pool_id)
        DEFERRABLE INITIALLY DEFERRED
);

CREATE TABLE v4_liquidity_changes (
    chain_id BIGINT NOT NULL,
    block_number BIGINT NOT NULL,
    block_hash BYTEA NOT NULL,
    block_time TIMESTAMPTZ NOT NULL,
    transaction_index INTEGER NOT NULL,
    tx_hash BYTEA NOT NULL,
    log_index INTEGER NOT NULL,
    pool_id BYTEA NOT NULL,
    sender BYTEA NOT NULL,
    tick_lower INTEGER NOT NULL,
    tick_upper INTEGER NOT NULL,
    -- Signed int256: a negative delta removes liquidity.
    liquidity_delta NUMERIC(78,0) NOT NULL,
    salt BYTEA NOT NULL,
    CONSTRAINT v4_liquidity_changes_pkey PRIMARY KEY (chain_id, tx_hash, log_index),
    CONSTRAINT v4_liquidity_changes_chain_id_positive CHECK (chain_id > 0),
    CONSTRAINT v4_liquidity_changes_block_number_nonnegative CHECK (block_number >= 0),
    CONSTRAINT v4_liquidity_changes_block_hash_length CHECK (octet_length(block_hash) = 32),
    CONSTRAINT v4_liquidity_changes_transaction_index_nonnegative CHECK (transaction_index >= 0),
    CONSTRAINT v4_liquidity_changes_tx_hash_length CHECK (octet_length(tx_hash) = 32),
    CONSTRAINT v4_liquidity_changes_log_index_nonnegative CHECK (log_index >= 0),
    CONSTRAINT v4_liquidity_changes_pool_id_length CHECK (octet_length(pool_id) = 32),
    CONSTRAINT v4_liquidity_changes_sender_length CHECK (octet_length(sender) = 20),
    CONSTRAINT v4_liquidity_changes_tick_lower_int24 CHECK (tick_lower BETWEEN -8388608 AND 8388607),
    CONSTRAINT v4_liquidity_changes_tick_upper_int24 CHECK (tick_upper BETWEEN -8388608 AND 8388607),
    CONSTRAINT v4_liquidity_changes_salt_length CHECK (octet_length(salt) = 32),
    CONSTRAINT v4_liquidity_changes_block_fk FOREIGN KEY (chain_id, block_number, block_hash, block_time)
        REFERENCES indexed_blocks (chain_id, block_number, block_hash, block_time)
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT v4_liquidity_changes_graduation_fk FOREIGN KEY (chain_id, pool_id)
        REFERENCES graduations (chain_id, pool_id)
        DEFERRABLE INITIALLY DEFERRED
);

CREATE TABLE v4_swaps (
    chain_id BIGINT NOT NULL,
    block_number BIGINT NOT NULL,
    block_hash BYTEA NOT NULL,
    block_time TIMESTAMPTZ NOT NULL,
    transaction_index INTEGER NOT NULL,
    tx_hash BYTEA NOT NULL,
    log_index INTEGER NOT NULL,
    pool_id BYTEA NOT NULL,
    sender BYTEA NOT NULL,
    -- Signed int128 swapper deltas: a negative amount is paid into the pool.
    amount0_delta NUMERIC(78,0) NOT NULL,
    amount1_delta NUMERIC(78,0) NOT NULL,
    sqrt_price_x96 NUMERIC(78,0) NOT NULL,
    liquidity NUMERIC(78,0) NOT NULL,
    tick INTEGER NOT NULL,
    fee INTEGER NOT NULL,
    -- The transaction sender; sender is the router. NULL until the lookup succeeds.
    tx_from BYTEA,
    CONSTRAINT v4_swaps_pkey PRIMARY KEY (chain_id, tx_hash, log_index),
    CONSTRAINT v4_swaps_chain_id_positive CHECK (chain_id > 0),
    CONSTRAINT v4_swaps_block_number_nonnegative CHECK (block_number >= 0),
    CONSTRAINT v4_swaps_block_hash_length CHECK (octet_length(block_hash) = 32),
    CONSTRAINT v4_swaps_transaction_index_nonnegative CHECK (transaction_index >= 0),
    CONSTRAINT v4_swaps_tx_hash_length CHECK (octet_length(tx_hash) = 32),
    CONSTRAINT v4_swaps_log_index_nonnegative CHECK (log_index >= 0),
    CONSTRAINT v4_swaps_pool_id_length CHECK (octet_length(pool_id) = 32),
    CONSTRAINT v4_swaps_sender_length CHECK (octet_length(sender) = 20),
    CONSTRAINT v4_swaps_sqrt_price_x96_positive CHECK (sqrt_price_x96 > 0),
    CONSTRAINT v4_swaps_liquidity_nonnegative CHECK (liquidity >= 0),
    CONSTRAINT v4_swaps_tick_int24 CHECK (tick BETWEEN -8388608 AND 8388607),
    CONSTRAINT v4_swaps_fee_uint24 CHECK (fee BETWEEN 0 AND 16777215),
    CONSTRAINT v4_swaps_tx_from_length CHECK (octet_length(tx_from) = 20),
    CONSTRAINT v4_swaps_block_fk FOREIGN KEY (chain_id, block_number, block_hash, block_time)
        REFERENCES indexed_blocks (chain_id, block_number, block_hash, block_time)
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT v4_swaps_graduation_fk FOREIGN KEY (chain_id, pool_id)
        REFERENCES graduations (chain_id, pool_id)
        DEFERRABLE INITIALLY DEFERRED
);

CREATE INDEX v4_swaps_pool_order_idx
    ON v4_swaps (chain_id, pool_id, block_number, transaction_index, log_index);

-- Pool registry projection (pool id -> token), written from graduations by the per-token rebuild.
CREATE TABLE v4_pools (
    chain_id BIGINT NOT NULL,
    pool_id BYTEA NOT NULL,
    token_address BYTEA NOT NULL,
    position_token_id NUMERIC(78,0) NOT NULL,
    hook BYTEA NOT NULL,
    CONSTRAINT v4_pools_pkey PRIMARY KEY (chain_id, pool_id),
    CONSTRAINT v4_pools_chain_token_key UNIQUE (chain_id, token_address),
    CONSTRAINT v4_pools_chain_id_positive CHECK (chain_id > 0),
    CONSTRAINT v4_pools_pool_id_length CHECK (octet_length(pool_id) = 32),
    CONSTRAINT v4_pools_token_address_length CHECK (octet_length(token_address) = 20),
    CONSTRAINT v4_pools_position_token_id_nonnegative CHECK (position_token_id >= 0),
    CONSTRAINT v4_pools_hook_length CHECK (octet_length(hook) = 20),
    CONSTRAINT v4_pools_token_fk FOREIGN KEY (chain_id, token_address)
        REFERENCES tokens (chain_id, token_address)
        DEFERRABLE INITIALLY DEFERRED
);

-- +goose Down
-- Restoring the v1-only schema would drop or violate engine-2 rows, so refuse while any exist.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM token_launches WHERE engine_version <> 1 OR lp_pair IS NULL OR weth IS NULL)
           OR EXISTS (SELECT 1 FROM tokens WHERE engine_version <> 1 OR lp_pair IS NULL OR weth IS NULL)
           OR EXISTS (SELECT 1 FROM graduations WHERE lp_pair IS NULL)
           OR EXISTS (SELECT 1 FROM trades WHERE snipe_tax <> 0)
           OR EXISTS (SELECT 1 FROM v4_pools)
           OR EXISTS (SELECT 1 FROM snipe_tax_claims)
           OR EXISTS (SELECT 1 FROM fee_recipient_proposals)
           OR EXISTS (SELECT 1 FROM fee_recipient_acceptances)
           OR EXISTS (SELECT 1 FROM lp_fee_collections)
           OR EXISTS (SELECT 1 FROM lp_fee_claims)
           OR EXISTS (SELECT 1 FROM v4_pool_initializations)
           OR EXISTS (SELECT 1 FROM v4_liquidity_changes)
           OR EXISTS (SELECT 1 FROM v4_swaps) THEN
        RAISE EXCEPTION 'engine v2 ledger rows exist; delete them before rolling back 00024_engine_v2_ledger';
    END IF;
END $$;
-- +goose StatementEnd

DROP TRIGGER trades_match_launch_engine_trigger ON trades;
DROP FUNCTION trades_match_launch_engine();
DROP TRIGGER graduations_match_launch_engine_trigger ON graduations;
DROP FUNCTION graduations_match_launch_engine();

DROP TABLE v4_pools;
DROP TABLE v4_swaps;
DROP TABLE v4_liquidity_changes;
DROP TABLE v4_pool_initializations;
DROP TABLE lp_fee_claims;
DROP TABLE lp_fee_collections;
DROP TABLE fee_recipient_acceptances;
DROP TABLE fee_recipient_proposals;
DROP TABLE snipe_tax_claims;

ALTER TABLE graduations
    DROP CONSTRAINT graduations_chain_pool_key,
    DROP CONSTRAINT graduations_engine_shape,
    DROP COLUMN token_remainder,
    DROP COLUMN eth_remainder,
    DROP COLUMN sqrt_price_x96,
    DROP COLUMN liquidity,
    DROP COLUMN position_token_id,
    DROP COLUMN pool_id,
    ALTER COLUMN lp_liquidity_burned SET NOT NULL,
    ALTER COLUMN lp_pair SET NOT NULL;

ALTER TABLE trades
    DROP CONSTRAINT trades_snipe_tax_within_gross,
    DROP CONSTRAINT trades_snipe_tax_buy_only,
    DROP CONSTRAINT trades_snipe_tax_nonnegative,
    DROP COLUMN snipe_tax;

ALTER TABLE tokens
    ALTER COLUMN token_is_token0 SET EXPRESSION AS (token_address < weth);

ALTER TABLE tokens
    DROP CONSTRAINT tokens_engine_v2_fields,
    DROP CONSTRAINT tokens_engine_pair,
    DROP CONSTRAINT tokens_lp_creator_share_bps_uint16,
    DROP CONSTRAINT tokens_anti_snipe_duration_seconds_uint32,
    DROP CONSTRAINT tokens_anti_snipe_start_bps_uint16,
    DROP CONSTRAINT tokens_locker_length,
    DROP CONSTRAINT tokens_hook_length,
    DROP COLUMN lp_creator_share_bps,
    DROP COLUMN anti_snipe_duration_seconds,
    DROP COLUMN anti_snipe_start_bps,
    DROP COLUMN locker,
    DROP COLUMN hook,
    ALTER COLUMN weth SET NOT NULL,
    ALTER COLUMN lp_pair SET NOT NULL;

ALTER TABLE token_launches
    DROP CONSTRAINT token_launches_engine_v2_fields,
    DROP CONSTRAINT token_launches_engine_pair,
    DROP CONSTRAINT token_launches_lp_creator_share_bps_uint16,
    DROP CONSTRAINT token_launches_anti_snipe_duration_seconds_uint32,
    DROP CONSTRAINT token_launches_anti_snipe_start_bps_uint16,
    DROP CONSTRAINT token_launches_locker_length,
    DROP CONSTRAINT token_launches_hook_length,
    DROP COLUMN lp_creator_share_bps,
    DROP COLUMN anti_snipe_duration_seconds,
    DROP COLUMN anti_snipe_start_bps,
    DROP COLUMN locker,
    DROP COLUMN hook,
    ALTER COLUMN weth SET NOT NULL,
    ALTER COLUMN lp_pair SET NOT NULL;

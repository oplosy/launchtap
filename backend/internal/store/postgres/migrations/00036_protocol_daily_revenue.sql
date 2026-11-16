-- +goose Up
-- Protocol revenue per UTC day and engine (docs/specs/2026-11-16-protocol-revenue-rollup-preflight.md).
-- GET /v1/stats/protocol/revenue summed every trade, graduation and LP fee collection on each
-- request; the stats worker now keeps this rollup next to protocol_daily, rebuilding only the
-- days of newly indexed blocks, and the endpoint sums its few rows.
CREATE TABLE protocol_daily_revenue (
    chain_id BIGINT NOT NULL,
    day DATE NOT NULL,
    engine_version INTEGER NOT NULL,
    launch_fees NUMERIC(78,0) NOT NULL DEFAULT 0,
    trade_fees NUMERIC(78,0) NOT NULL DEFAULT 0,
    snipe_tax NUMERIC(78,0) NOT NULL DEFAULT 0,
    graduation_remainder NUMERIC(78,0) NOT NULL DEFAULT 0,
    lp_fees NUMERIC(78,0) NOT NULL DEFAULT 0,
    CONSTRAINT protocol_daily_revenue_pkey PRIMARY KEY (chain_id, day, engine_version),
    CONSTRAINT protocol_daily_revenue_chain_id_positive CHECK (chain_id > 0),
    CONSTRAINT protocol_daily_revenue_engine_version_positive CHECK (engine_version > 0),
    CONSTRAINT protocol_daily_revenue_launch_fees_nonnegative CHECK (launch_fees >= 0),
    CONSTRAINT protocol_daily_revenue_trade_fees_nonnegative CHECK (trade_fees >= 0),
    CONSTRAINT protocol_daily_revenue_snipe_tax_nonnegative CHECK (snipe_tax >= 0),
    CONSTRAINT protocol_daily_revenue_graduation_remainder_nonnegative CHECK (graduation_remainder >= 0),
    CONSTRAINT protocol_daily_revenue_lp_fees_nonnegative CHECK (lp_fees >= 0)
);

-- The from-day rebuild filters each source by block_time; trades, launches and graduations have
-- this index since 00015 and 00030.
CREATE INDEX lp_fee_collections_chain_block_time_idx ON lp_fee_collections (chain_id, block_time);

-- Backfill the whole history once (RecomputeProtocolDailyRevenue with from_day '-infinity', per
-- chain). Later refreshes rebuild only the days above protocol_stats.daily_through_block.
INSERT INTO protocol_daily_revenue (chain_id, day, engine_version, launch_fees, trade_fees, snipe_tax, graduation_remainder, lp_fees)
SELECT chain_id, day, engine_version, sum(launch_fees), sum(trade_fees), sum(snipe_tax),
       sum(graduation_remainder), sum(lp_fees)
FROM (
    SELECT l.chain_id, (l.block_time AT TIME ZONE 'UTC')::date AS day, l.engine_version, l.launch_fee_paid AS launch_fees,
           0::numeric AS trade_fees, 0::numeric AS snipe_tax, 0::numeric AS graduation_remainder, 0::numeric AS lp_fees
    FROM token_launches AS l
    UNION ALL
    SELECT t.chain_id, (t.block_time AT TIME ZONE 'UTC')::date, l.engine_version, 0, t.protocol_fee, t.snipe_tax, 0, 0
    FROM trades AS t
    JOIN token_launches AS l ON l.chain_id = t.chain_id AND l.token_address = t.token_address
    UNION ALL
    SELECT g.chain_id, (g.block_time AT TIME ZONE 'UTC')::date, l.engine_version, 0, 0, 0, g.eth_remainder, 0
    FROM graduations AS g
    JOIN token_launches AS l ON l.chain_id = g.chain_id AND l.token_address = g.token_address
    UNION ALL
    SELECT c.chain_id, (c.block_time AT TIME ZONE 'UTC')::date, l.engine_version, 0, 0, 0, 0,
           c.eth_amount - div(c.eth_amount * COALESCE(l.lp_creator_share_bps, 0), 10000)
    FROM lp_fee_collections AS c
    JOIN token_launches AS l ON l.chain_id = c.chain_id AND l.token_address = c.token_address
) AS daily
GROUP BY chain_id, day, engine_version;

-- +goose Down
DROP INDEX IF EXISTS lp_fee_collections_chain_block_time_idx;
DROP TABLE protocol_daily_revenue;

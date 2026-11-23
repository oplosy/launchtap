-- Demo leaderboard for the local stack (web/scripts/local-stack.ps1), for designing the
-- Leaderboard page. Local only: 48 made-up wallets that never traded on chain, with realized
-- profit written straight into the projections the leaderboard reads. Profit falls off steeply
-- with rank (a few whales, a long tail), a third of the wallets carry a username, and each wallet's
-- profit is booked in legs spread over the last 75 days, so the 7d, 30d and all-time windows rank
-- differently. The indexer never marks these wallets dirty, so it leaves them as written; the
-- windowed ranking picks the legs up on its next refresh (about 30 s). Safe to run twice.
BEGIN;

CREATE TEMP TABLE demo_traders ON COMMIT DROP AS
SELECT n,
       decode(substr(md5('leaderboard-demo-' || n), 1, 32) || substr(md5('leaderboard-tail-' || n), 1, 8), 'hex') AS wallet,
       -- Top profit ~42 ETH, falling off with rank; tiny for the tail.
       42 / power(n, 1.35) AS target_eth,
       2 + n % 9 AS legs
FROM generate_series(1, 48) AS n;

CREATE TEMP TABLE demo_names (n INT, username TEXT) ON COMMIT DROP;
INSERT INTO demo_names VALUES
    (1, 'whale_of_wallstreet'), (2, 'curve_sniper'), (4, 'diamond_dana'), (5, 'gm_degen'),
    (7, 'exit_liquidity'), (8, 'bonding_bandit'), (11, 'lp_burner'), (13, 'moon_math'),
    (16, 'quiet_accumulator'), (19, 'early_ape'), (22, 'rugproof'), (26, 'satoshis_cousin'),
    (29, 'chart_whisperer'), (33, 'two_x_tim'), (38, 'ngmi_nancy'), (44, 'last_buyer');

INSERT INTO social_users (chain_id, deployment_id, privy_did, username, username_changed_at)
SELECT 31337, 'task6-anvil', 'did:privy:leaderboard-demo-' || n, username, now() FROM demo_names
ON CONFLICT DO NOTHING;

INSERT INTO social_public_wallets (chain_id, deployment_id, wallet, privy_did)
SELECT 31337, 'task6-anvil', t.wallet, 'did:privy:leaderboard-demo-' || t.n
FROM demo_names AS d JOIN demo_traders AS t USING (n)
ON CONFLICT DO NOTHING;

-- The launched tokens, so legs (and the wallet pages they lead to) name real tokens; a made-up
-- address stands in when the stack runs without a seed.
CREATE TEMP TABLE demo_tokens ON COMMIT DROP AS
SELECT row_number() OVER (ORDER BY token_address) - 1 AS i, token_address
FROM tokens WHERE chain_id = 31337;

-- Each leg's share of the wallet's profit and its age come from a hash of (wallet, leg), so the
-- data is the same on every run. About one leg in six is a loss.
CREATE TEMP TABLE demo_legs ON COMMIT DROP AS
SELECT t.n, t.wallet, j,
       COALESCE(
           (SELECT token_address FROM demo_tokens
            WHERE i = (t.n * 7 + j) % GREATEST((SELECT count(*) FROM demo_tokens), 1)),
           decode(substr(md5('leaderboard-token-' || ((t.n * 7 + j) % 12)), 1, 40), 'hex')
       ) AS token_address,
       now() - make_interval(secs => (75 * 86400 * h.age)::int) AS block_time,
       round(t.target_eth / t.legs * (CASE WHEN h.sign < 1.0 / 6 THEN -0.35 ELSE 0.6 + h.share * 0.9 END)
             * 1e18)::numeric(79, 0) AS realized_pnl
FROM demo_traders AS t
CROSS JOIN LATERAL generate_series(1, t.legs) AS j
CROSS JOIN LATERAL (
    SELECT ('x' || substr(md5(t.n || ':' || j || ':share'), 1, 8))::bit(32)::bigint / 4294967296.0 AS share,
           ('x' || substr(md5(t.n || ':' || j || ':age'), 1, 8))::bit(32)::bigint / 4294967296.0 AS age,
           ('x' || substr(md5(t.n || ':' || j || ':sign'), 1, 8))::bit(32)::bigint / 4294967296.0 AS sign
) AS h;

INSERT INTO wallet_realized_legs (chain_id, wallet, token_address, block_number, transaction_index, block_time, realized_pnl)
SELECT 31337, wallet, token_address, j, n, block_time, realized_pnl
FROM demo_legs
WHERE realized_pnl <> 0
ON CONFLICT DO NOTHING;

INSERT INTO wallet_totals (chain_id, wallet, realized_pnl, leaderboard_realized_pnl, tokens_traded, held_count, first_block_number, last_block_number)
SELECT 31337, wallet, sum(realized_pnl), sum(realized_pnl), count(DISTINCT token_address) + n % 5, n % 4, 0, max(j)
FROM demo_legs
GROUP BY n, wallet
ON CONFLICT DO NOTHING;

COMMIT;

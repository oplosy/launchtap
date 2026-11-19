-- Reads for the indexed-state reconciliation pass
-- (docs/specs/2026-11-14-state-reconciliation-preflight.md). Amounts are read as text so the
-- comparison parses them into *big.Int without a float on the way.

-- name: ListReconcileTokens :many
-- Two samples of up to row_limit tokens each: the tokens whose reserves changed most recently
-- (where trading, and therefore drift, happens), and a rotating sample over every token, starting
-- at rotation_start and wrapping around, so a token that never trades or has left the recent set
-- is still compared on some pass (security-audit run-4).
WITH recent AS (
    SELECT t.token_address
    FROM tokens t
    JOIN token_reserves r ON r.chain_id = t.chain_id AND r.token_address = t.token_address
    WHERE t.chain_id = sqlc.arg(chain_id)
    ORDER BY r.source_block_number DESC, t.token_address
    LIMIT sqlc.arg(row_limit)
), rotating AS (
    SELECT t.token_address
    FROM tokens t
    WHERE t.chain_id = sqlc.arg(chain_id)
    ORDER BY (t.token_address < sqlc.arg(rotation_start)::bytea), t.token_address
    LIMIT sqlc.arg(row_limit)
), picked AS (
    SELECT token_address FROM recent
    UNION
    SELECT token_address FROM rotating
)
SELECT
    t.token_address,
    t.curve_address,
    t.phase,
    -- Engine 1 only (NULL for engine 2): the pair and WETH pick the pair's reserve order.
    t.lp_pair,
    t.weth,
    COALESCE(r.reserve_source, '')::text AS reserve_source,
    -- V4 rows carry sqrt_price_x96 instead of reserves; curve and pair rows the reverse
    -- (token_reserves_source_shape), so each source reads only its own columns.
    COALESCE(r.eth_reserve, 0)::text AS eth_reserve_text,
    COALESCE(r.token_reserve, 0)::text AS token_reserve_text,
    COALESCE(r.sqrt_price_x96, 0)::text AS sqrt_price_x96_text,
    -- Engine 2 only, empty without a pool registry row: the graduated pool's coordinates
    -- (docs/specs/2026-11-18-pool-reconciliation-preflight.md).
    COALESCE(vp.pool_id, ''::bytea)::bytea AS v4_pool_id,
    COALESCE(vm.pool_manager, ''::bytea)::bytea AS v4_pool_manager
FROM picked p
JOIN tokens t ON t.chain_id = sqlc.arg(chain_id) AND t.token_address = p.token_address
LEFT JOIN token_reserves r ON r.chain_id = t.chain_id AND r.token_address = t.token_address
LEFT JOIN v4_pools vp ON vp.chain_id = t.chain_id AND vp.token_address = t.token_address
LEFT JOIN v4_pool_managers vm ON vm.chain_id = t.chain_id AND vp.pool_id IS NOT NULL
ORDER BY t.token_address;

-- name: SumHolderBalances :one
SELECT COALESCE(sum(balance), 0)::text AS total_text
FROM holder_balances
WHERE chain_id = sqlc.arg(chain_id) AND token_address = sqlc.arg(token_address);

-- name: ListTopHolders :many
SELECT holder_address, balance::text AS balance_text
FROM holder_balances
WHERE chain_id = sqlc.arg(chain_id) AND token_address = sqlc.arg(token_address) AND balance > 0
ORDER BY balance DESC, holder_address
LIMIT sqlc.arg(row_limit);

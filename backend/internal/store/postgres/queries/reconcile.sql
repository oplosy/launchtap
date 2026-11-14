-- Reads for the indexed-state reconciliation pass
-- (docs/specs/2026-11-14-state-reconciliation-preflight.md). Amounts are read as text so the
-- comparison parses them into *big.Int without a float on the way.

-- name: ListReconcileTokens :many
-- The tokens whose reserves changed most recently: where trading, and therefore drift, happens.
SELECT
    t.token_address,
    t.curve_address,
    t.phase,
    r.reserve_source,
    -- V4 reserve rows carry no ETH/token reserve (NULL); only curve rows are compared.
    COALESCE(r.eth_reserve, 0)::text AS eth_reserve_text,
    COALESCE(r.token_reserve, 0)::text AS token_reserve_text
FROM tokens t
JOIN token_reserves r ON r.chain_id = t.chain_id AND r.token_address = t.token_address
WHERE t.chain_id = sqlc.arg(chain_id)
ORDER BY r.source_block_number DESC, t.token_address
LIMIT sqlc.arg(row_limit);

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

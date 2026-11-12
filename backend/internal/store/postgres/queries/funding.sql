-- Early-buyer clusters Level 2 (docs/specs/2026-11-12-funding-source-clusters-design.md).

-- The finalized head the indexer has observed; NULL until finality is known.
-- name: GetFundingFinalizedHead :one
SELECT finalized_number
FROM sync_state
WHERE chain_id = sqlc.arg(chain_id) AND deployment_id = sqlc.arg(deployment_id);

-- Early wallets of any token (same rule as ListEarlyCurveDeliveries) with no funding row yet,
-- whose first early delivery is at or below the finalized head, oldest first.
-- name: ListWalletsAwaitingFunding :many
SELECT tr.to_address AS wallet
FROM tokens AS t
JOIN transfers AS tr ON tr.chain_id = t.chain_id AND tr.token_address = t.token_address
LEFT JOIN v4_pool_managers AS manager ON manager.chain_id = t.chain_id
WHERE t.chain_id = sqlc.arg(chain_id)
  AND tr.from_address = t.curve_address
  AND tr.block_number >= t.launch_block_number
  AND tr.block_number <= sqlc.arg(finalized_block)::bigint
  AND tr.block_time <= t.launch_block_time + make_interval(secs => sqlc.arg(window_seconds)::integer)
  AND NOT token_holder_excluded(t.engine_version, tr.to_address, t.curve_address, t.lp_pair,
                                t.locker, manager.pool_manager)
  AND NOT EXISTS (SELECT 1 FROM wallet_funding AS wf
                  WHERE wf.chain_id = t.chain_id AND wf.wallet = tr.to_address)
GROUP BY tr.to_address
ORDER BY min(tr.block_number), tr.to_address
LIMIT sqlc.arg(page_size)::integer;

-- Rows are immutable: a second resolution of the same wallet keeps the first.
-- name: InsertWalletFunding :exec
INSERT INTO wallet_funding (chain_id, wallet, status, funder, funded_block, funding_tx, checked_block)
VALUES (sqlc.arg(chain_id), sqlc.arg(wallet), sqlc.arg(status), sqlc.narg(funder), sqlc.narg(funded_block),
        sqlc.narg(funding_tx), sqlc.arg(checked_block))
ON CONFLICT (chain_id, wallet) DO NOTHING;

-- Funding rows of the given wallets, with whether the funder must be ignored: an operator
-- exclusion or any token's curve.
-- name: ListWalletFundingFor :many
SELECT wf.wallet, wf.status, wf.funder,
       (wf.funder IS NOT NULL AND (
            EXISTS (SELECT 1 FROM funding_excluded_funders AS x
                    WHERE x.chain_id = wf.chain_id AND x.address = wf.funder)
         OR EXISTS (SELECT 1 FROM tokens AS c
                    WHERE c.chain_id = wf.chain_id AND c.curve_address = wf.funder)
       ))::boolean AS funder_ignored
FROM wallet_funding AS wf
WHERE wf.chain_id = sqlc.arg(chain_id) AND wf.wallet = ANY(sqlc.arg(wallets)::bytea[]);

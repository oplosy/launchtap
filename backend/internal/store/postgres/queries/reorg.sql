-- name: FindCommonAncestor :one
SELECT block_number, block_hash
FROM indexed_blocks
WHERE chain_id = $1 AND block_hash = ANY($2::bytea[])
ORDER BY block_number DESC
LIMIT 1;

-- name: AffectedTokensAbove :many
SELECT DISTINCT affected.token_address FROM (
    SELECT launch.token_address FROM token_launches AS launch WHERE launch.chain_id=$1 AND launch.block_number>$2
    UNION ALL SELECT trade.token_address FROM trades AS trade WHERE trade.chain_id=$1 AND trade.block_number>$2
    UNION ALL SELECT graduation.token_address FROM graduations AS graduation WHERE graduation.chain_id=$1 AND graduation.block_number>$2
    UNION ALL SELECT claim.token_address FROM creator_fee_claims AS claim WHERE claim.chain_id=$1 AND claim.block_number>$2
    UNION ALL SELECT claim.token_address FROM protocol_fee_claims AS claim WHERE claim.chain_id=$1 AND claim.block_number>$2
    UNION ALL SELECT credit.token_address FROM refund_credits AS credit WHERE credit.chain_id=$1 AND credit.block_number>$2
    UNION ALL SELECT claim.token_address FROM refund_claims AS claim WHERE claim.chain_id=$1 AND claim.block_number>$2
    UNION ALL SELECT transfer.token_address FROM transfers AS transfer WHERE transfer.chain_id=$1 AND transfer.block_number>$2
    UNION ALL SELECT claim.token_address FROM snipe_tax_claims AS claim WHERE claim.chain_id=$1 AND claim.block_number>$2
    UNION ALL SELECT proposal.token_address FROM fee_recipient_proposals AS proposal WHERE proposal.chain_id=$1 AND proposal.block_number>$2
    UNION ALL SELECT acceptance.token_address FROM fee_recipient_acceptances AS acceptance WHERE acceptance.chain_id=$1 AND acceptance.block_number>$2
    UNION ALL SELECT collection.token_address FROM lp_fee_collections AS collection WHERE collection.chain_id=$1 AND collection.block_number>$2
    UNION ALL SELECT claim.token_address FROM lp_fee_claims AS claim WHERE claim.chain_id=$1 AND claim.block_number>$2
    -- V4 pool events resolve to their token through the graduation ledger, not a projection.
    UNION ALL
    SELECT graduation.token_address FROM graduations AS graduation JOIN (
        SELECT initialization.pool_id FROM v4_pool_initializations AS initialization WHERE initialization.chain_id=$1 AND initialization.block_number>$2
        UNION SELECT change.pool_id FROM v4_liquidity_changes AS change WHERE change.chain_id=$1 AND change.block_number>$2
        UNION SELECT swap.pool_id FROM v4_swaps AS swap WHERE swap.chain_id=$1 AND swap.block_number>$2
    ) AS pools ON pools.pool_id=graduation.pool_id WHERE graduation.chain_id=$1
    UNION ALL
    SELECT token.token_address FROM tokens AS token JOIN (
        SELECT mint.pair_address FROM pool_mints AS mint WHERE mint.chain_id=$1 AND mint.block_number>$2
        UNION SELECT burn.pair_address FROM pool_burns AS burn WHERE burn.chain_id=$1 AND burn.block_number>$2
        UNION SELECT swap.pair_address FROM pool_swaps AS swap WHERE swap.chain_id=$1 AND swap.block_number>$2
        UNION SELECT sync.pair_address FROM pool_syncs AS sync WHERE sync.chain_id=$1 AND sync.block_number>$2
    ) AS pairs ON pairs.pair_address=token.lp_pair WHERE token.chain_id=$1
) AS affected ORDER BY token_address;

-- name: ListPositionPairsAbove :many
-- Run before DeleteCanonicalAbove: the rows (and a launched token's own row) are still present.
-- Excluded wallets (R4) are dropped here because a deleted token's curve and pair are not
-- resolvable afterwards.
SELECT DISTINCT pairs.wallet, pairs.token_address FROM (
    SELECT trade.trader AS wallet, trade.token_address FROM trades AS trade
    WHERE trade.chain_id=sqlc.arg(chain_id) AND trade.block_number>sqlc.arg(block_number)
    UNION ALL
    SELECT swap.tx_from, token.token_address FROM pool_swaps AS swap
    JOIN tokens AS token ON token.chain_id=swap.chain_id AND token.lp_pair=swap.pair_address
    WHERE swap.chain_id=sqlc.arg(chain_id) AND swap.block_number>sqlc.arg(block_number) AND swap.tx_from IS NOT NULL
    UNION ALL
    SELECT transfer.from_address, transfer.token_address FROM transfers AS transfer
    WHERE transfer.chain_id=sqlc.arg(chain_id) AND transfer.block_number>sqlc.arg(block_number)
    UNION ALL
    SELECT transfer.to_address, transfer.token_address FROM transfers AS transfer
    WHERE transfer.chain_id=sqlc.arg(chain_id) AND transfer.block_number>sqlc.arg(block_number)
) AS pairs
WHERE pairs.wallet <> decode(repeat('00', 20), 'hex')
  AND pairs.wallet <> sqlc.arg(factory)::bytea
  AND NOT EXISTS (
    SELECT 1 FROM tokens AS excluded
    WHERE excluded.chain_id=sqlc.arg(chain_id) AND excluded.curve_address=pairs.wallet
  )
  AND NOT EXISTS (
    SELECT 1 FROM tokens AS excluded
    WHERE excluded.chain_id=sqlc.arg(chain_id) AND excluded.lp_pair=pairs.wallet
  )
ORDER BY pairs.wallet, pairs.token_address;

-- name: DeleteTokenLaunchesAbove :execrows
DELETE FROM token_launches WHERE chain_id=$1 AND block_number>$2;
-- name: DeleteTradesAbove :execrows
DELETE FROM trades WHERE chain_id=$1 AND block_number>$2;
-- name: DeleteGraduationsAbove :execrows
DELETE FROM graduations WHERE chain_id=$1 AND block_number>$2;
-- name: DeleteCreatorFeeClaimsAbove :execrows
DELETE FROM creator_fee_claims WHERE chain_id=$1 AND block_number>$2;
-- name: DeleteProtocolFeeClaimsAbove :execrows
DELETE FROM protocol_fee_claims WHERE chain_id=$1 AND block_number>$2;
-- name: DeleteLaunchFeeClaimsAbove :execrows
DELETE FROM launch_fee_claims WHERE chain_id=$1 AND block_number>$2;
-- name: DeleteRefundCreditsAbove :execrows
DELETE FROM refund_credits WHERE chain_id=$1 AND block_number>$2;
-- name: DeleteRefundClaimsAbove :execrows
DELETE FROM refund_claims WHERE chain_id=$1 AND block_number>$2;
-- name: DeleteTransfersAbove :execrows
DELETE FROM transfers WHERE chain_id=$1 AND block_number>$2;
-- name: DeletePoolMintsAbove :execrows
DELETE FROM pool_mints WHERE chain_id=$1 AND block_number>$2;
-- name: DeletePoolBurnsAbove :execrows
DELETE FROM pool_burns WHERE chain_id=$1 AND block_number>$2;
-- name: DeletePoolSwapsAbove :execrows
DELETE FROM pool_swaps WHERE chain_id=$1 AND block_number>$2;
-- name: DeletePoolSyncsAbove :execrows
DELETE FROM pool_syncs WHERE chain_id=$1 AND block_number>$2;
-- name: DeleteLaunchPauseEventsAbove :execrows
DELETE FROM launch_pause_events WHERE chain_id=$1 AND block_number>$2;
-- name: DeleteTradingPauseEventsAbove :execrows
DELETE FROM trading_pause_events WHERE chain_id=$1 AND block_number>$2;
-- name: DeleteEngineConfigurationsAbove :execrows
DELETE FROM engine_configurations WHERE chain_id=$1 AND block_number>$2;
-- name: DeleteFutureDefaultsConfigurationsAbove :execrows
DELETE FROM future_defaults_configurations WHERE chain_id=$1 AND block_number>$2;
-- name: DeleteFutureTreasuryConfigurationsAbove :execrows
DELETE FROM future_treasury_configurations WHERE chain_id=$1 AND block_number>$2;
-- Engine-2 ledger. V4 pool rows reference graduations, so they are deleted before them.
-- name: DeleteSnipeTaxClaimsAbove :execrows
DELETE FROM snipe_tax_claims WHERE chain_id=$1 AND block_number>$2;
-- name: DeleteFeeRecipientProposalsAbove :execrows
DELETE FROM fee_recipient_proposals WHERE chain_id=$1 AND block_number>$2;
-- name: DeleteFeeRecipientAcceptancesAbove :execrows
DELETE FROM fee_recipient_acceptances WHERE chain_id=$1 AND block_number>$2;
-- name: DeleteLpFeeCollectionsAbove :execrows
DELETE FROM lp_fee_collections WHERE chain_id=$1 AND block_number>$2;
-- name: DeleteLpFeeClaimsAbove :execrows
DELETE FROM lp_fee_claims WHERE chain_id=$1 AND block_number>$2;
-- name: DeleteV4PoolInitializationsAbove :execrows
DELETE FROM v4_pool_initializations WHERE chain_id=$1 AND block_number>$2;
-- name: DeleteV4LiquidityChangesAbove :execrows
DELETE FROM v4_liquidity_changes WHERE chain_id=$1 AND block_number>$2;
-- name: DeleteV4SwapsAbove :execrows
DELETE FROM v4_swaps WHERE chain_id=$1 AND block_number>$2;
-- name: DeleteIndexedBlocksAbove :execrows
DELETE FROM indexed_blocks WHERE chain_id=$1 AND block_number>$2;

-- name: RecordIndexerReorg :one
INSERT INTO indexer_reorgs (
 chain_id, deployment_id, detected_tip_number, detected_tip_hash,
 common_ancestor_number, common_ancestor_hash, depth, detected_at, outcome
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'open')
RETURNING reorg_id;

-- name: CompleteIndexerReorg :one
UPDATE indexer_reorgs SET outcome='recovered', completed_at=now()
WHERE reorg_id=$1 AND outcome='open'
RETURNING chain_id, deployment_id, common_ancestor_number, common_ancestor_hash;

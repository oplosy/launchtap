-- name: InsertTrade :execrows
INSERT INTO trades (
    chain_id, block_number, block_hash, block_time, transaction_index, tx_hash, log_index,
    token_address, trader, is_buy, eth_gross, eth_refund, token_amount, protocol_fee,
    creator_fee, new_eth_reserve, new_token_reserve
) VALUES (
    $1, $2, $3, $4, $5, $6, $7,
    $8, $9, $10, $11, $12, $13, $14,
    $15, $16, $17
)
ON CONFLICT (chain_id, tx_hash, log_index) DO NOTHING;

-- name: TradeMatchesEvent :one
SELECT chain_id = $1 AND block_number = $2 AND block_hash = $3 AND block_time = $4
   AND transaction_index = $5 AND tx_hash = $6 AND log_index = $7
   AND token_address = $8 AND trader = $9 AND is_buy = $10 AND eth_gross = $11
   AND eth_refund = $12 AND token_amount = $13 AND protocol_fee = $14 AND creator_fee = $15
   AND new_eth_reserve = $16 AND new_token_reserve = $17
FROM trades WHERE chain_id = $1 AND tx_hash = $6 AND log_index = $7;

-- name: InsertLaunchPauseEvent :execrows
INSERT INTO launch_pause_events (
    chain_id, block_number, block_hash, block_time, transaction_index, tx_hash, log_index, paused
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (chain_id, tx_hash, log_index) DO NOTHING;

-- name: LaunchPauseEventMatchesEvent :one
SELECT chain_id = $1 AND block_number = $2 AND block_hash = $3 AND block_time = $4
   AND transaction_index = $5 AND tx_hash = $6 AND log_index = $7 AND paused = $8
FROM launch_pause_events WHERE chain_id = $1 AND tx_hash = $6 AND log_index = $7;

-- name: InsertTokenLaunch :execrows
INSERT INTO token_launches (chain_id, block_number, block_hash, block_time, transaction_index, tx_hash, log_index, token_address, curve_address, creator, lp_pair, weth, protocol_treasury, engine_version, name, symbol, total_supply, virtual_eth, virtual_token, curve_tokens, lp_tokens, graduation_eth, launch_fee_paid, trade_fee_bps, protocol_share_bps)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25)
ON CONFLICT (chain_id, tx_hash, log_index) DO NOTHING;
-- name: TokenLaunchMatchesEvent :one
SELECT chain_id=$1 AND block_number=$2 AND block_hash=$3 AND block_time=$4 AND transaction_index=$5 AND tx_hash=$6 AND log_index=$7 AND token_address=$8 AND curve_address=$9 AND creator=$10 AND lp_pair=$11 AND weth=$12 AND protocol_treasury=$13 AND engine_version=$14 AND name=$15 AND symbol=$16 AND total_supply=$17 AND virtual_eth=$18 AND virtual_token=$19 AND curve_tokens=$20 AND lp_tokens=$21 AND graduation_eth=$22 AND launch_fee_paid=$23 AND trade_fee_bps=$24 AND protocol_share_bps=$25
FROM token_launches WHERE chain_id=$1 AND tx_hash=$6 AND log_index=$7;

-- name: InsertGraduation :execrows
INSERT INTO graduations (chain_id, block_number, block_hash, block_time, transaction_index, tx_hash, log_index, token_address, lp_pair, eth_to_pool, tokens_to_pool, lp_liquidity_burned)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) ON CONFLICT (chain_id, tx_hash, log_index) DO NOTHING;
-- name: GraduationMatchesEvent :one
SELECT chain_id=$1 AND block_number=$2 AND block_hash=$3 AND block_time=$4 AND transaction_index=$5 AND tx_hash=$6 AND log_index=$7 AND token_address=$8 AND lp_pair=$9 AND eth_to_pool=$10 AND tokens_to_pool=$11 AND lp_liquidity_burned=$12
FROM graduations WHERE chain_id=$1 AND tx_hash=$6 AND log_index=$7;

-- name: InsertCreatorFeeClaim :execrows
INSERT INTO creator_fee_claims (chain_id, block_number, block_hash, block_time, transaction_index, tx_hash, log_index, token_address, creator, amount)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT (chain_id, tx_hash, log_index) DO NOTHING;
-- name: CreatorFeeClaimMatchesEvent :one
SELECT chain_id=$1 AND block_number=$2 AND block_hash=$3 AND block_time=$4 AND transaction_index=$5 AND tx_hash=$6 AND log_index=$7 AND token_address=$8 AND creator=$9 AND amount=$10
FROM creator_fee_claims WHERE chain_id=$1 AND tx_hash=$6 AND log_index=$7;

-- name: InsertProtocolFeeClaim :execrows
INSERT INTO protocol_fee_claims (chain_id, block_number, block_hash, block_time, transaction_index, tx_hash, log_index, token_address, treasury, amount)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT (chain_id, tx_hash, log_index) DO NOTHING;
-- name: ProtocolFeeClaimMatchesEvent :one
SELECT chain_id=$1 AND block_number=$2 AND block_hash=$3 AND block_time=$4 AND transaction_index=$5 AND tx_hash=$6 AND log_index=$7 AND token_address=$8 AND treasury=$9 AND amount=$10
FROM protocol_fee_claims WHERE chain_id=$1 AND tx_hash=$6 AND log_index=$7;

-- name: InsertLaunchFeeClaim :execrows
INSERT INTO launch_fee_claims (chain_id, block_number, block_hash, block_time, transaction_index, tx_hash, log_index, treasury, amount)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT (chain_id, tx_hash, log_index) DO NOTHING;
-- name: LaunchFeeClaimMatchesEvent :one
SELECT chain_id=$1 AND block_number=$2 AND block_hash=$3 AND block_time=$4 AND transaction_index=$5 AND tx_hash=$6 AND log_index=$7 AND treasury=$8 AND amount=$9
FROM launch_fee_claims WHERE chain_id=$1 AND tx_hash=$6 AND log_index=$7;

-- name: InsertRefundCredit :execrows
INSERT INTO refund_credits (chain_id, block_number, block_hash, block_time, transaction_index, tx_hash, log_index, token_address, account, amount)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT (chain_id, tx_hash, log_index) DO NOTHING;
-- name: RefundCreditMatchesEvent :one
SELECT chain_id=$1 AND block_number=$2 AND block_hash=$3 AND block_time=$4 AND transaction_index=$5 AND tx_hash=$6 AND log_index=$7 AND token_address=$8 AND account=$9 AND amount=$10
FROM refund_credits WHERE chain_id=$1 AND tx_hash=$6 AND log_index=$7;

-- name: InsertRefundClaim :execrows
INSERT INTO refund_claims (chain_id, block_number, block_hash, block_time, transaction_index, tx_hash, log_index, token_address, account, amount)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT (chain_id, tx_hash, log_index) DO NOTHING;
-- name: RefundClaimMatchesEvent :one
SELECT chain_id=$1 AND block_number=$2 AND block_hash=$3 AND block_time=$4 AND transaction_index=$5 AND tx_hash=$6 AND log_index=$7 AND token_address=$8 AND account=$9 AND amount=$10
FROM refund_claims WHERE chain_id=$1 AND tx_hash=$6 AND log_index=$7;

-- name: InsertTransfer :execrows
INSERT INTO transfers (chain_id, block_number, block_hash, block_time, transaction_index, tx_hash, log_index, token_address, from_address, to_address, value)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT (chain_id, tx_hash, log_index) DO NOTHING;
-- name: TransferMatchesEvent :one
SELECT chain_id=$1 AND block_number=$2 AND block_hash=$3 AND block_time=$4 AND transaction_index=$5 AND tx_hash=$6 AND log_index=$7 AND token_address=$8 AND from_address=$9 AND to_address=$10 AND value=$11
FROM transfers WHERE chain_id=$1 AND tx_hash=$6 AND log_index=$7;

-- name: InsertPoolMint :execrows
INSERT INTO pool_mints (chain_id, block_number, block_hash, block_time, transaction_index, tx_hash, log_index, pair_address, sender, amount0, amount1)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT (chain_id, tx_hash, log_index) DO NOTHING;
-- name: PoolMintMatchesEvent :one
SELECT chain_id=$1 AND block_number=$2 AND block_hash=$3 AND block_time=$4 AND transaction_index=$5 AND tx_hash=$6 AND log_index=$7 AND pair_address=$8 AND sender=$9 AND amount0=$10 AND amount1=$11
FROM pool_mints WHERE chain_id=$1 AND tx_hash=$6 AND log_index=$7;

-- name: InsertPoolBurn :execrows
INSERT INTO pool_burns (chain_id, block_number, block_hash, block_time, transaction_index, tx_hash, log_index, pair_address, sender, amount0, amount1, to_address)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) ON CONFLICT (chain_id, tx_hash, log_index) DO NOTHING;
-- name: PoolBurnMatchesEvent :one
SELECT chain_id=$1 AND block_number=$2 AND block_hash=$3 AND block_time=$4 AND transaction_index=$5 AND tx_hash=$6 AND log_index=$7 AND pair_address=$8 AND sender=$9 AND amount0=$10 AND amount1=$11 AND to_address=$12
FROM pool_burns WHERE chain_id=$1 AND tx_hash=$6 AND log_index=$7;

-- name: InsertPoolSwap :execrows
INSERT INTO pool_swaps (chain_id, block_number, block_hash, block_time, transaction_index, tx_hash, log_index, pair_address, sender, amount0_in, amount1_in, amount0_out, amount1_out, to_address, tx_from)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15) ON CONFLICT (chain_id, tx_hash, log_index) DO NOTHING;
-- tx_from is enrichment, not event identity: the match below ignores it.
-- name: PoolSwapMatchesEvent :one
SELECT chain_id=$1 AND block_number=$2 AND block_hash=$3 AND block_time=$4 AND transaction_index=$5 AND tx_hash=$6 AND log_index=$7 AND pair_address=$8 AND sender=$9 AND amount0_in=$10 AND amount1_in=$11 AND amount0_out=$12 AND amount1_out=$13 AND to_address=$14
FROM pool_swaps WHERE chain_id=$1 AND tx_hash=$6 AND log_index=$7;

-- name: InsertPoolSync :execrows
INSERT INTO pool_syncs (chain_id, block_number, block_hash, block_time, transaction_index, tx_hash, log_index, pair_address, reserve0, reserve1)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT (chain_id, tx_hash, log_index) DO NOTHING;
-- name: PoolSyncMatchesEvent :one
SELECT chain_id=$1 AND block_number=$2 AND block_hash=$3 AND block_time=$4 AND transaction_index=$5 AND tx_hash=$6 AND log_index=$7 AND pair_address=$8 AND reserve0=$9 AND reserve1=$10
FROM pool_syncs WHERE chain_id=$1 AND tx_hash=$6 AND log_index=$7;

-- name: InsertTradingPauseEvent :execrows
INSERT INTO trading_pause_events (chain_id, block_number, block_hash, block_time, transaction_index, tx_hash, log_index, paused)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT (chain_id, tx_hash, log_index) DO NOTHING;
-- name: TradingPauseEventMatchesEvent :one
SELECT chain_id=$1 AND block_number=$2 AND block_hash=$3 AND block_time=$4 AND transaction_index=$5 AND tx_hash=$6 AND log_index=$7 AND paused=$8
FROM trading_pause_events WHERE chain_id=$1 AND tx_hash=$6 AND log_index=$7;

-- name: InsertEngineConfiguration :execrows
INSERT INTO engine_configurations (chain_id, block_number, block_hash, block_time, transaction_index, tx_hash, log_index, engine_version, implementation, enabled)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT (chain_id, tx_hash, log_index) DO NOTHING;
-- name: EngineConfigurationMatchesEvent :one
SELECT chain_id=$1 AND block_number=$2 AND block_hash=$3 AND block_time=$4 AND transaction_index=$5 AND tx_hash=$6 AND log_index=$7 AND engine_version=$8 AND implementation=$9 AND enabled=$10
FROM engine_configurations WHERE chain_id=$1 AND tx_hash=$6 AND log_index=$7;

-- name: InsertFutureDefaultsConfiguration :execrows
INSERT INTO future_defaults_configurations (chain_id, block_number, block_hash, block_time, transaction_index, tx_hash, log_index, config_hash)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT (chain_id, tx_hash, log_index) DO NOTHING;
-- name: FutureDefaultsConfigurationMatchesEvent :one
SELECT chain_id=$1 AND block_number=$2 AND block_hash=$3 AND block_time=$4 AND transaction_index=$5 AND tx_hash=$6 AND log_index=$7 AND config_hash=$8
FROM future_defaults_configurations WHERE chain_id=$1 AND tx_hash=$6 AND log_index=$7;

-- name: InsertFutureTreasuryConfiguration :execrows
INSERT INTO future_treasury_configurations (chain_id, block_number, block_hash, block_time, transaction_index, tx_hash, log_index, previous_treasury, new_treasury)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT (chain_id, tx_hash, log_index) DO NOTHING;
-- name: FutureTreasuryConfigurationMatchesEvent :one
SELECT chain_id=$1 AND block_number=$2 AND block_hash=$3 AND block_time=$4 AND transaction_index=$5 AND tx_hash=$6 AND log_index=$7 AND previous_treasury=$8 AND new_treasury=$9
FROM future_treasury_configurations WHERE chain_id=$1 AND tx_hash=$6 AND log_index=$7;

-- Engine v2 (Plan v2-B Task 5). The v1 inserts above keep their exact v1 column set. A v2 row
-- leaves the pair columns NULL and fills the v2 columns; the engine CHECKs and the deferred
-- *_match_launch_engine triggers reject a row whose shape disagrees with its launch.

-- name: InsertTokenLaunchV2 :execrows
INSERT INTO token_launches (chain_id, block_number, block_hash, block_time, transaction_index, tx_hash, log_index, token_address, curve_address, creator, protocol_treasury, engine_version, name, symbol, total_supply, virtual_eth, virtual_token, curve_tokens, lp_tokens, graduation_eth, launch_fee_paid, trade_fee_bps, protocol_share_bps, hook, locker, anti_snipe_start_bps, anti_snipe_duration_seconds, lp_creator_share_bps)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28)
ON CONFLICT (chain_id, tx_hash, log_index) DO NOTHING;
-- name: TokenLaunchV2MatchesEvent :one
SELECT chain_id=$1 AND block_number=$2 AND block_hash=$3 AND block_time=$4 AND transaction_index=$5 AND tx_hash=$6 AND log_index=$7 AND token_address=$8 AND curve_address=$9 AND creator=$10 AND protocol_treasury=$11 AND engine_version=$12 AND name=$13 AND symbol=$14 AND total_supply=$15 AND virtual_eth=$16 AND virtual_token=$17 AND curve_tokens=$18 AND lp_tokens=$19 AND graduation_eth=$20 AND launch_fee_paid=$21 AND trade_fee_bps=$22 AND protocol_share_bps=$23 AND COALESCE(hook=$24 AND locker=$25 AND anti_snipe_start_bps=$26 AND anti_snipe_duration_seconds=$27 AND lp_creator_share_bps=$28, FALSE) AND lp_pair IS NULL AND weth IS NULL
FROM token_launches WHERE chain_id=$1 AND tx_hash=$6 AND log_index=$7;

-- name: InsertTradeV2 :execrows
INSERT INTO trades (chain_id, block_number, block_hash, block_time, transaction_index, tx_hash, log_index, token_address, trader, is_buy, eth_gross, eth_refund, token_amount, protocol_fee, creator_fee, new_eth_reserve, new_token_reserve, snipe_tax)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
ON CONFLICT (chain_id, tx_hash, log_index) DO NOTHING;
-- name: TradeV2MatchesEvent :one
SELECT chain_id=$1 AND block_number=$2 AND block_hash=$3 AND block_time=$4 AND transaction_index=$5 AND tx_hash=$6 AND log_index=$7 AND token_address=$8 AND trader=$9 AND is_buy=$10 AND eth_gross=$11 AND eth_refund=$12 AND token_amount=$13 AND protocol_fee=$14 AND creator_fee=$15 AND new_eth_reserve=$16 AND new_token_reserve=$17 AND snipe_tax=$18
FROM trades WHERE chain_id=$1 AND tx_hash=$6 AND log_index=$7;

-- name: InsertGraduationV2 :execrows
INSERT INTO graduations (chain_id, block_number, block_hash, block_time, transaction_index, tx_hash, log_index, token_address, eth_to_pool, tokens_to_pool, pool_id, position_token_id, liquidity, sqrt_price_x96, eth_remainder, token_remainder)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
ON CONFLICT (chain_id, tx_hash, log_index) DO NOTHING;
-- name: GraduationV2MatchesEvent :one
SELECT chain_id=$1 AND block_number=$2 AND block_hash=$3 AND block_time=$4 AND transaction_index=$5 AND tx_hash=$6 AND log_index=$7 AND token_address=$8 AND eth_to_pool=$9 AND tokens_to_pool=$10 AND COALESCE(pool_id=$11 AND position_token_id=$12 AND liquidity=$13 AND sqrt_price_x96=$14 AND eth_remainder=$15 AND token_remainder=$16, FALSE) AND lp_pair IS NULL AND lp_liquidity_burned IS NULL
FROM graduations WHERE chain_id=$1 AND tx_hash=$6 AND log_index=$7;

-- name: InsertSnipeTaxClaim :execrows
INSERT INTO snipe_tax_claims (chain_id, block_number, block_hash, block_time, transaction_index, tx_hash, log_index, token_address, treasury, amount)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT (chain_id, tx_hash, log_index) DO NOTHING;
-- name: SnipeTaxClaimMatchesEvent :one
SELECT chain_id=$1 AND block_number=$2 AND block_hash=$3 AND block_time=$4 AND transaction_index=$5 AND tx_hash=$6 AND log_index=$7 AND token_address=$8 AND treasury=$9 AND amount=$10
FROM snipe_tax_claims WHERE chain_id=$1 AND tx_hash=$6 AND log_index=$7;

-- name: InsertFeeRecipientProposal :execrows
INSERT INTO fee_recipient_proposals (chain_id, block_number, block_hash, block_time, transaction_index, tx_hash, log_index, token_address, current_recipient, next_recipient)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT (chain_id, tx_hash, log_index) DO NOTHING;
-- name: FeeRecipientProposalMatchesEvent :one
SELECT chain_id=$1 AND block_number=$2 AND block_hash=$3 AND block_time=$4 AND transaction_index=$5 AND tx_hash=$6 AND log_index=$7 AND token_address=$8 AND current_recipient=$9 AND next_recipient=$10
FROM fee_recipient_proposals WHERE chain_id=$1 AND tx_hash=$6 AND log_index=$7;

-- name: InsertFeeRecipientAcceptance :execrows
INSERT INTO fee_recipient_acceptances (chain_id, block_number, block_hash, block_time, transaction_index, tx_hash, log_index, token_address, previous_recipient, next_recipient)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT (chain_id, tx_hash, log_index) DO NOTHING;
-- name: FeeRecipientAcceptanceMatchesEvent :one
SELECT chain_id=$1 AND block_number=$2 AND block_hash=$3 AND block_time=$4 AND transaction_index=$5 AND tx_hash=$6 AND log_index=$7 AND token_address=$8 AND previous_recipient=$9 AND next_recipient=$10
FROM fee_recipient_acceptances WHERE chain_id=$1 AND tx_hash=$6 AND log_index=$7;

-- name: InsertLpFeeCollection :execrows
INSERT INTO lp_fee_collections (chain_id, block_number, block_hash, block_time, transaction_index, tx_hash, log_index, token_address, eth_amount, token_amount)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT (chain_id, tx_hash, log_index) DO NOTHING;
-- name: LpFeeCollectionMatchesEvent :one
SELECT chain_id=$1 AND block_number=$2 AND block_hash=$3 AND block_time=$4 AND transaction_index=$5 AND tx_hash=$6 AND log_index=$7 AND token_address=$8 AND eth_amount=$9 AND token_amount=$10
FROM lp_fee_collections WHERE chain_id=$1 AND tx_hash=$6 AND log_index=$7;

-- name: InsertLpFeeClaim :execrows
INSERT INTO lp_fee_claims (chain_id, block_number, block_hash, block_time, transaction_index, tx_hash, log_index, token_address, recipient, eth_amount, token_amount)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT (chain_id, tx_hash, log_index) DO NOTHING;
-- name: LpFeeClaimMatchesEvent :one
SELECT chain_id=$1 AND block_number=$2 AND block_hash=$3 AND block_time=$4 AND transaction_index=$5 AND tx_hash=$6 AND log_index=$7 AND token_address=$8 AND recipient=$9 AND eth_amount=$10 AND token_amount=$11
FROM lp_fee_claims WHERE chain_id=$1 AND tx_hash=$6 AND log_index=$7;

-- name: InsertV4PoolInitialization :execrows
INSERT INTO v4_pool_initializations (chain_id, block_number, block_hash, block_time, transaction_index, tx_hash, log_index, pool_id, currency0, currency1, fee, tick_spacing, hooks, sqrt_price_x96, tick)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15) ON CONFLICT (chain_id, tx_hash, log_index) DO NOTHING;
-- name: V4PoolInitializationMatchesEvent :one
SELECT chain_id=$1 AND block_number=$2 AND block_hash=$3 AND block_time=$4 AND transaction_index=$5 AND tx_hash=$6 AND log_index=$7 AND pool_id=$8 AND currency0=$9 AND currency1=$10 AND fee=$11 AND tick_spacing=$12 AND hooks=$13 AND sqrt_price_x96=$14 AND tick=$15
FROM v4_pool_initializations WHERE chain_id=$1 AND tx_hash=$6 AND log_index=$7;

-- name: InsertV4LiquidityChange :execrows
INSERT INTO v4_liquidity_changes (chain_id, block_number, block_hash, block_time, transaction_index, tx_hash, log_index, pool_id, sender, tick_lower, tick_upper, liquidity_delta, salt)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) ON CONFLICT (chain_id, tx_hash, log_index) DO NOTHING;
-- name: V4LiquidityChangeMatchesEvent :one
SELECT chain_id=$1 AND block_number=$2 AND block_hash=$3 AND block_time=$4 AND transaction_index=$5 AND tx_hash=$6 AND log_index=$7 AND pool_id=$8 AND sender=$9 AND tick_lower=$10 AND tick_upper=$11 AND liquidity_delta=$12 AND salt=$13
FROM v4_liquidity_changes WHERE chain_id=$1 AND tx_hash=$6 AND log_index=$7;

-- name: InsertV4Swap :execrows
INSERT INTO v4_swaps (chain_id, block_number, block_hash, block_time, transaction_index, tx_hash, log_index, pool_id, sender, amount0_delta, amount1_delta, sqrt_price_x96, liquidity, tick, fee, tx_from)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16) ON CONFLICT (chain_id, tx_hash, log_index) DO NOTHING;
-- tx_from is enrichment, not event identity: the match below ignores it.
-- name: V4SwapMatchesEvent :one
SELECT chain_id=$1 AND block_number=$2 AND block_hash=$3 AND block_time=$4 AND transaction_index=$5 AND tx_hash=$6 AND log_index=$7 AND pool_id=$8 AND sender=$9 AND amount0_delta=$10 AND amount1_delta=$11 AND sqrt_price_x96=$12 AND liquidity=$13 AND tick=$14 AND fee=$15
FROM v4_swaps WHERE chain_id=$1 AND tx_hash=$6 AND log_index=$7;

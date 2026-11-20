-- name: ListProfileActions :many
-- Curve claims the signed-in wallets can make. Engine 1: the creator is owed every trade's
-- creator fee less its claims. Engine 2: the fee recipient can change (FeeRecipientAccepted), so
-- read each account's curve balance (creator_fee_balances.curve_eth_owed, mirroring the curve's
-- creatorFeesOwed). LpFeeLocker fees are claimed from the locker, not the curve, so they are not
-- a curve action here.
-- Only tokens the wallets can have an action on are read: engine-1 tokens they created, and
-- tokens where they hold an engine-2 curve balance or a refund credit (all indexed by account),
-- never every token on the chain (Antigravity audit HIGH-2).
WITH candidates AS (
    SELECT created.token_address FROM tokens AS created
    WHERE created.chain_id = sqlc.arg(chain_id) AND created.engine_version <> 2
      AND created.creator = ANY(sqlc.arg(wallets)::bytea[])
    UNION
    SELECT balance.token_address FROM creator_fee_balances AS balance
    WHERE balance.chain_id = sqlc.arg(chain_id) AND balance.account = ANY(sqlc.arg(wallets)::bytea[])
    UNION
    SELECT credit.token_address FROM refund_credits AS credit
    WHERE credit.chain_id = sqlc.arg(chain_id) AND credit.account = ANY(sqlc.arg(wallets)::bytea[])
)
SELECT t.token_address, t.curve_address, t.name, t.symbol, t.phase,
       owed.creator_fee_amount::numeric(78, 0) AS creator_fee_amount,
       owed.refund_amount::numeric(78, 0) AS refund_amount
FROM candidates
JOIN tokens AS t ON t.chain_id = sqlc.arg(chain_id) AND t.token_address = candidates.token_address
CROSS JOIN LATERAL (
    SELECT
        CASE
            WHEN t.engine_version = 2 THEN COALESCE((
                SELECT sum(balance.curve_eth_owed) FROM creator_fee_balances AS balance
                WHERE balance.chain_id = t.chain_id AND balance.token_address = t.token_address
                  AND balance.account = ANY(sqlc.arg(wallets)::bytea[])), 0::numeric)
            WHEN t.creator = ANY(sqlc.arg(wallets)::bytea[]) THEN GREATEST(
                COALESCE((SELECT sum(trade.creator_fee) FROM trades AS trade
                          WHERE trade.chain_id = t.chain_id AND trade.token_address = t.token_address), 0::numeric)
                - COALESCE((SELECT sum(claim.amount) FROM creator_fee_claims AS claim
                            WHERE claim.chain_id = t.chain_id AND claim.token_address = t.token_address), 0::numeric),
                0::numeric)
            ELSE 0::numeric
        END AS creator_fee_amount,
        GREATEST(
            COALESCE((SELECT sum(credit.amount) FROM refund_credits AS credit
                      WHERE credit.chain_id = t.chain_id AND credit.token_address = t.token_address
                        AND credit.account = ANY(sqlc.arg(wallets)::bytea[])), 0::numeric)
            - COALESCE((SELECT sum(claim.amount) FROM refund_claims AS claim
                        WHERE claim.chain_id = t.chain_id AND claim.token_address = t.token_address
                          AND claim.account = ANY(sqlc.arg(wallets)::bytea[])), 0::numeric),
            0::numeric) AS refund_amount
) AS owed
WHERE owed.creator_fee_amount > 0 OR owed.refund_amount > 0
ORDER BY t.token_address ASC;

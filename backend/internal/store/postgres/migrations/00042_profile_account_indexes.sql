-- +goose Up
-- The signed-in profile looks up a wallet's curve balances and refund credits by account
-- (ListProfileActions). Without these, every request scanned each table, and before them every
-- token on the chain (Antigravity audit HIGH-2,
-- docs/specs/2026-11-20-antigravity-audit-fixes-preflight.md).
CREATE INDEX creator_fee_balances_account_idx ON creator_fee_balances (chain_id, account);
CREATE INDEX refund_credits_account_idx ON refund_credits (chain_id, account);

-- +goose Down
DROP INDEX refund_credits_account_idx;
DROP INDEX creator_fee_balances_account_idx;

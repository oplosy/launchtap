# Set-based token stats recompute — pre-flight

Date: 2026-11-13. Status: locked.

## Problem

The stats worker claims up to 32 dirty tokens per batch and recomputes each with its own
`RecomputeTokenStats` statement, one round trip per token, before one protocol-aggregate refresh
per chain. Every statement repeats the same CTE plan for a single token.

## Locked decisions

1. **One query, many tokens.** `RecomputeTokensStats(chain_id, token_addresses bytea[])` replaces
   `RecomputeTokenStats`. The per-token reads (ATH candle, 24h baseline, latest close, 24h volume)
   become `LATERAL` subqueries correlated to each target token; the formulas are unchanged.
2. **Single SQL source.** `Adapter.RecomputeTokenStats` (reorg recovery, `Compute`) calls the batch
   query with one token, so there is one copy of the stats SQL, not two that can drift.
3. **One statement per chain.** `AggregationSource.ComputeBatch` groups claims by chain. Duplicate
   tokens in the array are collapsed (`DISTINCT`), since `ON CONFLICT DO UPDATE` cannot touch the
   same row twice in one statement.
4. **Failure isolation is kept.** If the batch statement fails, the batch falls back to one
   statement per token, so a single bad token fails only its own claim, as before. The fallback
   logs a warning, and the upsert writes rows in `(chain_id, token_address)` order so two
   overlapping batches cannot lock rows in opposite orders.
5. **Clock.** All tokens in one statement share its `now()` for the 24h window; previously each
   token read its own statement time. The window boundary moves by at most the batch's duration.
6. **Unchanged:** the result columns, the ATH carry-over from the previous `token_stats` row, the
   Go mirror `stats.ComputeTokenStats`, and the protocol-aggregate refresh.

## Verification

A parity integration test recomputes several tokens in one batch and requires the same
`token_stats` rows as recomputing each token alone; the existing stats integration tests run
unchanged through the single-token path. The rewrite was also checked once against the previous
query text before it was removed.

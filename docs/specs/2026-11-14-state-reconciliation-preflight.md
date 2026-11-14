# Indexed-state reconciliation — pre-flight 2026-11-14

Closes the second "Indexer and data correctness" Path to 9 item: "a reconciliation job comparing
indexed state with chain reads, alerting on drift" (`docs/quality-scorecard.md`).

## Starting point

The indexer derives every projection from logs. Reorg recovery is tested against a fresh
re-index (`TestAnvilV2DeepReorgSoak`), and the store refuses inconsistent block coordinates. Nothing
running in production compares the projections with the contracts' own state, so a decoding bug,
a missed log range or a manual database edit would stay silent.

## Decisions

1. **Where it runs.** A `reconcile.Worker` inside `cmd/indexer`, started next to the sender
   backfill, using the shared pool (never the ownership connection) and the indexer's RPC client.
   It only reads. A failed pass is logged and counted; it never stops ingest.
   *Alternative:* a separate command and container — another deployable for a read-only loop.

2. **One pinned block per pass.** The pass reads the database in one read-only REPEATABLE READ
   transaction: the observed block (number and hash) and the sampled tokens' indexed state, all
   from the same snapshot. Every chain read is an `eth_call` at that block number. After the reads,
   the header at that number is read again; if its hash differs from the snapshot's, the chain
   reorganised during the pass and the pass is discarded (outcome `reorg`), never reported as drift.
   *Rationale:* the projections are only meaningful at the block they were indexed through.

3. **What is compared.** The 25 tokens with the most recent reserve update (where trading happens)
   are sampled each pass. For each:
   - `phase` against the curve's `phase()`;
   - in the curve phase, the curve reserves (`token_reserves`, source `curve`) against
     `virtualEthReserve()` and `virtualTokenReserve()`;
   - the sum of every indexed holder balance against the token's `totalSupply()` — this catches a
     missed or duplicated transfer for any holder, not only the sampled ones;
   - the 5 largest indexed holders' balances against `balanceOf`.
   *Rationale:* these are the facts the UI shows as authoritative (price, phase, holders) and the
   ones a missed log changes. Fees and V4 pool state are left out: they are covered by the Anvil
   gates, and the sum check already catches a missed transfer anywhere.

4. **Cadence and cost.** Every 10 minutes, first pass one minute after start; at most
   25 × (1 + 2 + 1 + 5) + 2 = 227 calls per pass. Constants, no new configuration.

5. **Signals.** Metrics `launchpad_indexer_reconciliation_runs_total{outcome}` (`ok`, `drift`,
   `reorg`, `error`), `launchpad_indexer_reconciliation_mismatches` (last completed pass),
   `launchpad_indexer_reconciliation_checks` and
   `launchpad_indexer_reconciliation_last_success_timestamp_seconds`. Each mismatch is logged with
   the check, token, subject, indexed and chain values. Alerts:
   - `IndexerStateDrift` — mismatches > 0 for 25 minutes (two consecutive passes), severity page;
   - `IndexerReconciliationStale` — no successful pass for an hour while the indexer is up, ticket.
   *Rationale:* one pass can race a block the indexer has not committed yet; two in a row cannot.

## Tests

- Unit tests of the comparison with fake store and chain: agreement, each mismatch kind, the
  reorg discard, and a chain read error.
- Anvil: the v2 gate runs a reconciliation pass after the lifecycle and expects zero mismatches,
  then corrupts one indexed holder balance and expects exactly the sum and holder mismatches.
- Alert rules in `deploy/monitoring/alerts.test.yml` (`promtool test rules`).

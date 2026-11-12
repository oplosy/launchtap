-- name: GetCanonicalTransaction :many
SELECT event_kind, tx_hash, token_address, pair_address, block_number, block_hash,
       block_time, transaction_index, log_index, finality_status
FROM (
    SELECT 'token_launch'::text AS event_kind, e.tx_hash, e.token_address,
           NULL::bytea AS pair_address, e.block_number, e.block_hash, e.block_time,
           e.transaction_index, e.log_index, ib.finality_status
    FROM token_launches e
    JOIN indexed_blocks ib USING (chain_id, block_number, block_hash, block_time)
    WHERE e.chain_id = $1 AND e.tx_hash = $2
      AND EXISTS (SELECT 1 FROM sync_state ss WHERE ss.chain_id = e.chain_id AND ss.deployment_id = $3)
    UNION ALL
    SELECT 'trade'::text, e.tx_hash, e.token_address, NULL::bytea, e.block_number,
           e.block_hash, e.block_time, e.transaction_index, e.log_index, ib.finality_status
    FROM trades e
    JOIN indexed_blocks ib USING (chain_id, block_number, block_hash, block_time)
    WHERE e.chain_id = $1 AND e.tx_hash = $2
      AND EXISTS (SELECT 1 FROM sync_state ss WHERE ss.chain_id = e.chain_id AND ss.deployment_id = $3)
    UNION ALL
    SELECT 'creator_fee_claim'::text, e.tx_hash, e.token_address, NULL::bytea,
           e.block_number, e.block_hash, e.block_time, e.transaction_index, e.log_index,
           ib.finality_status
    FROM creator_fee_claims e
    JOIN indexed_blocks ib USING (chain_id, block_number, block_hash, block_time)
    WHERE e.chain_id = $1 AND e.tx_hash = $2
      AND EXISTS (SELECT 1 FROM sync_state ss WHERE ss.chain_id = e.chain_id AND ss.deployment_id = $3)
    UNION ALL
    SELECT 'refund_claim'::text, e.tx_hash, e.token_address, NULL::bytea, e.block_number,
           e.block_hash, e.block_time, e.transaction_index, e.log_index, ib.finality_status
    FROM refund_claims e
    JOIN indexed_blocks ib USING (chain_id, block_number, block_hash, block_time)
    WHERE e.chain_id = $1 AND e.tx_hash = $2
      AND EXISTS (SELECT 1 FROM sync_state ss WHERE ss.chain_id = e.chain_id AND ss.deployment_id = $3)
    UNION ALL
    SELECT 'router_swap'::text, e.tx_hash, decode(repeat('00', 20), 'hex'), e.pair_address, e.block_number,
           e.block_hash, e.block_time, e.transaction_index, e.log_index, ib.finality_status
    FROM pool_swaps e
    JOIN indexed_blocks ib USING (chain_id, block_number, block_hash, block_time)
    WHERE e.chain_id = $1 AND e.tx_hash = $2
      AND EXISTS (SELECT 1 FROM sync_state ss WHERE ss.chain_id = e.chain_id AND ss.deployment_id = $3)
    UNION ALL
    -- Engine-2 pool trades: the token comes from the graduation ledger row that opened the pool.
    SELECT 'v4_swap'::text, e.tx_hash, g.token_address, NULL::bytea, e.block_number,
           e.block_hash, e.block_time, e.transaction_index, e.log_index, ib.finality_status
    FROM v4_swaps e
    JOIN graduations g ON g.chain_id = e.chain_id AND g.pool_id = e.pool_id
    JOIN indexed_blocks ib
      ON ib.chain_id = e.chain_id AND ib.block_number = e.block_number
     AND ib.block_hash = e.block_hash AND ib.block_time = e.block_time
    WHERE e.chain_id = $1 AND e.tx_hash = $2
      AND EXISTS (SELECT 1 FROM sync_state ss WHERE ss.chain_id = e.chain_id AND ss.deployment_id = $3)
) events
ORDER BY block_number, transaction_index, log_index;

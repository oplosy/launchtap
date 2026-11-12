-- +goose Up
-- Early-buyer clusters Level 2 (docs/specs/2026-11-12-funding-source-clusters-design.md). Who
-- sent each early wallet its first ETH, written once by cmd/funding from finalized history and
-- never rewritten. funded_block is also set for an `unknown` wallet whose balance appeared
-- without a visible transfer.
CREATE TABLE wallet_funding (
    chain_id BIGINT NOT NULL,
    wallet BYTEA NOT NULL,
    status TEXT NOT NULL,
    funder BYTEA,
    funded_block BIGINT,
    funding_tx BYTEA,
    checked_block BIGINT NOT NULL,
    resolved_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT wallet_funding_pkey PRIMARY KEY (chain_id, wallet),
    CONSTRAINT wallet_funding_chain_id_positive CHECK (chain_id > 0),
    CONSTRAINT wallet_funding_wallet_length CHECK (octet_length(wallet) = 20),
    CONSTRAINT wallet_funding_status CHECK (status IN ('funded', 'unfunded', 'contract', 'unknown')),
    CONSTRAINT wallet_funding_funder_length CHECK (funder IS NULL OR octet_length(funder) = 20),
    CONSTRAINT wallet_funding_tx_length CHECK (funding_tx IS NULL OR octet_length(funding_tx) = 32),
    CONSTRAINT wallet_funding_blocks CHECK (
        checked_block >= 0 AND (funded_block IS NULL OR (funded_block >= 0 AND funded_block <= checked_block))
    ),
    CONSTRAINT wallet_funding_funded_complete CHECK (
        (status = 'funded') = (funder IS NOT NULL AND funding_tx IS NOT NULL)
        AND (status <> 'funded' OR funded_block IS NOT NULL)
        AND (status IN ('funded', 'unknown') OR funded_block IS NULL)
    )
);

CREATE INDEX wallet_funding_funder_idx ON wallet_funding (chain_id, funder) WHERE funder IS NOT NULL;

-- Operator-maintained funders that never link wallets: exchanges, bridges (including the aliased
-- L1 sender of canonical deposits), faucets.
CREATE TABLE funding_excluded_funders (
    chain_id BIGINT NOT NULL,
    address BYTEA NOT NULL,
    label TEXT NOT NULL,
    added_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT funding_excluded_funders_pkey PRIMARY KEY (chain_id, address),
    CONSTRAINT funding_excluded_funders_chain_id_positive CHECK (chain_id > 0),
    CONSTRAINT funding_excluded_funders_address_length CHECK (octet_length(address) = 20),
    CONSTRAINT funding_excluded_funders_label_length CHECK (char_length(label) BETWEEN 1 AND 200)
);

-- +goose Down
DROP TABLE IF EXISTS funding_excluded_funders;
DROP TABLE IF EXISTS wallet_funding;

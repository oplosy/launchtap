# ETH/USD enrichment source

Status: implemented. The CoinGecko adapter shipped with P0-A (`backend/internal/pricefeed`,
`GET /v1/reference/eth-usd`). The 2026-11-14 scope decision
(`docs/specs/2026-11-14-product-scope-decisions.md`) moved it to the free Demo plan
(`COINGECKO_API_PLAN=demo`); the paid-plan wording below is the original 2026-09-11 decision.
Only the key itself is still open (`backlog.md`, operator-only inputs).

Last reviewed: 2026-11-18

## Decision

Use the CoinGecko API as an optional cached enrichment source for ETH/USD. The
production plan is a commercial CoinGecko API plan (Basic or higher), not the
keyless public endpoint or the free Demo plan. The adapter endpoint is:

```text
GET https://pro-api.coingecko.com/api/v3/simple/price
    ?ids=ethereum
    &vs_currencies=usd
    &include_last_updated_at=true
    &precision=full
Authorization: x-cg-pro-api-key: <secret>
```

The response field is `ethereum.usd`; `ethereum.last_updated_at` is the provider
publication timestamp in Unix seconds. CoinGecko documents `/simple/price`, the
`last_updated_at` freshness field, and the paid-plan API-key authentication in its
[endpoint reference](https://docs.coingecko.com/reference/simple-price) and
[API-key setup guide](https://docs.coingecko.com/docs/setting-up-your-api-key).

## Why no on-chain feed was selected

Robinhood’s [oracle documentation](https://docs.robinhood.com/chain/oracles-and-price-feeds/)
says that Chainlink supplies price feeds and directs integrators to Chainlink’s
address registry; it does not publish a Robinhood Chain or Robinhood Chain Testnet
ETH/USD proxy address. The [Chainlink Robinhood feed page](https://docs.chain.link/data-feeds/tokenized-equity-feeds/robinhood)
documents Robinhood tokenized-equity feeds on Robinhood Chain Mainnet, not an
ETH/USD feed on chain 46630. The [Robinhood Data Streams page](https://docs.robinhood.com/chain/data-streams/)
publishes a mainnet verifier proxy only (chain 4663). That verifier is not an
ETH/USD feed and its address must not be reused or inferred for testnet.

The official Robinhood RPCs were checked on 2026-09-11: mainnet returned chain ID
4663 and testnet returned chain ID 46630. The documented mainnet Data Streams
verifier address returned deployed bytecode on mainnet. These checks do not turn
the verifier into a price-feed address and do not establish a testnet deployment.

## Runtime contract

ETH-native values remain authoritative. The enrichment adapter must never be on the
critical path for:

- event indexing, canonical-ledger writes, projections, or reorg recovery;
- curve or DEX quotes and transaction construction; or
- list/detail correctness when USD is unavailable.

The adapter is read-only and cached. A provider error, timeout, HTTP 429, malformed
response, missing price, or stale timestamp leaves the canonical ETH values intact
and makes the USD value unavailable. It must not fabricate zero, retry indefinitely,
or block an indexing chunk. A stale cache may be retained for diagnostics, but API
responses must return USD as `null` after the freshness limit.

Recommended initial policy for the future adapter:

- poll at most once per 60 seconds per process and coalesce concurrent requests;
- accept a sample only when `last_updated_at` is present, not in the future, and is
  no more than 120 seconds old at read time;
- on a 429, use exponential backoff with jitter and honor the next scheduled poll;
- on 5xx, timeout, DNS, or malformed data, keep the last sample marked stale and
  expose `null` USD; alerting belongs to the health/operations layer.

CoinGecko documents a 20-second Pro `/simple/price` update cadence, and its pricing
page documents the commercial plans and their rate limits. The adapter’s 60-second
poll and 120-second freshness bound are application policy, not a provider SLA.

## Numeric and licensing rules

`usd` is an external decimal, not an on-chain integer. The adapter must parse the
JSON number lexeme with an exact decimal representation, never an IEEE-754 float,
then store it in the existing nullable `NUMERIC(38,18)` fields. Values must be
positive, finite, within the column precision, and representable with at most 18
fractional digits; invalid or over-precision values are rejected and treated as
unavailable. No USD value is used to derive an ETH amount.

Commercial use requires the product to display “Data provided by CoinGecko” with a
link to [CoinGecko API](https://www.coingecko.com/en/api). Raw API data must not be
resold, sublicensed, or redistributed. Review the provider’s
[commercial-license guidance](https://support.coingecko.com/hc/en-us/articles/16760512207257-What-Are-the-Differences-Between-Commercial-and-Custom-Licenses)
and [API terms](https://www.coingecko.com/en/api_terms) before production launch.

## Explicitly rejected alternative

Coinbase Exchange’s public ETH-USD ticker is technically easy to query, but its
[Market Data Terms](https://www.coinbase.com/legal/market_data) restrict use and
redistribution in ways that are not a safe default for a public end-user product.
It is not the selected source unless legal/product ownership obtains written terms
that permit the intended use.

## Implementation boundary

This closeout selects and documents the source only. No provider call is added to
the indexer, no migration is added, and no production API key is committed. A future
adapter task must add source/retrieval timestamps, cache metrics, attribution in the
web surface, and tests for stale, 429, outage, malformed-decimal, and missing-data
paths before enabling `ETH_USD_SOURCE` in production.

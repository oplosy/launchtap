# Product scope decisions (2026-11-14)

The product owner decided the open product backlog items that waited on outside inputs. These
decisions close the "Product completeness" path in `docs/quality-scorecard.md` item 11 for the
items below.

| Item | Decision | Rationale |
|------|----------|-----------|
| Forum ticker tape (P0-C5 deferral) | **Out of scope.** | It needs an equities quote feed whose free tiers do not allow redistributing quotes on a public site. A paid licence and a new dependency are not worth it for decoration next to the forum. The forum keeps working without it. |
| Early-buyer clusters Level 2 | **Out of scope; Level 1 stays.** | Level 2 needs a paid archive + trace RPC (`debug_traceBlockByNumber` with the callTracer) and a reviewed exclusion list. Level 1 (same-block buyers) answers the main question without either. The built Level 2 code stays switched off (`cmd/funding` is not deployed). |
| Telegram alerts | **In scope; the owner creates the bot.** | The feature is built and tested. The worker is now an opt-in Compose service (`--profile telegram`), so turning it on needs only the bot token and username in `BACKEND_ENV_FILE`. |
| ETH/USD reference price | **In scope, on CoinGecko's free Demo key.** | A single ETH/USD poll does not need a paid plan. `COINGECKO_API_PLAN=demo` uses the Demo host and key header and polls every 5 minutes, within the plan's 10,000-call monthly cap. The plan was checked on 2026-11-14. Freshness widens to 11 minutes accordingly (`backend/internal/pricefeed/feed.go`). The owner creates the key. |

Not decided here: the Dexscreener/GeckoTerminal market links. They wait on the reviewed mainnet
manifest, not on a provider (`backlog.md`, "P0-A external inputs"). The testnet engine switch and
the fork RPC secret are operator actions, not product scope.

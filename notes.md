# Notes — launchpad

> Free-form project notes: ideas, decisions, open questions, research output.
> Unfinished work goes to `backlog.md`; durable agent instructions to `AGENTS.md`.

---

## Reference product

- **pons** (Pons Labs, LLC) — `pons.family` / `@ponsdotfamily`
- Fixed-supply token launchpad on **Robinhood Chain** (EVM L2).
- Model: bonding curve → "graduation" at a threshold → liquidity-locked pool.
- Non-custodial: "Your wallet submits every transaction. pons does not custody assets."
- Feeds public analytics from **Dune** data ("Data is supplied by Dune").
- Wallet UX via **Reown** ("UX by reown").
- We are building a pons-like product; the screens below are the reference.

---

## Feature analysis (screen by screen)

### 1. Navbar (Image #4)
- Left: logo (P mark).
- Center: segmented control — **Explore / Forum / Analytics**.
- Right: theme toggle (sun icon, light/dark), **Connect** button (lime-green pill).
- Dark theme is the default.

### 2. Wallet connect (Image #5)
- "Connect Wallet" modal. List: WalletConnect (QR CODE tag), Trust Wallet,
  MetaMask, Binance Wallet, SafePal, and at the bottom "Search Wallet — 540+".
- pons uses **Reown** here (formerly WalletConnect / Web3Modal).
- **Our options** → see "Technical decisions / Wallet connect" below.

### 3. Token launch (Image #6)
- Top right: **v2 / v1** toggle (two different launch engines/contracts).
- Left form fields:
  - Name, Ticker
  - Description (short text)
  - Token image (upload)
  - X profile (`x.com/handle`), Telegram (`t.me/community`)
  - **Paired asset** dropdown (ETH) — "Graduates once the curve raises 4.2 ETH."
  - **Developer buy** — amount (ETH) the developer buys at launch, with balance check
  - **Advanced** (collapsible) — extra settings
  - Bottom: "ETH pair, ETH 0.0005 due" + **Connect wallet** CTA
- Right-side **live preview card** ("Your token"):
  - Launch fee **0.0005 ETH**
  - Paired with **ETH**
  - Trade fee **1.00%**
  - Launch window **99% snipe tax, 3s** (aggressive anti-snipe tax for the first 3s)
  - Graduation **4.2 ETH**
  - Liquidity **Locked**
- Note: these parameters (fee, snipe-tax duration, graduation threshold) probably
  differ between v1 and v2; should come from config.

### 4. Search & filter modal — "Stocks" (Image #7)
- Search input: name / ticker / address.
- **Sort by:** Relevance, Market cap, Volume, Newest, Oldest.
- **Age:** All / 24h / 7d.
- **Pair** filter: All / **Stocks** / individual stock ticker chips
  (AAPL, AMD, AMZN, BB, COIN, COST, CRCL, DJT, GLD, GME, GOOGL, HIMS, META,
  MSFT, MSTR, MU, NVDA, PLTR, QQQ, RDDT, SNDK, SPCX, SPY, TSLA, TTWO).
  → the "Stocks" button = the token's paired asset is a stock token.
- Result row: icon, name, $TICKER · $MC · age.
- Pagination: "1 to 24 of 17,302 · 1 / 721 · Previous / Next".

### 5. Lists — Graduated & Explore (Image #8)
- **Graduated** (badge + count, e.g. 510): "Tokens that cleared the graduation threshold."
  - 5-up card grid. Card: image, "Graduated" badge (+ "V2" badge on some),
    name, $TICKER, **MC / FDV**, short contract address, age ("49d ago").
  - Pagination 1..51.
- **Explore** (e.g. 410,337 launched): "Tokens still climbing toward graduation on Robinhood Chain."
  - Filters: Recent buys / Newest / Oldest / Market cap / Volume · All/24h/7d · **Both / v1 / v2**.
  - Same card structure.

### 6. Coin detail + chart (Image #9)
- **About** box: short description; "Burned 0 DELTA · $0 · 0% of supply".
  Links: X, **Dexscreener**, **GeckoTerminal**, **Contract**, **Pool**.
- **Left trade panel:**
  - Token title + tabs **Market / Limit / Orders**
  - **Sell** input (asset dropdown, e.g. ETH) ↕ **Buy** input (token)
  - Quick-ratio buttons: 25% / 50% / 75% / 100%
  - **Slippage** 1% + Adjust
  - **Connect wallet** CTA
- **Top-right stat strip:** Market cap, Liquidity, 24h volume, ATH.
- **Price header:** large value + change % + timeframe (1H).
- **Chart:** area chart; **Heatmap** toggle; timeframes **5M / 1H / 6H / 1D / ALL**;
  hour labels on the X axis.

### 7. Recent trades & Holders (Image #10)
- Tabs: **Recent trades / Holders**. Count on the right ("50").
- Trade row: direction arrow (green ↗ buy / red ↙ sell), amount + TICKER,
  short wallet address, price (ETH) + $ equivalent, "Xm" (minutes ago).
- Pagination 1..5.
- Holders tab: (probably) address, balance, % supply, first-buy date.

### 8. Forum — "Memestock" (Image #13)
- **Scrolling ticker tape at the top:** general market data — stock prices
  (SPCX, GOOGL, TSLA, GME, AAPL, SPY, SNDK...) + daily change %. Continuously
  scrolls right to left.
- Title: "Memestock — Every launch with a community, ranked by what is moving."
- Tabs: **Hot / New / Top**.
- Post feed (reddit-style): `s/TICKER` community, upvote/downvote,
  username + short address + time, title, body text/image,
  comment count, **Buy** button (inline).
- Right sidebar: **"Biggest launches"** — search + ranked list 1–10 (`s/PONS $444M` ...)
  + "Browse the launchpad" link.
- For us: the tape = general market data; the feed = messages **about our own coins only**.

### 9. Analytics — "Protocol analytics" (Image #12)
- "Independent onchain reporting for pons markets on Robinhood Chain."
- Toggle **24h / All time**. "View on Dune" button. "Dune updated HH:MM, latest complete day ... UTC".
- Stat tiles: **24h volume** ($457.13M, ±% prior day), **24h launches** (17.9K, ±%),
  **24h trades** (0, "No prior-day baseline").
- Charts: **Trading volume** (daily bars), **Token launches** (daily bars);
  latest completed day highlighted.
- pons pulls this from Dune. **We will index it ourselves** → "Technical decisions / Analytics".

### 10. Docs (footer link in Image #11)
- To be built comprehensively. Likely sections: how it works (bonding curve,
  graduation, fees, snipe tax), v1 vs v2 differences, token launch guide,
  trading (slippage, limit orders), liquidity & lock, contract addresses &
  ABIs, API/indexer docs, security/risk, FAQ.

### 11. Footer (Image #11)
- Logo + tagline: "Launch and explore fixed-supply tokens on Robinhood Chain.
  Your wallet submits every transaction. [brand] does not custody assets."
- Columns: **Product** (Explore, Analytics, Create, Profile, Docs) ·
  **Legal** (Privacy Policy, Terms of Use) · **Risk notice** (transactions are
  irreversible, tokens can lose value, no custody/warranty/financial advice).
- Bottom: © year + company; X link.

---

## Technical decisions

### Wallet connect — options
pons uses "UX by reown". Since it is an EVM chain, our alternatives:

| Library | What | Cost | Note |
|---------|------|------|------|
| **Reown AppKit** (formerly Web3Modal/WalletConnect) | 540+ wallets, mobile QR, widest coverage | Free; project ID from `cloud.reown.com` | exact same experience as pons |
| **RainbowKit** (+ wagmi + viem) | Clean UX, dev-friendly, very common | Fully free, open source | solid default for an EVM launchpad |
| **ConnectKit** (family) | Similar to RainbowKit, minimal | Free | alternative |
| **Dynamic / Privy** | Embedded wallet + email/Google/social login | Free tier + paid | if you want onboarding for non-crypto-natives |
| **thirdweb Connect** | Modal + embedded + account abstraction | Free tier | ties you to the thirdweb ecosystem |
| **Web3-Onboard** (Blocknative) | Framework-agnostic | Free | for a non-React stack |

- **Recommendation:** base layer **wagmi + viem**. For the modal, either **RainbowKit**
  (free, huge adoption) or, if we want to match pons exactly, **Reown AppKit** (widest
  wallet list out of the box). Add **Privy** or **Dynamic** later if email/social
  onboarding is needed.
- Decision made: **Privy** (see Decisions).

### Analytics — our own indexer
pons uses Dune; we will do it ourselves. Options:

| Approach | What | Cost | Note |
|----------|------|------|------|
| **Ponder** (`ponder.sh`) | TypeScript indexer, self-host, writes to Postgres, GraphQL/SQL API | Cheap: 1 small VPS + RPC plan | far simpler DX than a subgraph; ideal for a single-team app |
| **The Graph subgraph** (decentralized) | Standard, composable | Query fee (GRT) + signal; gets expensive at volume | hosted service shut down |
| **Self-host Graph Node** | Run the subgraph on your own server | No query fee but you run the infra (Postgres + IPFS + archive node) | heavy ops |
| **Envio HyperIndex** | Very fast, multi-chain, hosted/self-host | Generous free tier | fast backfill |
| **Subsquid / SQD** | High throughput | Medium | heavy historical data |
| **Goldsky** | Managed subgraph + mirror pipeline | Paid, low ops | the easy path |
| **Custom** (viem `watchEvent`/log polling + Postgres + cron aggregation) | Full control | Cheapest infra, most effort | |
| **Dune API** | what pons does; SQL + embed | Free-to-medium | only as an external cross-check |

- **Superseded:** Ponder was the early recommendation; the backend is now **Go**, so the
  indexer is our own Go service (see Decisions).
- Note: for price charts we also need to produce an OHLC candle table (bucketing trade
  events into time buckets).

---

## Ideas

### Roadmap candidates from the 2026-11-11 product research

Source: external product/competition research (`launchtap-product-research-2026-11-11.md`,
not in the repo). Its repo findings match `backlog.md`; its competitor facts (Pons V2
mechanics, Crius launch counts, Uniswap router support on chain 4663) are **unverified** —
read the primary sources before any decision rests on them. Candidates only, not decisions.

**Order:** Gate 0 (already in `backlog.md`: testnet engine switch, mainnet manifest/roles,
external audit, production RPC, shared rate limiter, image object storage/CDN, Privy
advisory before 2027-02-01) → cheap v1 wins → v2 engine pre-flight spec → everything else
after product-market fit. No new tokenomics on mainnet before Gate 0 closes.

**Cheap v1 wins (no contract change):**
- Launch facts / contract identity panel: v1 tokens have no mint, tax, blacklist or owner
  privileges and the LP is burned by construction — surface that with onchain evidence
  links (factory, engine version, curve config, LP burn tx). A trust signal we already
  earn but do not show.
- Trade preview: separate fee lines (protocol / creator / network), minimum received,
  `eth_call` simulation before the wallet opens, revert reason on failure.
- Graduation preview: remaining ETH, implied opening price, estimated market cap.
- Telegram alerts: watchlist, graduation, large trades (thresholds, dedup, opt-in).

**v2 engine (needs a pre-flight spec first — contract economics, high risk):**
- Post-graduation creator revenue. Biggest competitive gap: v1 creator fees stop at
  graduation, Pons/Bankr continue them. On a V2 pair there is no clean way: a token
  transfer tax breaks the "plain ERC-20" trust signal and trips honeypot scanners; a
  router-side fee misses trades outside our UI. Realistic path is a Uniswap V4 hook +
  fee escrow, which changes the "graduate to V2, burn LP" identity and adds hook audit
  scope. New factory/engine version; v1 tokens stay as they are.
- Anti-snipe tax: buy-only, launch-time fixed decay, immutable; quote paths (web,
  backend, contract) share one `currentTax` rule.
- Creator fee recipient change (timelocked, two-step) belongs to the same spec.

**Deferred (after product-market fit):** chain-wide cross-launchpad discovery (turns us
into an aggregator, Crius dependency, mixes external data into the canonical ledger),
multi-pool router / Uniswap Trading API (one V2 pair per graduated token suffices today),
AI launch assistant, public API/webhooks/SDK, holder rewards, buyback/vesting, CTO,
CCA-style auctions, stock-token / custom quote assets.

## Decisions

- **2026-09-01 — Wallet: Privy.** Embedded wallet + email/social login + external
  wallet connection; wagmi/viem underneath. Reason: onboarding for non-crypto-natives.
  RainbowKit / Reown AppKit ruled out.
- **2026-09-01 — Chart: TradingView Lightweight Charts** (free). Charting Library
  (licensed, heavy) and the Advanced widget (only works for exchange-listed tokens)
  ruled out. Our own indexer feeds the datafeed; the area+gradient look is the
  library's native style.
- **2026-09-01 — Analytics: no Dune dependency.** Daily aggregates produced from our
  own indexer (real-time + independent of any external service).
- **2026-09-01 — Backend language: Go (firm).** NO tRPC. NO Ponder (it's TS). The
  indexer = our own Go service (`go-ethereum` ethclient + `abigen` bindings + Postgres).
  Monorepo. DB = Docker Postgres (dev). Hosting is out of scope for now.
- **2026-09-01 — Backend architecture: modular monolith.** One Go module. Two processes:
  `cmd/api` (stateless, scales horizontally) + `cmd/indexer` (singleton; aggregation =
  a goroutine inside it). NO network calls between them — they communicate via Postgres
  (+ `LISTEN/NOTIFY`). Same box, deployed together. NO gRPC, NO microservices.
- **2026-09-01 — API = REST/JSON.** Go `huma` (free OpenAPI + validation from Go types)
  → typed TS client for the frontend from the OpenAPI (one-way codegen). Live data = SSE.
  The only "RPC" in the system is JSON-RPC to the chain node (`ethclient`).
- **2026-09-01 — Backend architecture (detail, spec input):**
  - Hexagonal + feature-oriented modules: `launch, trading, token, holder, candle,
    stats, metadata`. Interfaces defined where consumed; `store/postgres` implements them.
  - DIP line: pgx/sqlc/ethclient **forbidden** in the domain; `common.Address/Hash`,
    `*big.Int` value types **allowed** (product is EVM-native).
  - `chain/` is infra only (RPC/logs/blocks/decode). Reorg + sync loop + routing = `indexer/`.
    Phase transition (curve→graduated) = `launch` service, not chain's job.
  - `candle`/`stats` (aggregation) conceptually separate from the indexer: canonical
    trades → aggregator → derived. Incremental update + periodic reconcile sweep. For v1,
    a `cmd/indexer` goroutine.
  - **UnitOfWork:** 1 block chunk = 1 DB tx, multi-module. `WithinTx(ctx, fn(Repositories))`;
    pgx.Tx does not leak into the domain (sqlc `DBTX`).
  - **Event ordering:** sort deterministically before processing by
    `(block_number, tx_index, log_index)`.
  - **Unified market trades:** canonical split stays (`trades` curve / `pool_swaps` DEX);
    candle/volume/ATH/feed/history read from a common `market_trades` VIEW. Execution price
    comes from trade legs; spot price comes from post-event curve reserves or pair `Sync`.
    `token_is_token0` is stored at launch. Materialize only if measured performance demands.
  - **store/ layout:** one package `internal/store/postgres`, one file per feature +
    `uow.go` + `db.go`.
- **2026-09-01 — Auth: Privy is the single model (custom SIWE CANCELLED).** The backend
  verifies the access token for the session and a separate identity token for linked
  wallets, requires matching subjects, and authorizes only linked wallets. Metadata writes:
  `tokens.creator ∈ user.linkedWallets`. No `/auth/siwe/*` and no session table.
- **2026-09-01 — SSE reliability = best-effort.** Postgres = source of truth; SSE = a
  live hint. If a process dies after commit but before publish, an event can be missed =
  NOT data loss. A reconnecting client first fetches the REST snapshot, then applies SSE
  deltas.
- **2026-09-01 — Approved from the "pending approval" block:** (2) Limit/Orders out of v1,
  (3) constant-product curve, (4) Uniswap v2 pair + LP burn, (5) ETH-only pair in v1.
  **(1) decomposition and (6) forum/ticker deferral — "look at it more", parked.**
- **2026-09-01 — Indexer split:** trades → main indexer (ideal for an event stream);
  holders list → a separate table/indexer or a post-graduation snapshot (updating
  balances + sorting on every transfer bloats a subgraph and lags).
- **2026-09-01 — Price/MC/FDV/ATH:** source is our own indexer. Curve spot comes from
  post-trade reserves; post-graduation spot comes from pair `Sync`; execution prices come
  from trade legs. `FDV = spot × total supply`; `MC = spot × circulating supply` under the
  backend spec's system-address exclusions. ATH uses execution-price history.
  Dexscreener/GeckoTerminal are external links only.
- **2026-09-01 — NO v1/v2 UI toggle.** We start with a single launch engine. Contracts
  are versioned (new deploy + registry), single flow for the user.
- **2026-09-01 — Budget: ZERO COST.** Every service on a free tier. No money out of
  pocket. Development on testnet (gas free from the faucet). Paid/mainnet concerns
  (external audit, Vercel Pro, persistent DB, custom domain, mainnet deploy gas, RPC
  volume) are "look at it later". Protocol fees in v1 are in config, 0/nominal on
  testnet, treasury = own wallet.
- **2026-09-01 — Chart architecture:** Lightweight Charts is only the **renderer**. The
  OHLC/price series is **produced by the indexer** (Trade event prices → time buckets →
  open/high/low/close/volume) and returned by the API. The Image #9 chart is an area
  chart = a "price over time" series is enough; OHLC table for candlestick mode.
- **2026-09-01 — Chain & dev environment: EVM-agnostic core contracts, target = Robinhood Chain.**
  Stack = Solidity + Foundry. No chain-specific address/service/assumption is baked into
  the contract; `chain → {RPC, DEX router, explorer, chainId, WETH}` config mapping.
  - Local: Anvil (fork RH testnet when needed)
  - Primary testnet: **Robinhood Chain Testnet — chainId 46630**,
    RPC `rpc.testnet.chain.robinhood.com`, explorer `explorer.testnet.chain.robinhood.com`,
    Alchemy `robinhood-testnet.g.alchemy.com`
  - Base Sepolia: optional portability check, not required
  - Mainnet: **Robinhood Chain — chainId 4663**, RPC `rpc.mainnet.chain.robinhood.com`,
    explorer `robinhoodchain.blockscout.com`, Alchemy `robinhood-mainnet.g.alchemy.com`
  - RH Chain = EVM-equivalent, Arbitrum Nitro/Orbit, blob DA, gas in ETH; standard
    tooling works without modification. Testnet Feb 2026, mainnet Jul 2026.
  - **Graduation DEX = Uniswap v2 (live on RH Chain from day one: v2/v3/v4/UniswapX)**
    - `UniswapV2Factory` = `0x8bceaa40b9acdfaedf85adf4ff01f5ad6517937f`
    - `UniswapV2Router02` = `0x89e5db8b5aa49aa85ac63f691524311aeb649eba`
    - WETH = `0x0Bd7D308f8E1639FAb988df18A8011f41EAcAD73` (mainnet only)
    - Mainnet dependency addresses were verified against official docs and live bytecode.
      They MUST NOT be reused for testnet; testnet graduation is fail-closed until its own
      reviewed deployment manifest exists.
  - **No faucet** — testnet ETH is bridged from Sepolia via the canonical Arbitrum
    bridge (or Alchemy testnet credit). A setup step.
  - **Competitor/reference:** pons + **Uniswap's own RH Chain launchpad** (to analyze).
  - The project is **NOT Solana/Anchor** — EVM from the start (reference, wallet, DEX all EVM).

- **2026-09-01 — Design closure:**
  - Contract behavior is normative in
    `docs/specs/2026-09-01-contract-core-design.md`; backend mirrors it.
  - EIP-1167 clone parameters are write-once initialized storage, not per-clone Solidity
    `immutable` values. Clone creation and initialization are atomic.
  - Final buy is partial-fill with exact gross consumption and refund; it lands exactly on
    `T_r` sold / `G` real curve ETH and graduates in the same transaction.
  - Sell input is bounded by `tokensSold`; virtual reserves are never redeemable.
  - Launch, creator, and protocol fees use pull-based claims. Failed ETH recipients do not
    block market operations.
  - Canonical Uniswap pair is fixed at launch. Pre-graduation token transfers to the curve
    or pair are curve-only. Graduation uses direct Pair mint to the burn address, not
    Router02's ratio-selecting liquidity path.
  - A pre-existing WETH-only donation to an empty pair is accepted as extra liquidity;
    pair `Sync` is the authoritative opening reserve. Existing LP supply or token reserve
    is an invariant failure.
  - Indexer tracks observed/safe/finalized heads, persists a hash-linked block ledger, and
    rolls back mutable projections as well as event rows. Fixed five-block confirmation is
    cancelled as a production finality model.
  - Canonical DEX events include Mint/Burn/Swap/Sync. Spot and execution prices are distinct.
  - PostgreSQL `NOTIFY` is a wake-up only; durable dirty work lives in a table. Indexer
    singleton ownership uses a session advisory lock.
  - Backend runtime is Go 1.26, module `github.com/Contictus/launchtap/backend`.

### Parked — "look at it more" (2026-09-01)

- **Project decomposition:** A Core launchpad → B Indexer+Analytics → C Forum → D Docs.
  (Not decided.)
- **Forum + ticker tape:** proposal to defer to sub-project C. (Not decided.)
  Ticker data option: Finnhub / Twelve Data free tier, 15-min delayed, cached.

## Open questions

These do not change contract or backend correctness and are fail-closed or nullable:

- ETH/USD source: on-chain feed if a verified deployment exists; otherwise a cached
  external adapter. ETH remains canonical and USD stays nullable.
- Robinhood testnet DEX/WETH deployment manifest: use a separately verified official
  deployment or deploy a test-only stack. Mainnet addresses are never copied.
- Forum auth and moderation detail (sub-project C).
- Production organization: multisig signers, timelock delay, legal/geo policy, hosting,
  RPC provider, monitoring, and external audit vendor.

## Spec & plan documents

- `docs/specs/2026-09-01-contract-core-design.md` — authoritative contract state machine,
  economics, fee custody, graduation, security invariants, and event schema. Design closed.
- `docs/specs/2026-09-01-backend-core-design.md` — indexer, finality/reorg, canonical DB,
  market semantics, auth, API boundaries, and curve mirror. Design closed.
- `docs/plans/2026-09-01-contract-foundations.md` — first implementation plan; 12 tasks
  covering Foundry setup, token/curve/factory, graduation, vectors, adversarial tests,
  deployment manifests, fork compatibility, and release gate.
- The backend is split into 3 sequential plans after/alongside the contract foundation:
  1. `docs/plans/2026-09-01-backend-foundations.md` — scaffold + config/registry +
     `curve/` math (differential test) + `store/` (schema + migration + sqlc + UoW).
     **12 tasks, complete and merged to `main` (tag `v0.2.0`).**
  2. `docs/plans/2026-09-05-backend-indexer.md` — chain infra + sync loop + feature
     ingestion + aggregation. **5 tasks, code complete; live Robinhood acceptance deferred.**
  3. `docs/plans/2026-09-08-backend-api-identity.md` — Huma API + Privy auth + read
     endpoints + SSE. **7 tasks, complete.**
- `docs/specs/2026-09-09-web-client-design.md` and
  `docs/plans/2026-09-09-web-client.md` define Plan 4 core web delivery. **8 tasks,
  implemented and independently reviewed on `dev`; deterministic CI is complete, while live
  Robinhood/Privy/production release inputs remain deferred.** Forum/Memestock remains a
  separate later plan.

## Sub-project A — economy & contract parameters (DESIGN CLOSED)

> These are not "the one true answer"; they are the parameters that set the economy
> + trust model. Starting values, tunable after the curve simulation.
> Status: decided 2026-09-01 (see below).

### Token
- Total supply **1,000,000,000** (fixed, no mint), **18 decimals**
- Split: **800M** bonding curve (`T_r`) · **200M** graduation liquidity (`L`)
  — found consistent by the curve simulation (below); still tunable

### Bonding curve — virtual-reserve constant-product
State: `x` = ETH reserve, `y` = token reserve, invariant `x·y = k`
- Init: `x=x0`, `y=y0`, `k=x0·y0`
- Buy (`dxEff` = ETH after trade fee removed): `dy = y − k/(x+dxEff)`; `x += dxEff`; `y −= dy`
- Sell (`dy` tokens): `dxOut = x − k/(y+dy)`; `y += dy`; `x −= dxOut`; trade fee taken from `dxOut`
- Spot price `P = x/y`
- Tokens sold `= y0 − y`; graduation condition: `y ≤ y0 − T_r` (equivalently real ETH collected `≥ G`)
- Trade fee is skimmed OUTSIDE the curve (does not enter `k`) → `G` is clean curve ETH

**Parameter derivation** (not picked by hand — from the "no-gap graduation" constraint:
curve final price = Uniswap pool opening price):
```
y0 = ceil(T_r² / (T_r − L))
x0 = G · L / (T_r − L)
launch→graduation FDV multiple = (T_r / L)²        (independent of G)
initial FDV = G·L·S / T_r²
graduation FDV = G·S / L
```

**LP split scenarios** (`G = 4.2 ETH`, `S = 1B`, assuming ETH ~$4000):

| Split (T_r/L) | Multiple | x0 | y0 | init FDV | grad FDV | Character |
|---|---|---|---|---|---|---|
| 700M/300M | 5.4x | 3.15 ETH | 1.225B | ~2.6 ETH (~$10k) | 14 ETH (~$56k) | thick liquidity |
| **800M/200M** | **16x** | 1.4 ETH | 1.0667B | ~1.31 ETH (~$5.2k) | 21 ETH (~$84k) | **recommended** |
| 900M/100M | 81x | 0.525 ETH | 1.0125B | ~0.52 ETH (~$2k) | 42 ETH (~$168k) | thin liquidity, degen |

Changing `G` scales the FDVs, not the multiple.

Exact V1 base-unit value: `y0 = 1066666666666666666666666667`. Using floor instead
would put graduation one wei above `G`; initialization rejects that parameter set.

**Simulation result (80/20, G=4.2, ETH~$4000) — 2026-09-01:**
- Launch: spot 1.3125e-9 ETH/tok, FDV 1.3125 ETH (~$5.25k), circulating MC 0
- Graduation before LP transfer: spot 2.1e-8 (16x), FDV 21 ETH (~$84k), circulating
  MC 16.8 ETH (~$67k). After `L` enters the pair, circulating supply becomes 1B under
  the backend definition, so MC equals FDV absent burns.
- Pool opening price = curve final price = 2.1e-8 (no arb gap) ✓
- Convexity: first 50% of tokens → 20% of ETH; last 25% of tokens → 57% of ETH
- Dev buy max 1% (10M tok) at launch ≈ 0.0133 ETH (~$54), ~zero effect on initial FDV
- **CRITICAL:** pool ETH depth = G, independent of the split. "70/30 for a deeper
  pool" is WRONG — the split only sets the multiple + price level. Depth lever = G.
- Fresh pool (4.2 ETH + 200M tok): a 0.5 ETH buy → +25% price; 1 ETH → +53%. Thin but
  tradeable; pump.fun graduation-pool profile.
- pump.fun comparison: init FDV $4-5k / grad FDV $60-70k / multiple 10-15x → same league.
- **Decision 3 resolved:** G = 4.2 ETH default, configurable for future launches,
  snapshotted at launch.

### Fee
- Create: **0.0005 ETH** fixed → accrues in factory for protocol pull-claim
- Every trade (**curve phase only**): **1%** = 0.5% protocol + 0.5% creator
  - creator and protocol shares accrue separately in the curve; both use pull-claims
  - failed recipients cannot block trading; claims stay available while paused
- ⚠️ AFTER graduation, vanilla Uniswap → NO protocol/creator cut.
  Post-grad fee capture = future (Uniswap V4 hook; couples the V2/V4 decision to the DEX).
- Snipe tax: **NONE in v1**
- Developer buy: **allowed, max 1% supply at launch**
- Creator income: curve-phase trade fee share (the 0.5% above)

### Graduation
Flow: curve → threshold → curve trading closes → collected ETH + `L` tokens → DEX pool → LP token → burn
- V1 is explicitly tied to Uniswap v2-style ERC-20 LP tokens. Graduation calls the pair
  directly and mints the initial launch LP position to the fixed dead address.
- Pair creation is permissionless. The canonical pair is fixed at launch and token transfers
  to it are curve-only until graduation. Existing LP supply/token reserve causes revert;
  WETH-only donation is accepted as additional liquidity and surfaced by `Sync`.
- Graduation fee = 0 in v1. Exactly `G` launch-owned ETH and `L` tokens are contributed.
- “LP burned” applies to the initial launch position; later liquidity providers own their
  independently minted LP.

### Factory & upgrade
- **EIP-1167 minimal clone**, shared implementation, separate storage per token (cheap)
- **Non-upgradeable.** When an upgrade is needed: `CurveImplementationV1 / V2`; the
  factory routes new launches to the new impl, existing tokens are untouched.

### Admin / trust model
- Emergency pause → **multisig**
- Fee/config changes → **multisig + timelock**
- Existing token/curve logic → **CANNOT** be changed by the admin
  (the admin has no power to "change this curve / send the funds elsewhere")

### Parameter management principle (2026-09-01)
Every launch parameter (`x0,y0,T_r,L,G`, fees) is **snapshotted into the clone's
write-once initialized storage** in the same transaction as clone creation. These are not
Solidity `immutable` variables. The factory holds mutable defaults only for FUTURE launches.
A started launch's rules never change.

### Event schema (the interface the backend depends on) — 2026-09-01

The normative ABI is in `docs/specs/2026-09-01-contract-core-design.md` §7. It includes
engine version, pair, name/symbol, launch fee, LP liquidity burned, creator/protocol/launch
fee claims, and refund lifecycle. Do not copy a second event definition into this file;
the contract spec is the single source of truth.

### Indexing model (backend, per token)
| Phase | Price source | Holders source |
|-------|--------------|----------------|
| Pre-graduation | `Trade`: execution from legs, spot from post-trade x/y | ERC-20 `Transfer` |
| Post-graduation | `Swap`: execution; `Sync`: spot/reserves | ERC-20 `Transfer` |
The pair is known from `TokenLaunched`; `Graduated` flips the market source. The indexer
continues to reject impossible curve trades after graduation rather than silently accepting them.

### Decision status
1. **Token economy** — ✅ 1B / 18dec · 800M curve / 200M LP (80/20 provisional)
2. **Curve economy** — ✅ virtual-reserve constant-product; x0=1.4, y0=1.0667B, k derived;
   simulation done (above)
3. **Fee economy** — ✅ create 0.0005 ETH → protocol; trade 1% = 0.5%/0.5%, curve phase only
4. **G (graduation)** — ✅ 4.2 ETH default, configurable for future launches, snapshotted at launch
5. **Event schema** — ✅ LOCKED in the contract core spec; versioned by `engineVersion`
6. **Metadata** — ✅ fully off-chain (our Postgres). The creator authenticates via **Privy**
   and writes/edits it through the API; the API checks
   `tokens.creator ∈ user.linkedWallets`
   (verified via the on-chain `TokenLaunched`). description + image + X/Telegram.
   name/symbol are already on the ERC-20. No URI in the event.
   Accepted downside: metadata is not portable/permanent; an `ipfs://` mirror can be added later.
7. **Graduation fee** — ✅ 0 in v1 (all launch-owned G goes to the pool; no-gap holds
   absent an accepted third-party WETH donation)

## To decide before coding (checklist)

Status: ✅ decided · 🔴 repo-wide, before any code · 🟡 before sub-project A · ⚪ skip in v1

### Repo-wide (🔴)
- [x] **Target chain + dev environment** — ✅ EVM-agnostic core; target Robinhood Chain (testnet 46630 / mainnet 4663); ready for Uniswap v2 graduation
- [x] **Contract stack** — ✅ Solidity + Foundry
- [x] **Backend language** — ✅ Go
- [x] **Backend API layer** — ✅ REST/JSON via `huma` (no tRPC, no gRPC)
- [x] **Backend architecture** — ✅ modular monolith, 2 processes, hexagonal + feature modules
- [x] Repo structure — monorepo: `contracts/ backend/ web/ docs/`; web workspace tooling is
  selected in the Plan 4 design and pinned in the implemented Task 1 boundary
- [x] Frontend stack — Next.js App Router + strict TypeScript + Tailwind + source-owned
  shadcn/ui primitives; Privy + wagmi + viem + TanStack Query; Lightweight Charts. Exact
  versions are pinned in Plan 4 Task 1.
- [x] DB tooling — pgx/v5 + sqlc + goose; TimescaleDB deferred
- [ ] Hosting — web: Vercel Hobby · indexer+DB: Railway/Render/Fly/Neon free (deferred)
- [x] CI + test policy — GitHub Actions; Foundry, Go build/race/lint/sqlc/migration and
  required Postgres integration gates; web lint/typecheck/build when web exists

### Sub-project A: economy + contract design (🟡)
- [x] Token: fixed-supply ERC-20 — 1B, 18 decimals; curve/pair transfer restrictions only
  during the curve phase, ordinary ERC-20 behavior after graduation
- [x] Curve parameters: x0=1.4 ETH, y0=1.0667B, T_r=800M, L=200M, G=4.2 ETH
- [x] Fees: launch 0.0005 ETH → protocol; trade 1% = 0.5% protocol / 0.5% creator
- [x] Snipe tax: none in v1
- [x] Developer buy: allowed, max 1% supply
- [x] Creator income: curve-phase trade fee share + accrual/claim
- [x] Graduation: canonical Uniswap v2 Pair direct mint, initial LP to dead address;
  Router02 is not used for graduation
- [x] Factory pattern: EIP-1167 minimal clone, non-upgradeable + versioning
- [x] **Event schema** — locked in the contract core spec and engine-versioned
- [x] Admin/security: launch/trading pause → multisig; future defaults/engine → timelock;
  claims never pause; no rescue/admin graduation; existing curve logic and params fixed
- [x] Audit plan: Slither + invariant/fuzz + independent review during development;
  external audit required before mainnet funds
- [x] Anti-abuse: no max-wallet or cooldown in v1; developer buy remains capped at 1%

### Sub-project A: minimal indexer is part of A (🟡)
Explore/Graduated has "sort by volume/market cap" → an indexer is required from day 1.
- [x] Search (name/ticker/address) — Postgres full-text (in the backend spec)
- [x] Pagination — cursor-based (in the backend spec)
- [x] Live updates — SSE, best-effort (in the backend spec)
- [x] OHLC/price series production — indexer + `candles` table (in the backend spec)

### Skip in v1 (⚪)
i18n, telemetry, referrals, Limit/Orders, Stocks pairing, forum, mobile.

### Parallel / text work (🟢)
Terms/Privacy/risk copy, geo-restriction policy (US/OFAC block?), domain/DNS,
`AGENTS.md` command + code-style sections.

## Resources / links

- Reference product: https://pons.family — X: @ponsdotfamily
- Robinhood Chain dev docs: https://docs.robinhood.com/chain/connecting · contracts: https://docs.robinhood.com/chain/contracts
- RH Chain guides: https://www.quicknode.com/guides/robinhood/what-is-robinhood-chain · https://chainstack.com/what-is-robinhood-chain/
- Uniswap on RH Chain: https://blog.uniswap.org/robinhood-chain-is-live · v2 deployments: https://developers.uniswap.org/docs/protocols/v2/deployments
- Uniswap RH Chain launchpad (competitor): https://crypto.news/uniswap-launches-first-robinhood-chain-launchpad/
- Reown AppKit: https://reown.com/appkit  ·  Cloud: https://cloud.reown.com
- RainbowKit: https://rainbowkit.com  ·  wagmi: https://wagmi.sh  ·  viem: https://viem.sh
- Ponder: https://ponder.sh
- The Graph: https://thegraph.com  ·  Envio: https://envio.dev  ·  Subsquid: https://sqd.dev
- Dune: https://dune.com

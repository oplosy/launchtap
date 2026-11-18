# Engine 2 on Robinhood testnet — pre-flight (2026-11-18)

Source: `docs/audits/2026-11-18-project-gap-assessment.md` G2. Engine 2 is the mainnet engine,
but it has never run on a live chain. Robinhood testnet (46630) has no Uniswap v4 deployment.
The Uniswap deployments feed lists none for 46630 (checked 2026-11-18), and
`DeployLaunchpadV2.s.sol` refuses every chain except mainnet. The first live launch,
graduation and LP-fee collection would therefore happen with real funds.

## Facts checked on testnet (block ~155,072,263)

- Canonical Permit2 (`0x000000000022D473030F116dDEE9F6B43aC78BA3`) is deployed. Its runtime code
  equals the pinned `permit2/test/utils/DeployPermit2.sol` runtime byte for byte, except two
  immutable words: the cached chain id and the cached domain separator. Mainnet's Permit2 differs
  from the helper in the same two words only.
- The deterministic CREATE2 proxy (`0x4e59…956C`) is deployed, so the guard hook can be mined
  and deployed exactly as on mainnet.

## Decisions

1. **Deploy our own Uniswap v4 stack on testnet from the pinned builds the tests already use.**
   The stack is `PoolManager` (v4-core `59d3ecf5`), `PositionManager` and `V4Quoter`
   (v4-periphery `c6e4754d`), all from the `v4` profile (`out-v4/`), plus
   `UniversalRouter` 2.1.2 from the `router` profile (`out-router/`). This is the same code
   the Anvil engine-2 e2e and web gates run.
   *Rationale.* It is the only way to exercise launch → graduation → LP fees → pool swaps on a
   live chain without real funds.
   *Rejected:*
   - Waiting for an official Uniswap testnet deployment: no date.
   - A mainnet canary only: the first run would still use real funds. It stays as the second
     rehearsal, not the first.
2. **Reuse the canonical Permit2 already on testnet. Do not deploy a Permit2.** The bootstrap
   verifies it exactly. It takes the pinned helper runtime, replaces its two immutable words
   with `46630` and the on-chain `DOMAIN_SEPARATOR()`, recomputes that separator from the EIP-712
   formula, and requires the result to equal the on-chain code.
3. **Testnet-only wiring choices.**
   - The `PoolManager` owner is the testnet deployer. Upstream governance owns it on mainnet;
     the owner can only set a protocol-fee controller.
   - `PositionManager` uses the reviewed testnet WETH (`config/robinhood-testnet.json`), no
     position descriptor (`address(0)`), and an unsubscribe gas limit of 100,000, as in the
     local gates.
   - `UniversalRouter` maps every protocol it does not need to `UnsupportedProtocol`, exactly
     like `DeployLocalV4Periphery`. That is the configuration the web gate tests.
   - No `StateView` and no `PositionDescriptor` are deployed, because nothing in the backend or
     web reads them.
4. **One v4 dependency schema with two branches.** `v4-dependencies.schema.json` becomes
   `oneOf`:
   - `upstream`: today's mainnet record, unchanged.
   - `selfDeployed`: requires `"provenance": "self-deployed"`, the deploy transactions, the
     `PoolManager` owner, the pinned source commits, the `V4Quoter` code hash, and
     `stateView` / `positionDescriptor` as `null`. It allows `archiveRequired: false`.
   *Rationale.* The backend registry, the web generator and `check-deployments` already find
   records by the `config/*-v4.json` name, so none of them needs a second file type.
   *Rejected:* a second schema file. The registry picks a schema by file suffix and would need
   a new routing rule.
5. **`DeployLaunchpadV2` selects the record by chain id.** 4663 uses
   `robinhood-mainnet-v4.json` and authority target `RobinhoodMainnet`. 46630 uses
   `robinhood-testnet-v4.json` and authority target `RobinhoodTestnet`, and only if that
   reviewed file exists. Every other chain is refused as before. The code-hash pinning of
   `PoolManager`, `PositionManager` and `Permit2` applies on both chains. There is no
   environment-variable override of the record path, so a reviewed record is the only input.
6. **Generated records are candidates.** `scripts/bootstrap-testnet-v4.ps1` writes
   `deployments/.generated/robinhood-testnet-v4.json` with `reviewed: false`. A human reviews
   it and commits it as `config/robinhood-testnet-v4.json` with `reviewed: true`, with a
   byte-identical backend copy. `scripts/deploy-v2-testnet.ps1` then runs `DeployLaunchpadV2`
   and writes a candidate engine-2 manifest the same way. Both scripts accept only an unlocked
   Anvil-style sender or a named Foundry keystore account, never a raw key.

## Out of scope

- The broadcasts themselves. They need the operator's keystore and faucet ETH, and are listed
  in `backlog.md` "Operator-only inputs".
- Adding the testnet engine-2 manifest to the web generator and the backend copy. That happens
  after the reviewed manifest exists, per the runbook.
- Any change to `contracts/src`. Engine 2's audited bytecode is unaffected.

## Tests

- `test/v2/DeployTestnetV4.t.sol`:
  - the script refuses any chain other than 46630, and refuses when Permit2 is missing;
  - on a simulated 46630 with Permit2 etched, the core deployment wires `PositionManager` to
    its `PoolManager`, Permit2 and WETH, and wires `V4Quoter` to the `PoolManager`.
- `test/v2/DeployLaunchpadV2.t.sol`:
  - testnet without a reviewed record is still refused;
  - with a record present, the pinned-code checks apply.
- The full script, including the router, runs as a `forge script` simulation against the
  public testnet RPC without broadcast.
- Backend `deployments` tests:
  - a self-deployed record validates;
  - an upstream record carrying `provenance` fails;
  - a self-deployed record with a non-null `stateView` fails.

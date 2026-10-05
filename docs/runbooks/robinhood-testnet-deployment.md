# Robinhood Chain Testnet deployment

This runbook is the repository-owned path for producing a reviewed chain-46630
dependency record and Launchpad deployment manifest. It does not contain a private key,
mnemonic, RPC credential, or guessed address. Until the review steps below are complete,
the testnet remains fail-closed through `contracts/deployments/config/robinhood-testnet.disabled.json`.

## Current external status

The official Uniswap v2 deployment directory lists Robinhood Chain mainnet (`4663`) but
does not list Robinhood Chain Testnet (`46630`). Robinhood's network documentation provides
the testnet RPC and explorer, but does not provide an official WETH + Uniswap v2 Factory/Pair
deployment that this project can adopt. The current project-owned path therefore deploys the
testnet-only `LocalWETH` and Uniswap v2 factory contracts from this repository.

Sources checked on 2026-09-11:

- [Robinhood network configuration](https://docs.robinhood.com/chain/add-network-to-wallet/)
- [Robinhood contract information](https://docs.robinhood.com/chain/contracts/)
- [Uniswap v2 deployments](https://developers.uniswap.org/docs/protocols/v2/deployments)
- [Uniswap unified deployments](https://developers.uniswap.org/deployments)

The absence of a published testnet dependency is not permission to copy mainnet addresses.

## Prerequisites

Use Foundry 1.8.1 and PowerShell 7 (`pwsh`). Use a named Foundry keystore or a hardware
wallet account for a signed deployment. The scripts reject raw private-key and mnemonic
parameters. The deployer must have enough testnet ETH for four dependency/Launchpad
contract creations and the associated gas.

Set the public endpoint in the current PowerShell process only. Do not put a credentialed
RPC URL in Git, a manifest, or a ticket:

```powershell
$env:ROBINHOOD_TESTNET_RPC_URL = Read-Host "Robinhood Chain Testnet RPC URL"
cast chain-id --rpc-url $env:ROBINHOOD_TESTNET_RPC_URL
# Expected output: 46630
```

Record the deployer address and signer name separately:

```powershell
$deployer = "<deployer address from the named Foundry account>"
$account = "<named Foundry keystore account>"
```

Do not replace either placeholder with a private key.

## Phase 1: deploy and capture dependencies

Run a dry-run first. It validates chain identity and the dependency contracts without
broadcasting:

```powershell
cd contracts
pwsh ./scripts/bootstrap-testnet-dependencies.ps1 `
  -RpcUrl $env:ROBINHOOD_TESTNET_RPC_URL `
  -Sender $deployer
```

Expected output:

```text
Testnet dependency bootstrap dry-run verified. Nothing was broadcast.
```

After the dry-run is reviewed locally, broadcast with the named account:

```powershell
pwsh ./scripts/bootstrap-testnet-dependencies.ps1 `
  -RpcUrl $env:ROBINHOOD_TESTNET_RPC_URL `
  -Sender $deployer `
  -Account $account `
  -Broadcast
```

Expected output includes:

```text
Generated unreviewed testnet dependency candidate .../contracts/deployments/.generated/robinhood-testnet-dependencies.json
```

The candidate is intentionally not selectable. Verify each creation transaction and the
runtime code at the recorded WETH and factory addresses with the testnet explorer and
read-only RPC calls:

```powershell
$candidate = Get-Content -Raw ./deployments/.generated/robinhood-testnet-dependencies.json | ConvertFrom-Json
cast code $candidate.weth --rpc-url $env:ROBINHOOD_TESTNET_RPC_URL
cast code $candidate.uniswapV2Factory --rpc-url $env:ROBINHOOD_TESTNET_RPC_URL
cast codehash $candidate.weth --rpc-url $env:ROBINHOOD_TESTNET_RPC_URL
cast codehash $candidate.uniswapV2Factory --rpc-url $env:ROBINHOOD_TESTNET_RPC_URL
cast call $candidate.uniswapV2Factory "pairCodeHash()(bytes32)" --rpc-url $env:ROBINHOOD_TESTNET_RPC_URL
```

All code reads must return non-empty bytecode and the pair code hash must match the
repository's canonical Uniswap v2 Pair init-code hash. The explorer must show the two
creation transactions, the deployer, chain 46630, and the expected source/build evidence.

## Phase 2: review and enable dependencies

A human reviewer must compare the candidate against the receipts and source artifacts and
add the missing chain-dependency evidence required by
`contracts/deployments/chain-dependencies.schema.json`: the Pair runtime hash, a fixed
reference block/hash, provider names, and source URLs. Set `reviewed` to `true` only after
that comparison. Write the reviewed record as:

```text
contracts/deployments/config/robinhood-testnet.json
```

Temporarily move the disabled marker out of `config/`, run the deployment artifact gate, and
commit the dependency review as its own change. Moving it keeps the marker recoverable while
the active file is being checked; restore it immediately if validation fails:

```powershell
$disabled = "./deployments/config/robinhood-testnet.disabled.json"
$disabledPending = "./deployments/config/robinhood-testnet.disabled.pending-review.json"
Move-Item $disabled $disabledPending
try {
  pwsh ./scripts/check-deployments.ps1
  if ($LASTEXITCODE -ne 0) { throw "deployment artifact gate failed" }
} catch {
  Move-Item $disabledPending $disabled
  throw
}
```

Expected output:

```text
Deployment scripts and manifest boundaries verified.
```

If any value cannot be proven from a receipt, explorer, source artifact, or read-only RPC
call, keep the disabled marker and stop.

## Phase 3: deploy Launchpad and produce the candidate manifest

Use the same signed account only as the deployer. Pause authority, timelock, and protocol
treasury must be distinct reviewed addresses; the deployer must not retain either authority.
The current script reads those addresses as explicit arguments and validates the dependency
bytecode before deployment:

```powershell
pwsh ./scripts/deploy.ps1 `
  -Target robinhood-testnet `
  -RpcUrl $env:ROBINHOOD_TESTNET_RPC_URL `
  -DeploymentId robinhood-testnet-v1 `
  -Sender $deployer `
  -PauseAuthority "<reviewed pause authority address>" `
  -Timelock "<reviewed timelock address>" `
  -ProtocolTreasury "<reviewed treasury address>" `
  -Account $account `
  -Broadcast
```

Expected output includes:

```text
Generated candidate manifest .../contracts/deployments/.generated/robinhood-testnet-v1.json
```

Review the candidate against the LaunchFactory and BondingCurveV1 creation receipts,
runtime-code hashes, start block, dependency values, pair init-code hash, governance
addresses, and the no-residual-deployer-authority assertion. Only after independent review,
copy it to:

```text
contracts/deployments/config/robinhood-testnet-v1.json
```

Sync the backend copy and run all gates:

```powershell
cd ..\backend
go run ./cmd/check-deployments
go run github.com/go-task/task/v3/cmd/task@v3.53.1 verify
```

Only a reviewed manifest in `config/` may replace the disabled marker in a release commit.
Delete the pending marker only in the same reviewed commit that adds the active record; do
not commit the pending filename.
The first testnet acceptance run must then use fresh funded test wallets and record the
exact manifest digest, deployment block, transaction hashes, API/indexer health output, and
reorg/finality observations. Those wallets and funding are operator actions and must not be
committed.

## Engine implementation upgrade (2026-10-05 graduation-DoS fix)

The deployed v1 curve implementation `0x979b9b8172fba6daec305e388fb500b32f740497` accepts a
buy whose token recipient is the canonical pair, which freezes graduation permanently. The
fix is a new `BondingCurveV1` implementation registered for engine version 1. Clones that
already exist keep the old code and stay vulnerable; only launches made after the switch are
protected.

`contracts/script/DeployCurveImplementation.s.sol` broadcasts only the implementation
creation. On the forked chain it then switches the engine as the factory timelock and proves
that a 1 gwei buy delivered to a fresh launch's pair reverts with `InvalidRecipient`; the
engine switch itself stays a separate, timelock-signed transaction.

Set process-only values (the timelock is currently an EOA, not a contract):

```powershell
$env:ROBINHOOD_TESTNET_RPC_URL = "https://rpc.testnet.chain.robinhood.com"
$env:DEPLOYMENT_TARGET = "robinhood-testnet"
$env:DEPLOYER = "0x12cB30400339831107589695E5C71455e223Bf02"
$env:LAUNCH_FACTORY = "0xedddb61a53226ffdc6ecf7c042b803e769168d55"
$timelock = "0x02baFf7cc315e615CE4E909ECd155ca27FA07460"
$account = "<named Foundry keystore account for the deployer>"
$timelockAccount = "<named Foundry keystore account for the timelock>"
```

1. Dry-run (no broadcast). It must print `Script ran successfully`, the previous and new
   implementation, the runtime code hash, and the `configureEngine` calldata:

   ```powershell
   cd contracts
   forge script script/DeployCurveImplementation.s.sol --rpc-url $env:ROBINHOOD_TESTNET_RPC_URL --sender $env:DEPLOYER
   ```

   The 2026-10-05 dry-run from deployer nonce state at that time predicted implementation
   `0xB3b8c02516de5146e53Bcbcf3d5F2Bf229B380e2`, runtime code hash
   `0x0b1adceec8dc3b6c51778e71435d5ae41c00942a5ce477c4169e2b21a63732f6`, and about
   0.00008 ETH of gas. The address changes if the deployer sends any other transaction first.

2. Broadcast the implementation with the deployer account:

   ```powershell
   forge script script/DeployCurveImplementation.s.sol --rpc-url $env:ROBINHOOD_TESTNET_RPC_URL --sender $env:DEPLOYER --account $account --broadcast
   $impl = "<New curve implementation from the output>"
   cast codehash $impl --rpc-url $env:ROBINHOOD_TESTNET_RPC_URL
   cast call $impl "ENGINE_VERSION()(uint16)" --rpc-url $env:ROBINHOOD_TESTNET_RPC_URL
   ```

   The code hash must equal the dry-run hash and the engine version must be `1`.

3. Give the timelock EOA gas for one call, then switch the engine from the timelock:

   ```powershell
   cast send $timelock --value 0.0002ether --rpc-url $env:ROBINHOOD_TESTNET_RPC_URL --account $account
   cast send $env:LAUNCH_FACTORY "configureEngine(uint16,address,bool)" 1 $impl true --rpc-url $env:ROBINHOOD_TESTNET_RPC_URL --account $timelockAccount
   cast call $env:LAUNCH_FACTORY "curveImplementation(uint16)(address)" 1 --rpc-url $env:ROBINHOOD_TESTNET_RPC_URL
   cast call $env:LAUNCH_FACTORY "engineEnabled(uint16)(bool)" 1 --rpc-url $env:ROBINHOOD_TESTNET_RPC_URL
   ```

   The factory must return the new implementation and `true`.

4. Update the reviewed manifest: in `contracts/deployments/robinhood-testnet-v1.json` set
   `curveImplementation` to the new address and `bytecodeHashes.bondingCurveV1` to its code
   hash. Copy the file byte-identically to `backend/deployments/testdata/`, regenerate the web
   bindings, and run the gates; the indexer verifies the live implementation code hash against
   the manifest at startup:

   ```powershell
   Copy-Item deployments/robinhood-testnet-v1.json ../backend/deployments/testdata/robinhood-testnet-v1.json
   cd ../web; node scripts/generate-contracts.mjs
   cd ../backend; go run ./cmd/check-deployments
   ```

   Record the implementation creation and `configureEngine` transaction hashes in the
   change description. Restart any running indexer after the manifest change.

## Stop conditions

Stop and keep the chain disabled when the RPC is not chain 46630, either dependency has no
runtime code, the Pair init-code hash differs, a receipt cannot be matched to the candidate,
the explorer/RPC evidence is incomplete, authority ownership is unresolved, or any gate
fails. Never use `robinhood-mainnet.json` values as testnet substitutes.

After the run, clear process-only values:

```powershell
Remove-Item Env:ROBINHOOD_TESTNET_RPC_URL -ErrorAction SilentlyContinue
Remove-Variable deployer, account, candidate -ErrorAction SilentlyContinue
```

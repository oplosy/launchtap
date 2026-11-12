# External V4 ABI fragments for the browser

Reviewed, minimal ABI fragments that the web bundle signs against (Plan v2-C Task 1). Each file
holds only the entries the browser calls or decodes, so `web/scripts/generate-contracts.mjs`
copies them without an allowlist.

| File | Entries | Source revision |
|------|---------|-----------------|
| `Permit2.json` | `approve`, `allowance`, `AllowanceExpired`, `InsufficientAllowance` | `IAllowanceTransfer` from `lib/v4-periphery/lib/permit2` (canonical Permit2, unchanged across revisions) |
| `V4Quoter.json` | `quoteExactInputSingle`, `UnexpectedRevertBytes`, `NotEnoughLiquidity` | `IV4Quoter` in `lib/v4-periphery` (`QuoteExactSingleParams` is the same in the deployed v4 periphery) |
| `UniversalRouter.json` | `execute(bytes,bytes[],uint256)` and its revert errors | Universal Router v2.1.2 (`Uniswap/universal-router` release/2.1.x, v4-periphery `545a5d2a`), the reviewed record in `deployments/config/robinhood-mainnet-v4.json` |

The `V4_SWAP` input encoding itself lives in `web/src/v4-swap.ts`, which names the router
revision it mirrors (Plan v2-C D2). Regenerate the web contract surface after any change here
with `cd web && npm run web-contracts-sync`.

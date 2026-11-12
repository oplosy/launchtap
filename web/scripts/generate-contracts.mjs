import { mkdir, readFile, writeFile } from "node:fs/promises";
import { join, resolve } from "node:path";
import { deploymentEntry, filterAbi } from "./contracts-model.mjs";

const root = resolve(import.meta.dirname, "../..");
const target = join(import.meta.dirname, "../src/contracts/generated.ts");
const check = process.argv.includes("--check");
await mkdir(join(import.meta.dirname, "../src/contracts"), { recursive: true });
const allow = {
  factory: new Set([
    "launch",
    "launchFee",
    "futureDefaults",
    "engineEnabled",
    "curveImplementation",
    "launchesPaused",
    "tradingPaused",
    "TokenLaunched",
    "DeadlineExpired",
    "LaunchValueMismatch",
    "LaunchesPaused",
    "SlippageExceeded",
    "UnknownEngine",
    "EngineDisabled",
    "EngineVersionMismatch",
    "DeveloperBuyCapExceeded",
    "InvalidEngineImplementation",
    "EthTransferFailed",
    "TokenTransferFailed",
    "WethTransferFailed",
    "UnauthorizedFactory",
    "UnauthorizedPauseAuthority",
    "UnauthorizedTimelock",
    "InvalidAuthority",
    "ZeroAddress",
    "InvalidRecipient",
    "NotInitialized",
    "AlreadyInitialized",
    "ImplementationInitializationDisabled",
    "AlreadyGraduated",
    "InvalidSupplyAllocation",
    "InvalidCurveAllocation",
    "InvalidGraduationEth",
    "InvalidVirtualReserves",
    "InvalidTradeFeeBps",
    "InvalidProtocolShareBps",
    "InvalidCurveBoundary",
    "ArithmeticOverflow",
    "DivisionByZero",
    "UnauthorizedCurve",
    "WrongPhase",
    "ZeroInput",
    "ZeroOutput",
    "Oversell",
    "PairNotCanonical",
    "PairFactoryMismatch",
    "PairTokensMismatch",
    "PairSupplyNotZero",
    "PairTokenReserveNotZero",
    "PairTokenBalanceNotZero",
    "PairLiquidityZero",
    "TradingPaused",
    "UnauthorizedCreatorClaim",
    "UnauthorizedProtocolClaim",
    "NothingToClaim",
    "TransferRestricted",
    "AccountingInvariantFailed",
    "CurveInvariantFailed",
    "ReentrancyGuardReentrantCall",
    "InsufficientBalance",
    "FailedDeployment",
    "UnauthorizedPauseAuthority",
    "UnauthorizedTimelock",
  ]),
  curve: new Set([
    "buy",
    "sell",
    "quoteBuy",
    "quoteSell",
    "claimCreatorFees",
    "claimCreatorFeesTo",
    "claimRefund",
    "claimRefundTo",
    "unclaimedCreatorFees",
    "pendingRefund",
    "creator",
    "phase",
    "DeadlineExpired",
    "ERC20InsufficientAllowance",
    "ERC20InsufficientBalance",
    "NothingToClaim",
    "Oversell",
    "SlippageExceeded",
    "TradingPaused",
    "WrongPhase",
    "UnknownEngine",
    "EngineDisabled",
    "EngineVersionMismatch",
    "DeveloperBuyCapExceeded",
    "AccountingInvariantFailed",
    "AlreadyGraduated",
    "CurveInvariantFailed",
    "EthTransferFailed",
    "TokenTransferFailed",
    "WethTransferFailed",
    "PairNotCanonical",
    "PairFactoryMismatch",
    "PairTokensMismatch",
    "PairSupplyNotZero",
    "PairTokenReserveNotZero",
    "PairTokenBalanceNotZero",
    "PairLiquidityZero",
    "UnauthorizedCreatorClaim",
    "UnauthorizedProtocolClaim",
    "TransferRestricted",
    "TradingPaused",
    "LaunchesPaused",
    "ZeroInput",
    "ZeroOutput",
    "InvalidRecipient",
    "ArithmeticOverflow",
    "DivisionByZero",
    "InvalidAuthority",
    "UnauthorizedCurve",
    "ReentrancyGuardReentrantCall",
  ]),
  token: new Set([
    "allowance",
    "approve",
    "balanceOf",
    "decimals",
    "name",
    "symbol",
    "totalSupply",
    "ERC20InsufficientAllowance",
    "ERC20InsufficientBalance",
  ]),
};

const allowV2 = {
  factory: new Set([
    "launch",
    "launchFee",
    "futureDefaults",
    "launchesPaused",
    "tradingPaused",
    "TokenLaunched",
  ]),
  curve: new Set([
    "buy",
    "sell",
    "quoteBuy",
    "quoteSell",
    "claimCreatorFees",
    "claimRefund",
    "creatorFeesOwed",
    "pendingRefund",
    "creator",
    "phase",
    "launchTime",
    "currentTaxBps",
    "antiSnipeProfile",
    "lpCreatorShareBps",
    "feeRecipient",
    "pendingFeeRecipient",
    "proposeFeeRecipient",
    "cancelFeeRecipientProposal",
    "acceptFeeRecipient",
  ]),
  token: new Set([
    "allowance",
    "approve",
    "balanceOf",
    "decimals",
    "name",
    "symbol",
    "totalSupply",
  ]),
  locker: new Set(["claim", "owed", "positionOf"]),
};

async function readJson(path) {
  return JSON.parse(await readFile(join(root, path), "utf8"));
}

async function abi(path, names, options) {
  return filterAbi(await readJson(path), names, options);
}

const factory = await abi("contracts/abi/v1/LaunchFactory.json", allow.factory);
const token = await abi("contracts/abi/v1/LaunchToken.json", allow.token);
const curve = await abi("contracts/out/BondingCurveV1.sol/BondingCurveV1.json", allow.curve);
const routerDocument = await readJson("contracts/abi/v1/UniswapV2Router02.json");
const router = routerDocument.filter(
  (entry) =>
    entry.type === "function" &&
    new Set(["WETH", "getAmountsOut", "swapExactETHForTokens", "swapExactTokensForETH"]).has(
      entry.name,
    ),
);

// Engine v2 (Plan v2-C Task 1): every error is kept so decodeTransactionError can name it.
const v2 = { allErrors: true };
const factoryV2 = await abi("contracts/abi/v2/LaunchFactoryV2.json", allowV2.factory, v2);
const curveV2 = await abi("contracts/abi/v2/BondingCurveV2.json", allowV2.curve, v2);
const tokenV2 = await abi("contracts/abi/v2/LaunchTokenV2.json", allowV2.token, v2);
const locker = await abi("contracts/abi/v2/LpFeeLocker.json", allowV2.locker, v2);
const universalRouter = await readJson("contracts/abi/v2/external/UniversalRouter.json");
const v4Quoter = await readJson("contracts/abi/v2/external/V4Quoter.json");
const permit2 = await readJson("contracts/abi/v2/external/Permit2.json");

// Reviewed V4 dependency records, keyed by chain. An engine-2 manifest is joined with the
// record for its chain; no reviewed engine-2 manifest exists on a live chain yet.
const v4Records = new Map();
for (const name of ["config/robinhood-mainnet-v4.json"]) {
  const record = await readJson(`contracts/deployments/${name}`);
  if (record.reviewed === true) v4Records.set(record.chainId, record);
}

const manifests = [];
for (const source of [
  { name: "config/robinhood-mainnet.json", enabled: false },
  { name: "robinhood-testnet-v1.json", enabled: true },
]) {
  const document = await readJson(`contracts/deployments/${source.name}`);
  manifests.push(
    deploymentEntry({
      document,
      enabled: source.enabled,
      v4Record: v4Records.get(document.chainId) ?? null,
    }),
  );
}

const json = (value) => JSON.stringify(value, null, 2);
const abis = {
  factory,
  curve,
  token,
  router,
  factoryV2,
  curveV2,
  tokenV2,
  locker,
  universalRouter,
  v4Quoter,
  permit2,
};
const generated =
  `// Generated by scripts/generate-contracts.mjs; do not edit.

` +
  `export const browserAbis = ${json(abis)} as const;

` +
  `export type BrowserAbiName = keyof typeof browserAbis;

` +
  `export type ReviewedV4Deployment = {
` +
  `  poolManager: string; permit2: string; universalRouter: string | null; v4Quoter: string | null;
` +
  `  hook: string | null; locker: string | null;
` +
  `};

` +
  `export type ReviewedDeployment = {
` +
  `  deploymentId: string; chainId: number; name: string; engineVersion: 1 | 2; enabled: boolean;
` +
  `  factory: string | null; explorerBase: string | null; weth: string | null;
` +
  `  uniswapV2Factory: string | null; uniswapV2Router02: string | null;
` +
  `  marketLinks: { dexscreenerChain?: string; geckoterminalNetwork?: string } | null;
` +
  `  v4: ReviewedV4Deployment | null;
` +
  `};

` +
  `export const reviewedDeployments: readonly ReviewedDeployment[] = ${json(manifests)};
`;

if (check) {
  const actual = await readFile(target, "utf8");
  if (actual !== generated)
    throw new Error("src/contracts/generated.ts is stale; run npm run web-contracts-sync");
} else {
  await writeFile(target, generated, "utf8");
}

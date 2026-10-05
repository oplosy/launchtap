import { mkdir, readFile, writeFile } from "node:fs/promises";
import { join, resolve } from "node:path";

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

async function abi(path, names) {
  const document = JSON.parse(await readFile(join(root, path), "utf8"));
  const entries = document.abi ?? document;
  return entries.filter(
    (entry) =>
      (entry.type === "function" || entry.type === "error" || entry.type === "event") &&
      names.has(entry.name),
  );
}

const factory = await abi("contracts/abi/v1/LaunchFactory.json", allow.factory);
const token = await abi("contracts/abi/v1/LaunchToken.json", allow.token);
const curve = await abi("contracts/out/BondingCurveV1.sol/BondingCurveV1.json", allow.curve);
const routerDocument = JSON.parse(
  await readFile(join(root, "contracts/abi/v1/UniswapV2Router02.json"), "utf8"),
);
const router = routerDocument.filter(
  (entry) =>
    entry.type === "function" &&
    new Set(["WETH", "getAmountsOut", "swapExactETHForTokens", "swapExactTokensForETH"]).has(
      entry.name,
    ),
);

const manifests = [];
for (const source of [
  { name: "config/robinhood-mainnet.json", enabled: false },
  { name: "robinhood-testnet-v1.json", enabled: true },
]) {
  const document = JSON.parse(
    await readFile(join(root, "contracts/deployments", source.name), "utf8"),
  );
  manifests.push({
    deploymentId: document.deploymentId ?? document.target,
    chainId: document.chainId,
    name: document.name ?? document.target,
    enabled: source.enabled && Boolean(document.factory),
    factory: document.factory ?? null,
    explorerBase: document.explorerBase ?? null,
    weth: document.weth ?? null,
    uniswapV2Factory: document.uniswapV2Factory ?? null,
    uniswapV2Router02: document.uniswapV2Router02 ?? null,
  });
}

const json = (value) => JSON.stringify(value, null, 2);
const generated =
  `// Generated by scripts/generate-contracts.mjs; do not edit.\n\n` +
  `export const browserAbis = ${json({ factory, curve, token, router })} as const;\n\n` +
  `export type BrowserAbiName = keyof typeof browserAbis;\n\n` +
  `export type ReviewedDeployment = {\n` +
  `  deploymentId: string; chainId: number; name: string; enabled: boolean; factory: string | null;\n` +
  `  explorerBase: string | null; weth: string | null; uniswapV2Factory: string | null;\n` +
  `  uniswapV2Router02: string | null;\n` +
  `};\n\n` +
  `export const reviewedDeployments: readonly ReviewedDeployment[] = ${json(manifests)};\n`;

if (check) {
  const actual = await readFile(target, "utf8");
  if (actual !== generated)
    throw new Error("src/contracts/generated.ts is stale; run npm run web-contracts-sync");
} else {
  await writeFile(target, generated, "utf8");
}

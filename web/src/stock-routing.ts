import { encodeAbiParameters, getAddress, type Hex } from "viem";
import { NATIVE_ETH, V4_SWAP_COMMAND } from "@/v4-swap";

// Stock token routing (docs/specs/2026-11-24-stock-token-routing-preflight.md): a trader converts a
// Robinhood stock token to native ETH, or back, through reviewed Uniswap v4 routes. No launchpad
// contract is involved; the launch-token trade itself stays on the ETH flows.

type Address = `0x${string}`;

/** Robinhood Chain mainnet; testnet has no stock pools (pre-flight "Facts checked"). */
export const STOCK_ROUTING_CHAIN_ID = 4663;

/** USDG on mainnet: a route hop only, never a user-facing asset (6 decimals). */
export const USDG: Address = "0x5fc5360D0400a0Fd4f2af552ADD042D716F1d168";

/** One v4 pool on a route, hook-less, entered from the previous currency. */
export type RouteHop = { currency: Address; fee: number; tickSpacing: number };

export type StockAsset = {
  symbol: string;
  name: string;
  /** Canonical address from Robinhood's on-chain asset registry (docs.robinhood.com/chain/contracts). */
  address: Address;
  decimals: 18;
  /** Pools from the stock token to native ETH, in that order; the last hop's currency is ETH. */
  toEth: readonly RouteHop[];
};

const USDG_TO_ETH: RouteHop = { currency: NATIVE_ETH, fee: 500, tickSpacing: 10 };

/**
 * The reviewed routes, probed with the mainnet V4Quoter on 2026-11-24: NVDA has its deepest pool
 * against ETH at 0.05%; AAPL and TSLA trade only against USDG (0.3%), which reaches ETH at 0.05%.
 */
export const STOCK_ASSETS: readonly StockAsset[] = [
  {
    symbol: "NVDA",
    name: "NVIDIA",
    address: "0xd0601CE157Db5bdC3162BbaC2a2C8aF5320D9EEC",
    decimals: 18,
    toEth: [{ currency: NATIVE_ETH, fee: 500, tickSpacing: 10 }],
  },
  {
    symbol: "AAPL",
    name: "Apple",
    address: "0xaF3D76f1834A1d425780943C99Ea8A608f8a93f9",
    decimals: 18,
    toEth: [{ currency: USDG, fee: 3000, tickSpacing: 60 }, USDG_TO_ETH],
  },
  {
    symbol: "TSLA",
    name: "Tesla",
    address: "0x322F0929c4625eD5bAd873c95208D54E1c003b2d",
    decimals: 18,
    toEth: [{ currency: USDG, fee: 3000, tickSpacing: 60 }, USDG_TO_ETH],
  },
];

/** `PathKey` of v4-periphery `545a5d2a` (`libraries/PathKey.sol`). */
export type PathKey = {
  intermediateCurrency: Address;
  fee: number;
  tickSpacing: number;
  hooks: Address;
  hookData: Hex;
};

export type StockDirection = "stock-to-eth" | "eth-to-stock";

/** The exact-input path for a conversion: the input currency and the hops that follow it. */
export function stockPath(
  asset: StockAsset,
  direction: StockDirection,
): { currencyIn: Address; currencyOut: Address; path: PathKey[] } {
  const hops = asset.toEth;
  if (hops.at(-1)?.currency !== NATIVE_ETH)
    throw new Error(`Route for ${asset.symbol} does not end in ETH`);
  const key = (currency: Address, hop: RouteHop): PathKey => ({
    intermediateCurrency: currency,
    fee: hop.fee,
    tickSpacing: hop.tickSpacing,
    hooks: NATIVE_ETH,
    hookData: "0x",
  });
  if (direction === "stock-to-eth")
    return {
      currencyIn: asset.address,
      currencyOut: NATIVE_ETH,
      path: hops.map((hop) => key(hop.currency, hop)),
    };
  // Currencies along the route: asset, hop[0].currency, …, ETH. Walking back from ETH, the pool
  // of hop i is entered towards the currency before it (the asset for i = 0).
  const path = hops
    .map((hop, i) => key(i === 0 ? asset.address : hops[i - 1]!.currency, hop))
    .reverse();
  return { currencyIn: NATIVE_ETH, currencyOut: asset.address, path };
}

const pathKeyComponents = [
  { name: "intermediateCurrency", type: "address" },
  { name: "fee", type: "uint24" },
  { name: "tickSpacing", type: "int24" },
  { name: "hooks", type: "address" },
  { name: "hookData", type: "bytes" },
] as const;

/** `IV4Quoter.QuoteExactParams` (v4-periphery `545a5d2a`) for `quoteExactInput`. */
export function quoteExactParams(
  route: { currencyIn: Address; path: PathKey[] },
  exactAmount: bigint,
) {
  return { exactCurrency: route.currencyIn, path: route.path, exactAmount };
}

const SWAP_EXACT_IN = 0x07;
const SETTLE_ALL = 0x0c;
const TAKE_ALL = 0x0f;
const UINT128_MAX = (1n << 128n) - 1n;

/**
 * Encodes `UniversalRouter.execute` inputs for one exact-input multi-hop swap: `V4_SWAP` with
 * `SWAP_EXACT_IN`, `SETTLE_ALL` (input currency, at most `amountIn`) and `TAKE_ALL` (output
 * currency, at least `amountOutMinimum`).
 *
 * Mirrors `IV4Router.ExactInputParams` of the reviewed Universal Router v2.1.2 (v4-periphery
 * `545a5d2a`): `(Currency currencyIn, PathKey[] path, uint256[] minHopPriceX36, uint128 amountIn,
 * uint128 amountOutMinimum)`. `minHopPriceX36` is empty (off): `amountOutMinimum` bounds the whole
 * path, and `V4Router._swapExactInput` accepts an empty array.
 */
export function encodeV4ExactIn(args: {
  currencyIn: Address;
  currencyOut: Address;
  path: PathKey[];
  amountIn: bigint;
  amountOutMinimum: bigint;
}): { commands: Hex; inputs: readonly [Hex] } {
  if (args.path.length === 0) throw new Error("Swap path must not be empty");
  if (args.path.at(-1)!.intermediateCurrency !== args.currencyOut)
    throw new Error("Swap path must end in the output currency");
  if (args.amountIn <= 0n) throw new Error("Swap input must be greater than zero");
  if (
    args.amountIn > UINT128_MAX ||
    args.amountOutMinimum > UINT128_MAX ||
    args.amountOutMinimum < 0n
  )
    throw new Error("Swap amounts must fit uint128");
  const swap = encodeAbiParameters(
    [
      {
        type: "tuple",
        components: [
          { name: "currencyIn", type: "address" },
          { name: "path", type: "tuple[]", components: pathKeyComponents },
          { name: "minHopPriceX36", type: "uint256[]" },
          { name: "amountIn", type: "uint128" },
          { name: "amountOutMinimum", type: "uint128" },
        ],
      },
    ],
    [
      {
        currencyIn: args.currencyIn,
        path: args.path,
        minHopPriceX36: [],
        amountIn: args.amountIn,
        amountOutMinimum: args.amountOutMinimum,
      },
    ],
  );
  const settle = encodeAbiParameters(
    [{ type: "address" }, { type: "uint256" }],
    [args.currencyIn, args.amountIn],
  );
  const take = encodeAbiParameters(
    [{ type: "address" }, { type: "uint256" }],
    [args.currencyOut, args.amountOutMinimum],
  );
  const actions = `0x${[SWAP_EXACT_IN, SETTLE_ALL, TAKE_ALL]
    .map((action) => action.toString(16).padStart(2, "0"))
    .join("")}` as Hex;
  const input = encodeAbiParameters(
    [{ type: "bytes" }, { type: "bytes[]" }],
    [actions, [swap, settle, take]],
  );
  return { commands: `0x${V4_SWAP_COMMAND.toString(16).padStart(2, "0")}`, inputs: [input] };
}

/** The reference quote is taken for 1/REFERENCE_DIVISOR of the input. */
export const REFERENCE_DIVISOR = 1000n;
/** Above this the panel warns; above the block level it refuses to review (pre-flight D4). */
export const PRICE_IMPACT_WARN_BPS = 300n;
export const PRICE_IMPACT_BLOCK_BPS = 1500n;

/** The input whose quote serves as the marginal-price reference, never zero. */
export function referenceInput(amountIn: bigint): bigint {
  const reference = amountIn / REFERENCE_DIVISOR;
  return reference > 0n ? reference : 1n;
}

/**
 * How far the full quote falls short of the reference rate, in basis points, rounded up so the
 * guard never understates the impact. 0 when the full quote is at or above the reference rate.
 */
export function priceImpactBps(args: {
  amountIn: bigint;
  amountOut: bigint;
  referenceIn: bigint;
  referenceOut: bigint;
}): bigint {
  if (args.amountIn <= 0n || args.referenceIn <= 0n) throw new Error("Inputs must be positive");
  if (args.referenceOut <= 0n) return 10_000n;
  // expected = amountIn × referenceOut / referenceIn; impact = 1 − amountOut / expected.
  const expectedScaled = args.amountIn * args.referenceOut;
  const actualScaled = args.amountOut * args.referenceIn;
  if (actualScaled >= expectedScaled) return 0n;
  const shortfall = expectedScaled - actualScaled;
  return (shortfall * 10_000n + expectedScaled - 1n) / expectedScaled;
}

export type ImpactLevel = "ok" | "warn" | "block";

export function impactLevel(bps: bigint): ImpactLevel {
  if (bps > PRICE_IMPACT_BLOCK_BPS) return "block";
  if (bps > PRICE_IMPACT_WARN_BPS) return "warn";
  return "ok";
}

/**
 * Stock routing renders only when the flag is set, the site runs on Robinhood Chain mainnet and
 * the reviewed V4 router, quoter and Permit2 are available (pre-flight D5).
 */
export function stockRoutingEnabled(
  chainId: number | null,
  hasReviewedV4: boolean,
  env: { NEXT_PUBLIC_STOCK_ROUTING?: string } = {
    NEXT_PUBLIC_STOCK_ROUTING: process.env.NEXT_PUBLIC_STOCK_ROUTING,
  },
): boolean {
  return (
    env.NEXT_PUBLIC_STOCK_ROUTING === "1" && chainId === STOCK_ROUTING_CHAIN_ID && hasReviewedV4
  );
}

/** Registry self-check used by tests: checksummed, unique, routes ending in ETH. */
export function assertRegistry(assets: readonly StockAsset[]): void {
  const seen = new Set<string>();
  for (const asset of assets) {
    if (getAddress(asset.address) !== asset.address)
      throw new Error(`${asset.symbol} address is not checksummed`);
    if (seen.has(asset.address)) throw new Error(`${asset.symbol} is listed twice`);
    seen.add(asset.address);
    stockPath(asset, "stock-to-eth");
  }
}

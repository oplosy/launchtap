import { encodeAbiParameters, keccak256, type Hex } from "viem";

// Engine-2 pool trading through the Uniswap Universal Router (Plan v2-C Tasks 4 and 5).

type Address = `0x${string}`;

export const NATIVE_ETH = "0x0000000000000000000000000000000000000000" as const;
/** Pool constants from spec §6.1; never read from the API or a log (Plan v2-C D5). */
export const POOL_FEE = 10_000;
export const POOL_TICK_SPACING = 200;

export type PoolKey = {
  currency0: Address;
  currency1: Address;
  fee: number;
  tickSpacing: number;
  hooks: Address;
};

const poolKeyComponents = [
  { name: "currency0", type: "address" },
  { name: "currency1", type: "address" },
  { name: "fee", type: "uint24" },
  { name: "tickSpacing", type: "int24" },
  { name: "hooks", type: "address" },
] as const;

export function poolKey(token: Address, hook: Address): PoolKey {
  return {
    currency0: NATIVE_ETH,
    currency1: token,
    fee: POOL_FEE,
    tickSpacing: POOL_TICK_SPACING,
    hooks: hook,
  };
}

/** Mirrors v4-core `PoolIdLibrary.toId`: keccak256 of the ABI-encoded key. */
export function poolId(key: PoolKey): Hex {
  return keccak256(
    encodeAbiParameters(
      [{ type: "tuple", components: poolKeyComponents }],
      [{ ...key, fee: key.fee, tickSpacing: key.tickSpacing }],
    ),
  );
}

/** A pool is tradable only when the API's pool id equals the locally computed one. */
export function poolMatchesApi(
  token: Address,
  hook: Address,
  pool: { pool_id: string } | null | undefined,
): boolean {
  if (!pool) return false;
  return pool.pool_id.toLowerCase() === poolId(poolKey(token, hook)).toLowerCase();
}

/** Universal Router command and V4Router action ids (`Commands.V4_SWAP`, `Actions.*`). */
export const V4_SWAP_COMMAND = 0x10;
const SWAP_EXACT_IN_SINGLE = 0x06;
const SETTLE_ALL = 0x0c;
const TAKE_ALL = 0x0f;

const UINT128_MAX = (1n << 128n) - 1n;

/**
 * Encodes `UniversalRouter.execute` inputs for one exact-input single-pool swap
 * (Plan v2-C D4): `V4_SWAP` with `SWAP_EXACT_IN_SINGLE`, `SETTLE_ALL` (input currency, at most
 * `amountIn`) and `TAKE_ALL` (output currency, at least `amountOutMinimum`).
 *
 * Mirrors the `IV4Router.ExactInputSingleParams` layout of the reviewed Universal Router
 * v2.1.2 (`Uniswap/universal-router` release/2.1.x, v4-periphery 545a5d2a, Plan v2-C D2):
 * `(PoolKey poolKey, bool zeroForOne, uint128 amountIn, uint128 amountOutMinimum,
 * uint256 minHopPriceX36, bytes hookData)`. `minHopPriceX36` is 0 (off): `amountOutMinimum`
 * already bounds the single hop. The Anvil v2 web gate signs this through that router.
 */
export function encodeV4ExactInSingle(args: {
  key: PoolKey;
  zeroForOne: boolean;
  amountIn: bigint;
  amountOutMinimum: bigint;
}): { commands: Hex; inputs: readonly [Hex] } {
  if (args.amountIn <= 0n) throw new Error("Swap input must be greater than zero");
  if (
    args.amountIn > UINT128_MAX ||
    args.amountOutMinimum > UINT128_MAX ||
    args.amountOutMinimum < 0n
  )
    throw new Error("Swap amounts must fit uint128");
  const [currencyIn, currencyOut] = args.zeroForOne
    ? [args.key.currency0, args.key.currency1]
    : [args.key.currency1, args.key.currency0];
  const swap = encodeAbiParameters(
    [
      {
        type: "tuple",
        components: [
          { name: "poolKey", type: "tuple", components: poolKeyComponents },
          { name: "zeroForOne", type: "bool" },
          { name: "amountIn", type: "uint128" },
          { name: "amountOutMinimum", type: "uint128" },
          { name: "minHopPriceX36", type: "uint256" },
          { name: "hookData", type: "bytes" },
        ],
      },
    ],
    [
      {
        poolKey: args.key,
        zeroForOne: args.zeroForOne,
        amountIn: args.amountIn,
        amountOutMinimum: args.amountOutMinimum,
        minHopPriceX36: 0n,
        hookData: "0x",
      },
    ],
  );
  const settle = encodeAbiParameters(
    [{ type: "address" }, { type: "uint256" }],
    [currencyIn, args.amountIn],
  );
  const take = encodeAbiParameters(
    [{ type: "address" }, { type: "uint256" }],
    [currencyOut, args.amountOutMinimum],
  );
  const actions = `0x${[SWAP_EXACT_IN_SINGLE, SETTLE_ALL, TAKE_ALL]
    .map((action) => action.toString(16).padStart(2, "0"))
    .join("")}` as Hex;
  const input = encodeAbiParameters(
    [{ type: "bytes" }, { type: "bytes[]" }],
    [actions, [swap, settle, take]],
  );
  return { commands: `0x${V4_SWAP_COMMAND.toString(16).padStart(2, "0")}`, inputs: [input] };
}

export type Permit2SellStep = "reset-erc20" | "approve-erc20" | "approve-permit2" | "execute";

/**
 * The writes a sell still needs (Plan v2-C D3). The ERC-20 allowance to Permit2 and the Permit2
 * allowance to the router must both equal the sell amount, and the Permit2 allowance must
 * expire within the transaction TTL; anything larger or longer-lived is re-approved exactly.
 */
export function planPermit2Sell(args: {
  amount: bigint;
  erc20Allowance: bigint;
  permit2: { amount: bigint; expiration: bigint };
  nowSeconds: bigint;
  ttlSeconds: bigint;
}): Permit2SellStep[] {
  const steps: Permit2SellStep[] = [];
  if (args.erc20Allowance !== args.amount) {
    if (args.erc20Allowance !== 0n) steps.push("reset-erc20");
    steps.push("approve-erc20");
  }
  const permit2Exact =
    args.permit2.amount === args.amount &&
    args.permit2.expiration > args.nowSeconds &&
    args.permit2.expiration <= args.nowSeconds + args.ttlSeconds;
  if (!permit2Exact) steps.push("approve-permit2");
  steps.push("execute");
  return steps;
}

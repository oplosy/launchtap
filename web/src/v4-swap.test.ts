import { readFileSync } from "node:fs";
import { join } from "node:path";
import { decodeAbiParameters, getAddress, keccak256, toEventSelector, type Hex } from "viem";
import { describe, expect, it } from "vitest";
import {
  V4_SWAP_COMMAND,
  encodeV4ExactInSingle,
  planPermit2Sell,
  poolId,
  poolKey,
  poolMatchesApi,
} from "./v4-swap";

type FixtureLog = { emitterKind: string; topics: Hex[]; data: Hex };
const fixture = JSON.parse(
  readFileSync(join(__dirname, "../../contracts/fixtures/v2/event-logs-v2.json"), "utf8"),
) as { logs: FixtureLog[] };
const initializeTopic = toEventSelector(
  "Initialize(bytes32,address,address,uint24,int24,address,uint160,int24)",
);

const token = "0x00000000000000000000000000000000000000aa" as const;
const hook = "0x0000000000000000000000000000000000002000" as const;

describe("V4 pool key and id", () => {
  it("matches the PoolManager Initialize log of the v2 fixture", () => {
    const initialize = fixture.logs.find(
      (log) => log.emitterKind === "poolManager" && log.topics[0] === initializeTopic,
    );
    expect(initialize).toBeDefined();
    const [fee, tickSpacing, hooks] = decodeAbiParameters(
      [
        { type: "uint24" },
        { type: "int24" },
        { type: "address" },
        { type: "uint160" },
        { type: "int24" },
      ],
      initialize!.data,
    );
    expect(fee).toBe(10_000);
    expect(tickSpacing).toBe(200);
    const fixtureToken = getAddress(`0x${initialize!.topics[3]!.slice(26)}`);
    expect(BigInt(initialize!.topics[2]!)).toBe(0n);
    expect(poolId(poolKey(fixtureToken, hooks))).toBe(initialize!.topics[1]);
  });

  it("builds the spec pool key: native ETH first, 1% fee, tick spacing 200", () => {
    expect(poolKey(token, hook)).toEqual({
      currency0: "0x0000000000000000000000000000000000000000",
      currency1: token,
      fee: 10_000,
      tickSpacing: 200,
      hooks: hook,
    });
  });

  it("only trades a pool whose API id equals the locally computed one", () => {
    const id = poolId(poolKey(token, hook));
    expect(poolMatchesApi(token, hook, { pool_id: id })).toBe(true);
    expect(poolMatchesApi(token, hook, { pool_id: id.toUpperCase().replace("0X", "0x") })).toBe(
      true,
    );
    expect(poolMatchesApi(token, hook, { pool_id: keccak256("0x01") })).toBe(false);
    expect(poolMatchesApi(token, hook, null)).toBe(false);
  });
});

describe("Universal Router V4_SWAP encoding", () => {
  const key = poolKey(token, hook);

  it("encodes one V4_SWAP command with swap, settle-all and take-all actions", () => {
    const encoded = encodeV4ExactInSingle({
      key,
      zeroForOne: true,
      amountIn: 5n * 10n ** 17n,
      amountOutMinimum: 123n,
    });
    expect(encoded.commands).toBe(`0x${V4_SWAP_COMMAND.toString(16).padStart(2, "0")}`);
    expect(encoded.inputs).toHaveLength(1);
    const [actions, params] = decodeAbiParameters(
      [{ type: "bytes" }, { type: "bytes[]" }],
      encoded.inputs[0]!,
    );
    expect(actions).toBe("0x060c0f");
    expect(params).toHaveLength(3);
    // V4Router's calldata decoder needs nine static words before hookData (0x160 bytes minimum).
    expect((params[0]!.length - 2) / 2).toBeGreaterThanOrEqual(0x160);
    const [swap] = decodeAbiParameters(
      [
        {
          type: "tuple",
          components: [
            {
              name: "poolKey",
              type: "tuple",
              components: [
                { name: "currency0", type: "address" },
                { name: "currency1", type: "address" },
                { name: "fee", type: "uint24" },
                { name: "tickSpacing", type: "int24" },
                { name: "hooks", type: "address" },
              ],
            },
            { name: "zeroForOne", type: "bool" },
            { name: "amountIn", type: "uint128" },
            { name: "amountOutMinimum", type: "uint128" },
            { name: "minHopPriceX36", type: "uint256" },
            { name: "hookData", type: "bytes" },
          ],
        },
      ],
      params[0]!,
    );
    expect(swap.zeroForOne).toBe(true);
    expect(swap.amountIn).toBe(5n * 10n ** 17n);
    expect(swap.amountOutMinimum).toBe(123n);
    // Off: the minimum output already bounds the swap (router 2.1.2, v4-periphery 545a5d2a).
    expect(swap.minHopPriceX36).toBe(0n);
    expect(swap.hookData).toBe("0x");
    expect(getAddress(swap.poolKey.currency1)).toBe(getAddress(token));
    const settle = decodeAbiParameters([{ type: "address" }, { type: "uint256" }], params[1]!);
    const take = decodeAbiParameters([{ type: "address" }, { type: "uint256" }], params[2]!);
    expect(settle).toEqual(["0x0000000000000000000000000000000000000000", 5n * 10n ** 17n]);
    expect(take[0].toLowerCase()).toBe(token);
    expect(take[1]).toBe(123n);
  });

  it("settles the token and takes ETH on a sell", () => {
    const encoded = encodeV4ExactInSingle({
      key,
      zeroForOne: false,
      amountIn: 1000n,
      amountOutMinimum: 7n,
    });
    const [, params] = decodeAbiParameters(
      [{ type: "bytes" }, { type: "bytes[]" }],
      encoded.inputs[0]!,
    );
    const settle = decodeAbiParameters([{ type: "address" }, { type: "uint256" }], params[1]!);
    const take = decodeAbiParameters([{ type: "address" }, { type: "uint256" }], params[2]!);
    expect(settle[0].toLowerCase()).toBe(token);
    expect(settle[1]).toBe(1000n);
    expect(take).toEqual(["0x0000000000000000000000000000000000000000", 7n]);
  });

  it("refuses amounts that do not fit uint128", () => {
    expect(() =>
      encodeV4ExactInSingle({ key, zeroForOne: true, amountIn: 1n << 128n, amountOutMinimum: 0n }),
    ).toThrow(/uint128/);
    expect(() =>
      encodeV4ExactInSingle({ key, zeroForOne: true, amountIn: 0n, amountOutMinimum: 0n }),
    ).toThrow(/zero/);
  });
});

describe("Permit2 sell steps", () => {
  const now = 1_800_000_000n;
  const ttl = 900n;
  const base = { amount: 500n, nowSeconds: now, ttlSeconds: ttl };

  it("approves Permit2 and the router exactly when nothing is approved", () => {
    expect(
      planPermit2Sell({ ...base, erc20Allowance: 0n, permit2: { amount: 0n, expiration: 0n } }),
    ).toEqual(["approve-erc20", "approve-permit2", "execute"]);
  });

  it("resets a non-exact ERC-20 allowance to zero before the exact approval", () => {
    expect(
      planPermit2Sell({
        ...base,
        erc20Allowance: 10n ** 30n,
        permit2: { amount: 0n, expiration: 0n },
      }),
    ).toEqual(["reset-erc20", "approve-erc20", "approve-permit2", "execute"]);
  });

  it("skips steps already satisfied exactly and within the TTL", () => {
    expect(
      planPermit2Sell({
        ...base,
        erc20Allowance: 500n,
        permit2: { amount: 500n, expiration: now + ttl },
      }),
    ).toEqual(["execute"]);
  });

  it("does not trust a larger, expired or longer-lived Permit2 allowance", () => {
    for (const permit2 of [
      { amount: 501n, expiration: now + 60n },
      { amount: 500n, expiration: now },
      { amount: 500n, expiration: now + ttl + 1n },
    ]) {
      expect(planPermit2Sell({ ...base, erc20Allowance: 500n, permit2 })).toEqual([
        "approve-permit2",
        "execute",
      ]);
    }
  });
});

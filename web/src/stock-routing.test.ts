import { decodeAbiParameters } from "viem";
import { describe, expect, it } from "vitest";
import { decodeTransactionError } from "./transactions";
import { NATIVE_ETH } from "./v4-swap";
import {
  STOCK_ASSETS,
  USDG,
  assertRegistry,
  encodeV4ExactIn,
  impactLevel,
  priceImpactBps,
  referenceInput,
  stockPath,
  stockRoutingEnabled,
} from "./stock-routing";

const asset = (symbol: string) => STOCK_ASSETS.find((candidate) => candidate.symbol === symbol)!;

describe("stock registry", () => {
  it("is checksummed, unique and routes every asset to ETH", () => {
    expect(() => assertRegistry(STOCK_ASSETS)).not.toThrow();
    expect(STOCK_ASSETS.map((entry) => entry.symbol)).toEqual(["NVDA", "AAPL", "TSLA"]);
  });

  it("refuses a duplicate or a route that does not end in ETH", () => {
    const nvda = asset("NVDA");
    expect(() => assertRegistry([nvda, nvda])).toThrow("listed twice");
    expect(() =>
      assertRegistry([{ ...nvda, toEth: [{ currency: USDG, fee: 500, tickSpacing: 10 }] }]),
    ).toThrow("does not end in ETH");
  });
});

describe("stockPath", () => {
  it("walks a direct pool both ways", () => {
    const nvda = asset("NVDA");
    expect(stockPath(nvda, "stock-to-eth")).toEqual({
      currencyIn: nvda.address,
      currencyOut: NATIVE_ETH,
      path: [
        {
          intermediateCurrency: NATIVE_ETH,
          fee: 500,
          tickSpacing: 10,
          hooks: NATIVE_ETH,
          hookData: "0x",
        },
      ],
    });
    expect(stockPath(nvda, "eth-to-stock")).toEqual({
      currencyIn: NATIVE_ETH,
      currencyOut: nvda.address,
      path: [
        {
          intermediateCurrency: nvda.address,
          fee: 500,
          tickSpacing: 10,
          hooks: NATIVE_ETH,
          hookData: "0x",
        },
      ],
    });
  });

  it("reverses a two-hop route with each pool's own parameters", () => {
    const aapl = asset("AAPL");
    expect(
      stockPath(aapl, "stock-to-eth").path.map((hop) => [hop.intermediateCurrency, hop.fee]),
    ).toEqual([
      [USDG, 3000],
      [NATIVE_ETH, 500],
    ]);
    expect(
      stockPath(aapl, "eth-to-stock").path.map((hop) => [
        hop.intermediateCurrency,
        hop.fee,
        hop.tickSpacing,
      ]),
    ).toEqual([
      [USDG, 500, 10],
      [aapl.address, 3000, 60],
    ]);
  });
});

describe("encodeV4ExactIn", () => {
  const route = stockPath(asset("TSLA"), "stock-to-eth");

  it("encodes V4_SWAP with SWAP_EXACT_IN, SETTLE_ALL and TAKE_ALL", () => {
    const encoded = encodeV4ExactIn({ ...route, amountIn: 10n ** 18n, amountOutMinimum: 123n });
    expect(encoded.commands).toBe("0x10");
    const [actions, params] = decodeAbiParameters(
      [{ type: "bytes" }, { type: "bytes[]" }],
      encoded.inputs[0],
    );
    expect(actions).toBe("0x070c0f");
    const [swap] = decodeAbiParameters(
      [
        {
          type: "tuple",
          components: [
            { name: "currencyIn", type: "address" },
            {
              name: "path",
              type: "tuple[]",
              components: [
                { name: "intermediateCurrency", type: "address" },
                { name: "fee", type: "uint24" },
                { name: "tickSpacing", type: "int24" },
                { name: "hooks", type: "address" },
                { name: "hookData", type: "bytes" },
              ],
            },
            { name: "minHopPriceX36", type: "uint256[]" },
            { name: "amountIn", type: "uint128" },
            { name: "amountOutMinimum", type: "uint128" },
          ],
        },
      ],
      params[0]!,
    );
    expect(swap.currencyIn).toBe(route.currencyIn);
    expect(swap.path).toHaveLength(2);
    expect(swap.minHopPriceX36).toEqual([]);
    expect(swap.amountIn).toBe(10n ** 18n);
    expect(swap.amountOutMinimum).toBe(123n);
    const [settleCurrency, settleAmount] = decodeAbiParameters(
      [{ type: "address" }, { type: "uint256" }],
      params[1]!,
    );
    expect([settleCurrency, settleAmount]).toEqual([route.currencyIn, 10n ** 18n]);
    const [takeCurrency, takeAmount] = decodeAbiParameters(
      [{ type: "address" }, { type: "uint256" }],
      params[2]!,
    );
    expect([takeCurrency, takeAmount]).toEqual([NATIVE_ETH, 123n]);
  });

  it("refuses empty paths, mismatched outputs and out-of-range amounts", () => {
    expect(() =>
      encodeV4ExactIn({ ...route, path: [], amountIn: 1n, amountOutMinimum: 0n }),
    ).toThrow("empty");
    expect(() =>
      encodeV4ExactIn({ ...route, currencyOut: USDG, amountIn: 1n, amountOutMinimum: 0n }),
    ).toThrow("output currency");
    expect(() => encodeV4ExactIn({ ...route, amountIn: 0n, amountOutMinimum: 0n })).toThrow(
      "greater than zero",
    );
    expect(() => encodeV4ExactIn({ ...route, amountIn: 1n << 128n, amountOutMinimum: 0n })).toThrow(
      "uint128",
    );
    expect(() => encodeV4ExactIn({ ...route, amountIn: 1n, amountOutMinimum: -1n })).toThrow(
      "uint128",
    );
  });
});

describe("price impact", () => {
  it("compares the full quote with the scaled reference quote, rounding up", () => {
    expect(referenceInput(5n)).toBe(1n);
    expect(referenceInput(10_000n)).toBe(10n);
    // Reference rate 2 out per in; 1,000 in should give 2,000, gives 1,900: 5%.
    expect(
      priceImpactBps({ amountIn: 1000n, amountOut: 1900n, referenceIn: 1n, referenceOut: 2n }),
    ).toBe(500n);
    expect(priceImpactBps({ amountIn: 3n, amountOut: 2n, referenceIn: 1n, referenceOut: 1n })).toBe(
      3334n,
    );
    expect(
      priceImpactBps({ amountIn: 1000n, amountOut: 2100n, referenceIn: 1n, referenceOut: 2n }),
    ).toBe(0n);
    expect(priceImpactBps({ amountIn: 1n, amountOut: 0n, referenceIn: 1n, referenceOut: 0n })).toBe(
      10_000n,
    );
    expect(() =>
      priceImpactBps({ amountIn: 0n, amountOut: 0n, referenceIn: 1n, referenceOut: 1n }),
    ).toThrow();
  });

  it("classifies the impact at the warn and block levels", () => {
    expect(impactLevel(300n)).toBe("ok");
    expect(impactLevel(301n)).toBe("warn");
    expect(impactLevel(1500n)).toBe("warn");
    expect(impactLevel(1501n)).toBe("block");
  });
});

describe("price impact errors", () => {
  it("decodes the requote refusal into its own sentence", () => {
    expect(decodeTransactionError(new Error("StockRoutePriceImpactTooHigh")).message).toBe(
      "Price impact is above 15% on this route. Convert a smaller amount.",
    );
  });
});

describe("stockRoutingEnabled", () => {
  it("needs the flag, mainnet and the reviewed V4 addresses", () => {
    const on = { NEXT_PUBLIC_STOCK_ROUTING: "1" };
    expect(stockRoutingEnabled(4663, true, on)).toBe(true);
    expect(stockRoutingEnabled(4663, true, {})).toBe(false);
    expect(stockRoutingEnabled(4663, true, { NEXT_PUBLIC_STOCK_ROUTING: "true" })).toBe(false);
    expect(stockRoutingEnabled(46630, true, on)).toBe(false);
    expect(stockRoutingEnabled(4663, false, on)).toBe(false);
  });
});

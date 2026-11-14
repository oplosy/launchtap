import { describe, expect, it, vi } from "vitest";

vi.mock("./shared", () => ({ useTaxSecondsLeft: () => 42 }));

const { readCurveTradeIntent, useCurveTradeFigures } = await import("./curve-trade-logic");

const curve = "0x00000000000000000000000000000000000000c0" as const;
const account = "0x00000000000000000000000000000000000000a1" as const;

function reader(quote: readonly unknown[], taxBps: unknown = 0n) {
  const calls: { functionName: string; args?: readonly unknown[] }[] = [];
  return {
    calls,
    client: {
      readContract: async (args: { functionName: string; args?: readonly unknown[] }) => {
        calls.push({ functionName: args.functionName, args: args.args });
        return args.functionName === "currentTaxBps" ? taxBps : quote;
      },
    },
  };
}

const common = { abi: [], chainId: 31337, account, curve, toleranceBps: 200n, deadline: 1_000n };

describe("readCurveTradeIntent", () => {
  it("builds an exact engine-1 buy from the contract quote, not the API", async () => {
    // quoteBuy returns (inputUsed, tokensOut, protocolFee, creatorFee).
    const { client, calls } = reader([990n, 10_000n, 5n, 5n]);
    const result = await readCurveTradeIntent({
      ...common,
      publicClient: client,
      engineVersion: 1,
      side: "buy",
      quantity: 1_000n,
    });
    expect(calls).toEqual([{ functionName: "quoteBuy", args: [1_000n] }]);
    expect(result.output).toBe(10_000n);
    expect(result.fee).toBe(10n);
    expect(result.protocolFee).toBe(5n);
    expect(result.inputUsed).toBe(990n);
    expect(result.taxQuote).toEqual({ snipeTax: 0n, taxBps: 0 });
    expect(result.intent).toEqual({
      account,
      target: curve,
      value: 1_000n,
      args: [account, account, 9_800n, 1_000n],
      deadline: 1_000n,
      minimumOutput: 9_800n,
      chainId: 31337,
      functionName: "buy",
    });
  });

  it("builds a sell with no value and the token quantity as input", async () => {
    // quoteSell returns (ethOut, _, protocolFee, creatorFee).
    const { client, calls } = reader([5_000n, 0n, 25n, 25n]);
    const result = await readCurveTradeIntent({
      ...common,
      publicClient: client,
      engineVersion: 2,
      side: "sell",
      quantity: 777n,
    });
    expect(calls.map((call) => call.functionName)).toEqual(["quoteSell"]);
    expect(result.output).toBe(5_000n);
    expect(result.inputUsed).toBe(777n);
    expect(result.intent.value).toBe(0n);
    expect(result.intent.args).toEqual([777n, account, 4_900n, 1_000n]);
    expect(result.intent.functionName).toBe("sell");
  });

  it("reads the anti-snipe tax for an engine-2 buy", async () => {
    const { client, calls } = reader([1_000n, 8_000n, 4n, 4n, 120n], 2_500n);
    const result = await readCurveTradeIntent({
      ...common,
      publicClient: client,
      engineVersion: 2,
      side: "buy",
      quantity: 1_000n,
    });
    expect(calls.map((call) => call.functionName)).toEqual(["quoteBuy", "currentTaxBps"]);
    expect(result.taxQuote).toEqual({ snipeTax: 120n, taxBps: 2_500 });
  });

  it("refuses without a chain and on a malformed or negative quote", async () => {
    const valid = reader([1n, 1n, 0n, 0n]).client;
    await expect(
      readCurveTradeIntent({
        ...common,
        chainId: null,
        publicClient: valid,
        engineVersion: 1,
        side: "buy",
        quantity: 1n,
      }),
    ).rejects.toThrow("ChainUnavailable");
    await expect(
      readCurveTradeIntent({
        ...common,
        publicClient: reader([1n, -5n, 0n, 0n]).client,
        engineVersion: 1,
        side: "buy",
        quantity: 1n,
      }),
    ).rejects.toThrow("Invalid contract output");
    await expect(
      readCurveTradeIntent({
        ...common,
        publicClient: reader([1n, 1.5, 0n, 0n]).client,
        engineVersion: 1,
        side: "buy",
        quantity: 1n,
      }),
    ).rejects.toThrow("Invalid contract output");
    await expect(
      readCurveTradeIntent({
        ...common,
        publicClient: reader([1n, 1n, undefined, 0n]).client,
        engineVersion: 1,
        side: "buy",
        quantity: 1n,
      }),
    ).rejects.toThrow("Invalid protocol fee");
  });
});

describe("useCurveTradeFigures", () => {
  const base = {
    side: "buy" as const,
    slippageBps: 200n,
    quoteInputUsed: 1_000n,
    antiSnipeEndsAt: null,
    spotPriceWei: "100000000000000000",
    publicClient: undefined as never,
  };

  it("returns nothing to show without a quote", () => {
    expect(useCurveTradeFigures({ ...base, quote: null, taxQuote: null })).toEqual({
      taxLine: null,
      minimum: null,
      priceImpact: null,
    });
  });

  it("computes the minimum and the price impact for an untaxed quote", () => {
    const figures = useCurveTradeFigures({
      ...base,
      quote: { output: 9_000n, fee: 10n },
      taxQuote: { snipeTax: 0n, taxBps: 0 },
    });
    expect(figures.minimum).toBe(8_820n);
    expect(figures.priceImpact).not.toBeNull();
  });

  it("shows the tax line and no price impact for a taxed buy", () => {
    const figures = useCurveTradeFigures({
      ...base,
      quote: { output: 9_000n, fee: 10n },
      taxQuote: { snipeTax: 50n, taxBps: 1_000 },
    });
    expect(figures.taxLine).not.toBeNull();
    expect(figures.priceImpact).toBeNull();
  });

  it("ignores a malformed spot price and a missing slippage", () => {
    const figures = useCurveTradeFigures({
      ...base,
      slippageBps: null,
      spotPriceWei: "0x10",
      quote: { output: 9_000n, fee: 10n },
      taxQuote: { snipeTax: 0n, taxBps: 0 },
    });
    expect(figures.minimum).toBeNull();
  });
});

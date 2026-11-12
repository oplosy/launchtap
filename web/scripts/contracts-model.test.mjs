import { describe, expect, it } from "vitest";
import { deploymentEntry, filterAbi } from "./contracts-model.mjs";

const v2Manifest = {
  deploymentId: "local-v2",
  name: "Local v2",
  chainId: 31337,
  engineVersion: 2,
  factory: "0x0000000000000000000000000000000000000f01",
  poolManager: "0x0000000000000000000000000000000000000a01",
  permit2: "0x000000000022D473030F116dDEE9F6B43aC78BA3",
  hook: "0x0000000000000000000000000000000000002000",
  locker: "0x0000000000000000000000000000000000000c01",
  explorerBase: null,
};
const v4Record = {
  chainId: 31337,
  poolManager: "0x0000000000000000000000000000000000000A01",
  permit2: "0x000000000022D473030F116dDEE9F6B43aC78BA3",
  universalRouter: "0x0000000000000000000000000000000000000b01",
  v4Quoter: "0x0000000000000000000000000000000000000b02",
};

describe("reviewed deployment entries", () => {
  it("joins an engine-2 manifest with the reviewed V4 record", () => {
    const entry = deploymentEntry({ document: v2Manifest, enabled: true, v4Record });
    expect(entry.engineVersion).toBe(2);
    expect(entry.uniswapV2Router02).toBeNull();
    expect(entry.v4).toEqual({
      poolManager: v2Manifest.poolManager,
      permit2: v2Manifest.permit2,
      universalRouter: v4Record.universalRouter,
      v4Quoter: v4Record.v4Quoter,
      hook: v2Manifest.hook,
      locker: v2Manifest.locker,
    });
  });

  it("refuses an engine-2 manifest whose PoolManager differs from the V4 record", () => {
    expect(() =>
      deploymentEntry({
        document: v2Manifest,
        enabled: true,
        v4Record: { ...v4Record, poolManager: "0x0000000000000000000000000000000000000bad" },
      }),
    ).toThrow(/poolManager differs/);
  });

  it("refuses an engine-2 manifest without a V4 record or on another chain", () => {
    expect(() => deploymentEntry({ document: v2Manifest, enabled: true })).toThrow(
      /no reviewed V4/,
    );
    expect(() =>
      deploymentEntry({
        document: v2Manifest,
        enabled: true,
        v4Record: { ...v4Record, chainId: 1 },
      }),
    ).toThrow(/chain 1/);
  });

  it("keeps engine-1 manifests on the Uniswap V2 fields with no V4 section", () => {
    const entry = deploymentEntry({
      document: {
        deploymentId: "v1",
        name: "v1",
        chainId: 46630,
        factory: "0x0000000000000000000000000000000000000f01",
        weth: "0x0000000000000000000000000000000000000e01",
        uniswapV2Factory: null,
        uniswapV2Router02: "0x0000000000000000000000000000000000000e02",
      },
      enabled: true,
    });
    expect(entry.engineVersion).toBe(1);
    expect(entry.v4).toBeNull();
    expect(entry.uniswapV2Router02).toBe("0x0000000000000000000000000000000000000e02");
  });

  it("keeps every error of a v2 ABI but only the allowlisted functions", () => {
    const abi = [
      { type: "function", name: "buy" },
      { type: "function", name: "initialize" },
      { type: "error", name: "WrongPhase" },
    ];
    expect(
      filterAbi(abi, new Set(["buy"]), { allErrors: true }).map((entry) => entry.name),
    ).toEqual(["buy", "WrongPhase"]);
  });
});

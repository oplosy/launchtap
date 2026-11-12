import { describe, expect, it } from "vitest";
import { engineOf, reviewedV4Addresses } from "./engine";

const v4 = {
  poolManager: "0x0000000000000000000000000000000000000a01",
  permit2: "0x000000000022D473030F116dDEE9F6B43aC78BA3",
  universalRouter: "0x0000000000000000000000000000000000000b01",
  v4Quoter: "0x0000000000000000000000000000000000000b02",
  hook: "0x0000000000000000000000000000000000002000",
  locker: "0x0000000000000000000000000000000000000c01",
};
const deployment = { enabled: true, chainId: 31337, engineVersion: 2 as const, v4 };

describe("engine selection", () => {
  it("accepts engines 1 and 2 only", () => {
    expect(engineOf({ engine_version: 1 })).toBe(1);
    expect(engineOf({ engine_version: 2 })).toBe(2);
    expect(() => engineOf({ engine_version: 3 })).toThrow(/unsupported engine 3/);
    expect(() => engineOf({ engine_version: 0 })).toThrow();
  });
});

describe("reviewed V4 addresses", () => {
  it("returns every address of an enabled engine-2 deployment on the expected chain", () => {
    expect(reviewedV4Addresses(deployment, 31337)).toEqual(v4);
  });

  it("fails closed on a disabled, engine-1 or wrong-chain deployment", () => {
    expect(reviewedV4Addresses({ ...deployment, enabled: false }, 31337)).toBeNull();
    expect(reviewedV4Addresses({ ...deployment, engineVersion: 1, v4: null }, 31337)).toBeNull();
    expect(reviewedV4Addresses(deployment, 46630)).toBeNull();
    expect(reviewedV4Addresses(deployment, null)).toBeNull();
  });

  it("fails closed when any single address is missing or malformed", () => {
    for (const key of Object.keys(v4) as (keyof typeof v4)[]) {
      expect(reviewedV4Addresses({ ...deployment, v4: { ...v4, [key]: null } }, 31337)).toBeNull();
      expect(
        reviewedV4Addresses({ ...deployment, v4: { ...v4, [key]: "0x1234" } }, 31337),
      ).toBeNull();
    }
  });
});

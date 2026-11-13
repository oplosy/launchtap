import { describe, expect, it } from "vitest";
import { launchFacts } from "./launch-facts";

// v1 launch defaults: 1B supply, 800M on the curve, 200M seeded into the pair with 4.2 ETH.
const v1Curve = {
  engine_version: 1,
  phase: "curve",
  total_supply: "1000000000000000000000000000",
  curve_tokens: "800000000000000000000000000",
  lp_tokens: "200000000000000000000000000",
  graduation_eth: "4200000000000000000",
  real_curve_eth: "1000000000000000000",
  trade_fee_bps: 100,
  protocol_share_bps: 5000,
};

describe("launch facts", () => {
  it("states the engine-1 fee split from the on-chain parameters", () => {
    const facts = launchFacts(v1Curve)!;
    expect(facts.tradeFee).toBe("1%");
    // splitFees: protocol = fee × protocolShareBps / 10000, the creator gets the rest.
    expect(facts.feeSplit).toBe("50% to the protocol, 50% to the creator");
  });

  it("previews the pair the curve seeds at graduation", () => {
    const facts = launchFacts(v1Curve)!;
    // _graduate seeds graduationEth WETH and lpTokens: 4.2e18 × 1e18 / 2e26 wei per token.
    expect(facts.graduation.openingPriceWad).toBe("21000000000");
    // That price over the whole fixed supply: 4.2 ETH × 1e27 / 2e26 = 21 ETH.
    expect(facts.graduation.marketCapWei).toBe("21000000000000000000");
    expect(facts.graduation.remainingEthWei).toBe("3200000000000000000");
  });

  it("has no remaining ETH once graduated, and never a negative one", () => {
    expect(launchFacts({ ...v1Curve, phase: "graduated" })!.graduation.remainingEthWei).toBeNull();
    expect(
      launchFacts({ ...v1Curve, real_curve_eth: "5000000000000000000" })!.graduation
        .remainingEthWei,
    ).toBe("0");
  });

  it("returns no facts for engine 2 or for amounts that are not canonical integers", () => {
    expect(launchFacts({ ...v1Curve, engine_version: 2 })).toBeNull();
    expect(launchFacts({ ...v1Curve, lp_tokens: "0" })).toBeNull();
    expect(launchFacts({ ...v1Curve, graduation_eth: "4.2" })).toBeNull();
    expect(launchFacts({ ...v1Curve, protocol_share_bps: 10001 })).toBeNull();
  });
});

import { describe, expect, it } from "vitest";
import { launchFacts, type LaunchFactsInput } from "./launch-facts";

const WAD = 10n ** 18n;

const v1: LaunchFactsInput = {
  engine_version: 1,
  total_supply: (1_000_000_000n * WAD).toString(),
  curve_tokens: (800_000_000n * WAD).toString(),
  lp_tokens: (200_000_000n * WAD).toString(),
  trade_fee_bps: 100,
  protocol_share_bps: 5000,
  graduation_eth: ((42n * WAD) / 10n).toString(),
  phase: "curve",
};

describe("launchFacts", () => {
  it("derives the v1 supply split and fee shares", () => {
    const facts = launchFacts(v1);
    expect(facts.engineVersion).toBe("v1");
    expect(facts.totalSupply).toBe("1,000,000,000");
    expect(facts.curveShare).toEqual({ amount: "800,000,000", percent: "80.00%" });
    expect(facts.lpShare).toEqual({ amount: "200,000,000", percent: "20.00%" });
    expect(facts.tradeFee).toBe("1.00%");
    expect(facts.protocolFee).toBe("0.50%");
    expect(facts.creatorFee).toBe("0.50%");
    expect(facts.graduationEth).toBe("4.2");
  });

  it("splits an uneven protocol share without losing the remainder", () => {
    const facts = launchFacts({ ...v1, trade_fee_bps: 100, protocol_share_bps: 3333 });
    expect(facts.protocolFee).toBe("0.3333%");
    expect(facts.creatorFee).toBe("0.6667%");
  });

  it("marks malformed or out-of-range values unavailable instead of guessing", () => {
    const facts = launchFacts({
      ...v1,
      engine_version: 0,
      total_supply: "-1",
      curve_tokens: "1.5",
      lp_tokens: "",
      trade_fee_bps: 10_001,
      protocol_share_bps: -1,
      graduation_eth: "abc",
    });
    expect(facts.engineVersion).toBeNull();
    expect(facts.totalSupply).toBeNull();
    expect(facts.curveShare).toEqual({ amount: null, percent: null });
    expect(facts.lpShare).toEqual({ amount: null, percent: null });
    expect(facts.tradeFee).toBeNull();
    expect(facts.protocolFee).toBeNull();
    expect(facts.creatorFee).toBeNull();
    expect(facts.graduationEth).toBeNull();
  });

  it("does not compute a share against a zero total supply", () => {
    expect(launchFacts({ ...v1, total_supply: "0" }).curveShare.percent).toBeNull();
  });

  it("describes curve-phase restrictions and the curve pause", () => {
    const facts = launchFacts(v1);
    expect(facts.graduated).toBe(false);
    expect(facts.transferRule).toMatch(/curve or the pair/);
    expect(facts.pauseRule).toMatch(/can pause curve buys and sells/);
  });

  it("describes the graduated state as unrestricted and unpausable", () => {
    const facts = launchFacts({ ...v1, phase: "graduated" });
    expect(facts.graduated).toBe(true);
    expect(facts.transferRule).toMatch(/no transfer restrictions/i);
    expect(facts.pauseRule).toMatch(/cannot pause/);
  });
});

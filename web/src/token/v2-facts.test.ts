import { describe, expect, it } from "vitest";
import { v2Facts } from "./v2-facts";

const recipient = "0x00000000000000000000000000000000000000a1";
const v2Curve = {
  anti_snipe: { start_bps: 2000, duration_seconds: 600, ends_at: "2026-11-12T10:10:00Z" },
  fee_recipient: recipient,
  pending_fee_recipient: null,
  lp_creator_share_bps: 5000,
  locker_owed: null,
  pool: null,
};
const pool = {
  pool_id: `0x${"ab".repeat(32)}`,
  hook: "0x0000000000000000000000000000000000002000",
  position_token_id: "7",
};

describe("v2 token facts", () => {
  it("shows no v2 block for an engine-1 token", () => {
    expect(v2Facts({ engine_version: 1, phase: "curve", v2: null })).toBeNull();
    expect(v2Facts({ engine_version: 1, phase: "graduated", v2: null })).toBeNull();
  });

  it("shows the launch profile but no pool row before graduation", () => {
    const facts = v2Facts({ engine_version: 2, phase: "curve", v2: v2Curve })!;
    expect(facts.antiSnipe).toBe("20%, decaying to 0 over 10 min");
    expect(facts.antiSnipeEndsAt).toBe("2026-11-12T10:10:00Z");
    expect(facts.lpCreatorShare).toBe("50%");
    expect(facts.feeRecipient).toBe(recipient);
    expect(facts.pendingFeeRecipient).toBeNull();
    expect(facts.pool).toBeNull();
    expect(facts.liquidityIndexed).toBe(true);
  });

  it("shows the pool facts and a liquidity placeholder after graduation", () => {
    const pending = "0x00000000000000000000000000000000000000b1";
    const facts = v2Facts({
      engine_version: 2,
      phase: "Graduated",
      v2: {
        ...v2Curve,
        pending_fee_recipient: pending,
        pool,
        locker_owed: [{ account: recipient, eth: "1000", token: "5" }],
      },
    })!;
    expect(facts.pool).toEqual({ poolId: pool.pool_id, hook: pool.hook });
    expect(facts.pendingFeeRecipient).toBe(pending);
    expect(facts.lockerOwed).toEqual([{ account: recipient, eth: "1000", token: "5" }]);
    // V4 liquidity is not projected yet (Plan v2-B known limitation): never show a fake 0.
    expect(facts.liquidityIndexed).toBe(false);
  });

  it("treats an engine-2 token without its v2 payload as unavailable", () => {
    expect(v2Facts({ engine_version: 2, phase: "curve", v2: null })).toBeNull();
  });
});

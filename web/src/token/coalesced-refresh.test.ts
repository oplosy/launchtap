import { describe, expect, it } from "vitest";
import { createCoalescedRefresh } from "./coalesced-refresh";

const tick = () => new Promise((resolve) => setTimeout(resolve, 0));

describe("createCoalescedRefresh", () => {
  it("collapses triggers during a run into one follow-up run", async () => {
    let runs = 0;
    let release: () => void = () => undefined;
    const trigger = createCoalescedRefresh(async () => {
      runs += 1;
      if (runs === 1) await new Promise<void>((resolve) => (release = resolve));
    });
    trigger();
    trigger();
    trigger();
    expect(runs).toBe(1);
    release();
    await tick();
    expect(runs).toBe(2);
  });

  it("swallows an abort rejection (a reconnect reload cancelled the refresh) and stays usable", async () => {
    let runs = 0;
    const trigger = createCoalescedRefresh(async () => {
      runs += 1;
      if (runs === 1) throw new DOMException("The operation was aborted.", "AbortError");
    });
    // An unhandled rejection here fails the vitest run.
    trigger();
    await tick();
    trigger();
    await tick();
    expect(runs).toBe(2);
  });

  it("swallows other failures too: the loaders already surface their own error state", async () => {
    let runs = 0;
    const trigger = createCoalescedRefresh(async () => {
      runs += 1;
      throw new Error("token 500");
    });
    trigger();
    await tick();
    trigger();
    await tick();
    expect(runs).toBe(2);
  });
});

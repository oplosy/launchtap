import { describe, expect, it } from "vitest";
import { chainClockOffset, formatCountdown, remainingTaxSeconds, snipeTaxLine } from "./anti-snipe";

const endsAt = "2026-11-12T10:01:00Z";
const endsAtSeconds = Date.parse(endsAt) / 1000;

describe("anti-snipe countdown", () => {
  it("counts down from the window start to zero and stays at zero after it", () => {
    expect(remainingTaxSeconds(endsAt, endsAtSeconds - 60)).toBe(60);
    expect(remainingTaxSeconds(endsAt, endsAtSeconds - 30)).toBe(30);
    expect(remainingTaxSeconds(endsAt, endsAtSeconds)).toBe(0);
    expect(remainingTaxSeconds(endsAt, endsAtSeconds + 90)).toBe(0);
  });

  it("has no countdown without an end time or with a malformed one", () => {
    expect(remainingTaxSeconds("", endsAtSeconds - 10)).toBe(0);
    expect(remainingTaxSeconds(null, endsAtSeconds - 10)).toBe(0);
    expect(remainingTaxSeconds("not a time", endsAtSeconds - 10)).toBe(0);
  });

  it("follows chain time whatever the client clock says", () => {
    const chainNow = endsAtSeconds - 42;
    for (const skewSeconds of [-300, 0, 300]) {
      const clientNowMs = (chainNow + skewSeconds) * 1000;
      const offset = chainClockOffset(BigInt(chainNow), clientNowMs);
      // A tick five seconds later on the same skewed client clock.
      const chainTime = (clientNowMs + 5_000) / 1000 + offset;
      expect(remainingTaxSeconds(endsAt, chainTime)).toBe(37);
    }
  });

  it("formats minutes and seconds", () => {
    expect(formatCountdown(0)).toBe("0:00");
    expect(formatCountdown(7)).toBe("0:07");
    expect(formatCountdown(60)).toBe("1:00");
    expect(formatCountdown(125)).toBe("2:05");
  });
});

describe("anti-snipe cost line", () => {
  it("shows the tax with its rate and time left only while a tax is taken", () => {
    expect(snipeTaxLine({ snipeTax: 0n, taxBps: 2500, remainingSeconds: 30 })).toBeNull();
    expect(snipeTaxLine({ snipeTax: 10n, taxBps: 2500, remainingSeconds: 30 })).toEqual({
      amount: 10n,
      rate: "25%",
      timeLeft: "0:30",
    });
    expect(snipeTaxLine({ snipeTax: 10n, taxBps: 1234, remainingSeconds: 0 })).toEqual({
      amount: 10n,
      rate: "12.34%",
      timeLeft: null,
    });
  });
});

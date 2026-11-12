import { describe, expect, it } from "vitest";
import { formatBps, formatDuration, launchRulesV2 } from "./launch-v2";

describe("v2 launch facts from futureDefaults", () => {
  it("formats basis points without float rounding", () => {
    expect(formatBps(2000)).toBe("20%");
    expect(formatBps(5000)).toBe("50%");
    expect(formatBps(150)).toBe("1.5%");
    expect(formatBps(1)).toBe("0.01%");
    expect(formatBps(0)).toBe("0%");
  });

  it("formats the anti-snipe window in whole units", () => {
    expect(formatDuration(45)).toBe("45 s");
    expect(formatDuration(600)).toBe("10 min");
    expect(formatDuration(90)).toBe("1 min 30 s");
    expect(formatDuration(5400)).toBe("1 h 30 min");
    expect(formatDuration(0)).toBe("none");
  });

  it("describes the snapshot the next launch takes", () => {
    expect(
      launchRulesV2({
        antiSnipe: { startBps: 2000, durationSeconds: 600 },
        lpCreatorShareBps: 5000,
      }),
    ).toEqual({
      taxStart: "20%",
      taxWindow: "10 min",
      lpCreatorShare: "50%",
      lpProtocolShare: "50%",
    });
  });

  it("reports no tax when the profile is disabled", () => {
    expect(
      launchRulesV2({ antiSnipe: { startBps: 0, durationSeconds: 0 }, lpCreatorShareBps: 4000 }),
    ).toMatchObject({ taxStart: "0%", taxWindow: "none", lpProtocolShare: "60%" });
  });
});

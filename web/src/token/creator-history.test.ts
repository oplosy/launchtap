import { describe, expect, it } from "vitest";
import { creatorHistoryLine } from "./creator-history";

const current = "0x00000000000000000000000000000000000000AA";
const launch = (token: string, phase: string) => ({ token, phase });

describe("creator history line", () => {
  it("counts the creator's other launches and how many graduated", () => {
    expect(
      creatorHistoryLine(
        [
          launch("0x00000000000000000000000000000000000000aa", "curve"),
          launch("0x00000000000000000000000000000000000000bb", "graduated"),
          launch("0x00000000000000000000000000000000000000cc", "curve"),
          launch("0x00000000000000000000000000000000000000dd", "Graduated"),
        ],
        current,
        false,
      ),
    ).toBe("Launched 3 other tokens on Launchpad; 2 graduated.");
  });

  it("uses the singular for one other launch", () => {
    expect(
      creatorHistoryLine(
        [launch("0x00000000000000000000000000000000000000bb", "curve")],
        current,
        false,
      ),
    ).toBe("Launched 1 other token on Launchpad; 0 graduated.");
  });

  it("says when this is the creator's first launch", () => {
    expect(creatorHistoryLine([launch(current.toLowerCase(), "curve")], current, false)).toBe(
      "This is the creator's first launch on Launchpad.",
    );
  });

  it("states lower bounds when the list was cut off at one page", () => {
    expect(
      creatorHistoryLine(
        [
          launch("0x00000000000000000000000000000000000000bb", "graduated"),
          launch("0x00000000000000000000000000000000000000cc", "curve"),
        ],
        current,
        true,
      ),
    ).toBe("Launched at least 2 other tokens on Launchpad; at least 1 graduated.");
  });
});

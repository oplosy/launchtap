import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { LeaderboardView, LeaderboardWindowTabs } from "./leaderboard-view";

const NAMED = "0x00000000000000000000000000000000000000ab";
const PLAIN = "0x00000000000000000000000000000000000000ac";

describe("LeaderboardView", () => {
  it("labels a published username and keeps the address as the title", () => {
    const html = renderToStaticMarkup(
      createElement(LeaderboardView, {
        items: [
          { rank: 1, address: NAMED, realized_pnl_wei: "5", tokens_traded: 2, username: "alice" },
          { rank: 2, address: PLAIN, realized_pnl_wei: "4", tokens_traded: 1, username: null },
        ],
      }),
    );
    expect(html).toContain(">@alice</a>");
    expect(html.match(/>@/g)?.length).toBe(1);
    expect(html).toContain(`href="/wallet/${NAMED}"`);
  });

  it("explains an empty window", () => {
    const html = renderToStaticMarkup(createElement(LeaderboardView, { items: [], period: "7d" }));
    expect(html).toContain("in the last 7 days");
  });
});

describe("LeaderboardWindowTabs", () => {
  it("links every period and marks the active one", () => {
    const html = renderToStaticMarkup(createElement(LeaderboardWindowTabs, { active: "30d" }));
    expect(html).toContain('href="/leaderboard"');
    expect(html).toContain('href="/leaderboard?window=7d"');
    expect(html).toMatch(
      /href="\/leaderboard\?window=30d" aria-current="page"|aria-current="page"[^>]*href="\/leaderboard\?window=30d"/,
    );
    expect(html.match(/aria-current/g)?.length).toBe(1);
  });
});

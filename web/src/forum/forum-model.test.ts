import { describe, expect, it } from "vitest";
import type { ForumPost } from "@/api/client";
import {
  appendForumPage,
  communityLabel,
  forumTabHref,
  parseForumTab,
  tokenHref,
} from "./forum-model";

const post = (id: string): ForumPost => ({
  id,
  token: "0x0000000000000000000000000000000000000010",
  body: "gm",
  score: 0,
  created_at: "2026-11-10T09:00:00.000000Z",
  author: { username: null, wallet: null, role: "holder" },
  name: "Pons",
  symbol: "PONS",
});

describe("forum model", () => {
  it("parses the tab and builds its link", () => {
    expect(parseForumTab(undefined)).toBe("new");
    expect(parseForumTab("hot")).toBe("hot");
    expect(parseForumTab("top")).toBe("top");
    expect(parseForumTab("best")).toBe("best");
    expect(forumTabHref("best")).toBe("/forum?tab=best");
    expect(parseForumTab("HOT")).toBe("new");
    expect(parseForumTab(["hot", "top"])).toBe("new");
    expect(forumTabHref("new")).toBe("/forum");
    expect(forumTabHref("top")).toBe("/forum?tab=top");
  });

  it("labels communities and links tokens", () => {
    expect(communityLabel("PONS")).toBe("s/PONS");
    expect(communityLabel("  ")).toBe("s/TOKEN");
    expect(communityLabel("<b>x</b>")).toBe("s/<b>x</b>");
    expect(tokenHref("0x0000000000000000000000000000000000000010")).toMatch(/^\/token\/0x/);
    expect(tokenHref("javascript:alert(1)")).toBeNull();
  });

  it("appends pages without duplicates", () => {
    expect(
      appendForumPage([post("9"), post("8")], [post("8"), post("7")]).map((p) => p.id),
    ).toEqual(["9", "8", "7"]);
  });
});

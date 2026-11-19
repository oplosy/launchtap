import { describe, expect, it } from "vitest";
import type { ForumPost } from "@/api/client";
import {
  appendForumPage,
  communityLabel,
  forumTabHref,
  isArtBlock,
  parseForumTab,
  postScale,
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

describe("postScale", () => {
  it("sizes a post by its length in characters", () => {
    expect(postScale("gm")).toBe("poster");
    expect(postScale("x".repeat(48))).toBe("poster");
    expect(postScale("x".repeat(49))).toBe("statement");
    expect(postScale("x".repeat(140))).toBe("statement");
    expect(postScale("x".repeat(141))).toBe("reading");
  });

  it("counts code points and ignores surrounding whitespace", () => {
    expect(postScale(`  ${"🚀".repeat(48)}  `)).toBe("poster");
  });
});

describe("isArtBlock", () => {
  it("marks every seventh block from the fourth", () => {
    expect([0, 1, 2, 3, 4, 9, 10, 17].map((index) => isArtBlock(index, "gm"))).toEqual([
      false,
      false,
      false,
      true,
      false,
      false,
      true,
      true,
    ]);
  });

  it("never puts a long post on an image", () => {
    expect(isArtBlock(3, "x".repeat(141))).toBe(false);
  });
});

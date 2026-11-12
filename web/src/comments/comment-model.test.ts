import { describe, expect, it } from "vitest";
import {
  appendPage,
  authorLabel,
  mergeFirstPage,
  nextVote,
  problemMessage,
  scoreLabel,
  voteProblemMessage,
  type CommentItem,
} from "./comment-model";

const WALLET = "0x00000000000000000000000000000000000000A1";

function comment(id: number): CommentItem {
  return {
    id: String(id),
    token: "0x0000000000000000000000000000000000000002",
    body: `c${id}`,
    created_at: "2026-11-09T00:00:00.000000Z",
    author: { username: null, wallet: WALLET, role: "holder" },
    score: 0,
  };
}
const ids = (items: CommentItem[]) => items.map((c) => c.id);

describe("mergeFirstPage", () => {
  it("replaces the first page, keeps older loaded items and dedupes", () => {
    const current = [comment(10), comment(9), comment(8), comment(5), comment(4)];
    const fresh = [comment(12), comment(11), comment(10), comment(8)];
    // 9 was hidden: it is newer than the oldest fresh item (8) and absent from the fresh page.
    expect(ids(mergeFirstPage(current, fresh))).toEqual(["12", "11", "10", "8", "5", "4"]);
  });

  it("compares ids as integers, not strings", () => {
    expect(ids(mergeFirstPage([comment(9)], [comment(10)]))).toEqual(["10", "9"]);
  });

  it("an empty fresh page clears the list", () => {
    expect(mergeFirstPage([comment(1)], [])).toEqual([]);
  });

  it("appendPage adds older items once", () => {
    expect(ids(appendPage([comment(5), comment(4)], [comment(4), comment(3)]))).toEqual([
      "5",
      "4",
      "3",
    ]);
  });
});

describe("authorLabel", () => {
  it("username only", () => {
    expect(authorLabel({ username: "alice", wallet: null, role: "holder" })).toEqual({
      primary: "@alice",
      walletHref: null,
    });
  });
  it("wallet only", () => {
    const label = authorLabel({ username: null, wallet: WALLET, role: "creator" });
    expect(label.primary).not.toContain("@");
    expect(label.walletHref).toContain(WALLET.toLowerCase().slice(2, 6));
  });
  it("username and wallet", () => {
    const label = authorLabel({ username: "alice", wallet: WALLET, role: "holder" });
    expect(label.primary).toBe("@alice");
    expect(label.walletHref).not.toBeNull();
  });
});

describe("problemMessage", () => {
  it("has a sentence for every spec error code and a fallback", () => {
    for (const code of [
      "authentication_failed",
      "wallet_not_linked",
      "not_holder",
      "banned",
      "token_not_found",
      "comment_not_found",
      "invalid_comment",
      "invalid_cursor",
      "invalid_limit",
      "invalid_report",
      "own_comment",
      "rate_limited",
    ]) {
      expect(problemMessage(code)).not.toBe(problemMessage(undefined));
    }
    expect(problemMessage("nope")).toBe(problemMessage(undefined));
  });
});

describe("votes", () => {
  it("clicking the active arrow clears the vote; the other arrow switches it", () => {
    expect(nextVote(0, 1)).toBe(1);
    expect(nextVote(1, 1)).toBe(0);
    expect(nextVote(1, -1)).toBe(-1);
    expect(nextVote(-1, -1)).toBe(0);
  });

  it("labels the score in points", () => {
    expect(scoreLabel(1)).toBe("1 point");
    expect(scoreLabel(0)).toBe("0 points");
    expect(scoreLabel(-2)).toBe("-2 points");
  });

  it("explains vote refusals in vote terms", () => {
    expect(voteProblemMessage("not_holder")).toBe("Hold this token to vote on its comments.");
    expect(voteProblemMessage("own_comment")).toBe("You cannot vote on your own comment.");
    expect(voteProblemMessage("rate_limited")).toBe("You are voting too fast. Try again shortly.");
    expect(voteProblemMessage("banned")).toBe(problemMessage("banned"));
  });
});

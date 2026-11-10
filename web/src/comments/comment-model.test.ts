import { describe, expect, it } from "vitest";
import {
  appendPage,
  authorLabel,
  mergeFirstPage,
  problemMessage,
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

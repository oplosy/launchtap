import { readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";
import type { InboxItem } from "@/api/client";
import { LARGE_TRADE_WEI } from "@/live/notification-rules";
import { appendInboxPage, describeInboxItem, formatUnreadCount, isUnread } from "./inbox-model";

const item = (overrides: Partial<InboxItem>): InboxItem => ({
  id: "15-0-1",
  kind: "large_trade",
  token: "0x0000000000000000000000000000000000000010",
  name: "Pons",
  symbol: "PONS",
  block_time: "2026-11-10T10:00:00Z",
  tx_hash: "0x" + "ab".repeat(32),
  eth_wei: "700000000000000000",
  side_buy: true,
  trader: null,
  source: "curve",
  ...overrides,
});

describe("inbox model", () => {
  it("pins the large-trade threshold to the server view (0.5 ETH)", () => {
    expect(LARGE_TRADE_WEI).toBe(500_000_000_000_000_000n);
    const migration = readFileSync(
      path.resolve(
        process.cwd(),
        "../backend/internal/store/postgres/migrations/00022_notification_inbox.sql",
      ),
      "utf8",
    );
    expect(migration).toContain(`gross_eth_volume >= ${LARGE_TRADE_WEI.toString()}`);
  });

  it("caps the unread label at 99+", () => {
    expect(formatUnreadCount(0)).toBe("0");
    expect(formatUnreadCount(7)).toBe("7");
    expect(formatUnreadCount(99)).toBe("99");
    expect(formatUnreadCount(100)).toBe("99+");
    expect(formatUnreadCount(Number.NaN)).toBe("0");
  });

  it("describes trades and graduations", () => {
    expect(describeInboxItem(item({}))).toBe("Large buy on PONS: 0.7 ETH");
    expect(describeInboxItem(item({ side_buy: false, source: "dex" }))).toBe(
      "Large sell on PONS on the pool: 0.7 ETH",
    );
    expect(
      describeInboxItem(
        item({ kind: "graduation", side_buy: null, source: null, eth_wei: "4000000000000000000" }),
      ),
    ).toBe("PONS graduated with 4 ETH to the pool");
    expect(describeInboxItem(item({ symbol: "  ", eth_wei: "bad" }))).toBe("Large buy on Token");
  });

  it("appends pages without duplicates and marks the newest unread", () => {
    const merged = appendInboxPage(
      [item({ id: "15-0-1" }), item({ id: "14-0-1" })],
      [item({ id: "14-0-1" }), item({ id: "13-0-0" })],
    );
    expect(merged.map((entry) => entry.id)).toEqual(["15-0-1", "14-0-1", "13-0-0"]);
    expect([0, 1, 2].map((index) => isUnread(index, 2))).toEqual([true, true, false]);
  });
});

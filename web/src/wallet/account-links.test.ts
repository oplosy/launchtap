import { describe, expect, it } from "vitest";
import { accountLinks } from "./account-links";

describe("accountLinks", () => {
  it("lists the account pages, the wallet page under the lower-case address", () => {
    expect(accountLinks("0xAbCdEf0000000000000000000000000000000001")).toEqual([
      { href: "/wallet/0xabcdef0000000000000000000000000000000001", label: "My wallet" },
      { href: "/profile", label: "Profile" },
      { href: "/rewards", label: "Rewards" },
      { href: "/explore?view=watchlist", label: "Watchlist" },
      { href: "/notifications", label: "Notifications" },
    ]);
  });
});

import { describe, expect, it } from "vitest";
import { canonicalTokenUrl, shareOnXUrl } from "./share-links";

describe("share links", () => {
  it("builds a lower-case canonical token url", () => {
    expect(
      canonicalTokenUrl("https://launch.example/", "0xABCDEFabcdefABCDEFabcdefABCDEFabcdefABCD"),
    ).toBe("https://launch.example/token/0xabcdefabcdefabcdefabcdefabcdefabcdefabcd");
  });

  it("encodes the post text and url with URLSearchParams", () => {
    const link = new URL(shareOnXUrl("A&B", "https://launch.example/token/0x1?x=1&y=2"));
    expect(link.origin + link.pathname).toBe("https://x.com/intent/post");
    expect(link.searchParams.get("text")).toBe("$A&B on Launchpad");
    expect(link.searchParams.get("url")).toBe("https://launch.example/token/0x1?x=1&y=2");
    expect(link.searchParams.get("y")).toBeNull();
  });
});

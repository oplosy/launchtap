import { createElement } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ImageResponse } from "next/og";
import { CARD_TEXT_DISALLOWED, cardText } from "./card-data";

/** Every character cardText can emit, so the probe proves the bundled font covers all of them. */
function allowedCharacters() {
  const chars: string[] = [];
  for (let code = 0x20; code <= 0x2026; code++) {
    const char = String.fromCodePoint(code);
    if (char.replace(CARD_TEXT_DISALLOWED, "") === char) chars.push(char);
  }
  return chars.join("");
}

describe("card text and the bundled font", () => {
  afterEach(() => vi.restoreAllMocks());

  it("renders every allowed character with all outbound network blocked", async () => {
    // The renderer loads its own wasm through a data: URL; anything else would leave the machine.
    const realFetch = globalThis.fetch;
    const blocked = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      if (String(input).startsWith("data:")) return realFetch(input, init);
      throw new Error(`outbound request blocked: ${String(input)}`);
    });
    vi.stubGlobal("fetch", blocked);
    const text = allowedCharacters();
    expect(text.length).toBeGreaterThan(180);
    const element = createElement(
      "div",
      { style: { display: "flex", width: "100%", height: "100%", fontSize: 20 } },
      text,
    );
    const response = new ImageResponse(element, { width: 1200, height: 630 });
    const bytes = new Uint8Array(await response.arrayBuffer());
    expect(Array.from(bytes.subarray(0, 4))).toEqual([0x89, 0x50, 0x4e, 0x47]);
    expect(
      blocked.mock.calls.map((call) => String(call[0])).filter((url) => !url.startsWith("data:")),
    ).toEqual([]);
    vi.unstubAllGlobals();
  });

  it("control: an emoji does trigger a third-party fetch, so the probe is meaningful", async () => {
    const realFetch = globalThis.fetch;
    const external = vi.fn();
    vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
      if (String(input).startsWith("data:")) return realFetch(input, init);
      external(String(input));
      throw new Error("outbound request blocked");
    });
    const element = createElement("div", { style: { display: "flex" } }, "\u{1F680}");
    const response = new ImageResponse(element, { width: 100, height: 100 });
    await expect(response.arrayBuffer()).rejects.toThrow();
    expect(external).toHaveBeenCalled();
    vi.unstubAllGlobals();
  });

  it("filters everything outside the allowed set before rendering", () => {
    expect(cardText("a\u{1F680}日bЖ", 32)).toBe("ab");
  });
});

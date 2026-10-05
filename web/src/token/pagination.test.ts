import { describe, expect, it, vi } from "vitest";
import { ApiProblem } from "@/api/problems";
import { appendTokenCollectionPage } from "./pagination";

const snapshot = { chain_id: 1, as_of_block: 10, as_of_block_hash: "0x1", finality: "safe" };
const page = (cursor?: string, snap = snapshot) => ({
  items: [],
  next_cursor: cursor,
  snapshot: snap,
});

describe("token collection pagination", () => {
  it("recovers invalid cursors from page one", async () => {
    const fetch = vi
      .fn()
      .mockRejectedValueOnce(new ApiProblem(409, "cursor_invalidated"))
      .mockResolvedValueOnce(page("next"));
    const result = await appendTokenCollectionPage(fetch, [page("cursor")], "cursor");
    expect(result.reset).toBe(true);
    expect(fetch).toHaveBeenLastCalledWith(undefined, undefined);
  });
  it("keeps pages when the snapshot advances and drops moved duplicates", async () => {
    const holder = (address: string) => ({ address, balance: "1", first_acquired_block: 1 });
    const first = { ...page("cursor"), items: [holder("0xAa"), holder("0xbb")] };
    const second = {
      ...page(undefined, { ...snapshot, as_of_block: 11 }),
      items: [holder("0xaa"), holder("0xcc")],
    };
    const fetch = vi.fn().mockResolvedValue(second);
    const result = await appendTokenCollectionPage(fetch, [first], "cursor");
    expect(result.reset).toBe(false);
    expect(result.pages).toHaveLength(2);
    expect(result.pages[1]?.items).toEqual([holder("0xcc")]);
    expect(fetch).toHaveBeenCalledTimes(1);
  });
  it("retains page one while appending a page from the same snapshot", async () => {
    const first = page("cursor");
    const second = page();
    const fetch = vi.fn().mockResolvedValue(second);
    const result = await appendTokenCollectionPage(fetch, [first], "cursor");
    expect(result.pages).toEqual([first, { ...second, items: [] }]);
    expect(fetch).toHaveBeenCalledTimes(1);
  });
});

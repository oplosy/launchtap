import { expect, test, type Page } from "@playwright/test";
import path from "node:path";

const snapshot = { chain_id: 4663, as_of_block: 321, as_of_block_hash: "0xabc", finality: "safe" };
const eth = (whole: string) => `${BigInt(whole) * 10n ** 18n}`;
const source = (engine1: string, engine2: string) => ({
  engine_1: engine1,
  engine_2: engine2,
  total: `${BigInt(engine1) + BigInt(engine2)}`,
});

async function installFixture(page: Page, revenueStatus = 200) {
  await page.route("**/v1/**", async (route) => {
    const url = new URL(route.request().url());
    const json = (body: unknown, status = 200) =>
      route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });
    if (url.pathname.endsWith("/stats/protocol/revenue")) {
      if (revenueStatus !== 200)
        return json({ status: revenueStatus, title: "Unavailable" }, revenueStatus);
      return json({
        snapshot,
        total: eth("6"),
        sources: {
          launch_fees: source(eth("1"), eth("1")),
          trade_fees: source(eth("1"), eth("2")),
          snipe_tax: source("0", "0"),
          graduation_remainder: source("0", "0"),
          lp_fees: source("0", eth("1")),
        },
      });
    }
    if (url.pathname.endsWith("/stats/protocol/daily")) return json({ snapshot, items: [] });
    if (url.pathname.endsWith("/stats/protocol"))
      return json({
        snapshot,
        volume_24h_eth: eth("2"),
        volume_all_time_eth: eth("9"),
        launches_24h: 1,
        launches_all_time: 3,
        trades_24h: 4,
        trades_all_time: 12,
        graduations_24h: 0,
        graduations_all_time: 1,
        updated_at: "2026-11-12T12:00:00Z",
      });
    return json({ status: 404, title: "Not found" }, 404);
  });
}

test("analytics shows protocol revenue by source and engine", async ({ page }) => {
  await installFixture(page);
  await page.goto("/analytics?fixture=populated");
  const card = page.locator(".analytics-revenue");
  await expect(card.getByRole("heading", { name: "6 ETH earned" })).toBeVisible();
  await expect(card).toContainText("All-time · block 321");
  const rows = card.getByRole("row");
  await expect(rows).toHaveCount(4);
  await expect(rows.nth(1)).toHaveText(/Launch fees\s*1 ETH\s*1 ETH\s*2 ETH/);
  await expect(rows.nth(2)).toHaveText(/Trade fees\s*1 ETH\s*2 ETH\s*3 ETH/);
  await expect(rows.nth(3)).toHaveText(/LP fees\s*0 ETH\s*1 ETH\s*1 ETH/);
  await expect(card).not.toContainText("Snipe tax");
  await card.scrollIntoViewIfNeeded();
  await card.screenshot({
    path: path.join(process.cwd(), "test-results", "screenshots", "protocol-revenue.png"),
  });
});

test("a failed revenue read leaves the rest of analytics standing", async ({ page }) => {
  await installFixture(page, 503);
  await page.goto("/analytics?fixture=populated");
  await expect(
    page.getByRole("heading", { name: "Protocol revenue is unavailable" }),
  ).toBeVisible();
  await expect(page.getByText("All-time volume")).toBeVisible();
});

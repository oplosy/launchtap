import { expect, test, type Page } from "@playwright/test";

// Legacy links move to /explore whatever the configuration; the list checks need the READY
// fixture configuration (TASK6_ANVIL_* variables) and mock every API call with page.route.
const ready = Boolean(process.env.TASK6_ANVIL_API_URL);

const snapshot = { chain_id: 31337, as_of_block: 10, as_of_block_hash: "0xabc", finality: "safe" };
const longName = "Long".repeat(30);

function token(n: number, over: Record<string, unknown> = {}) {
  return {
    address: `0x${n.toString(16).padStart(40, "0")}`,
    holder_count: n,
    launch_block: n,
    launch_time: new Date(Date.now() - n * 3_600_000).toISOString(),
    market_cap_eth: `${BigInt(n) * 10n ** 18n}`,
    name: `Token ${n}`,
    phase: "curve",
    symbol: `T${n}`,
    total_supply: `${10n ** 27n}`,
    volume_24h_eth: "0",
    ...over,
  };
}

async function installApi(page: Page, calls: string[]) {
  await page.route("**/v1/**", async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname.endsWith("/events")) {
      await route.fulfill({ status: 200, contentType: "text/event-stream", body: "" });
      return;
    }
    if (url.pathname.endsWith("/tokens/highlights")) {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          as_of: new Date().toISOString(),
          finality: "safe",
          king_of_the_hill: null,
          snapshot,
          trending: [{ ...token(9), window_volume_eth: "0", distinct_buyers: 1 }],
          window_start: new Date().toISOString(),
        }),
      });
      return;
    }
    if (url.pathname.endsWith("/tokens")) {
      calls.push(url.search);
      const graduated = url.searchParams.get("phase") === "graduated";
      const items = graduated
        ? [token(3, { phase: "graduated", name: longName })]
        : [token(1), token(2)];
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ snapshot, items }),
      });
      return;
    }
    await route.fulfill({ status: 404, contentType: "application/json", body: "{}" });
  });
}

test("/graduated permanently moves to the Graduated view", async ({ page }) => {
  const response = await page.request.get("/graduated", { maxRedirects: 0 });
  expect(response.status()).toBe(308);
  expect(response.headers().location).toBe("/explore?view=graduated");
  await page.goto("/graduated");
  await expect(page).toHaveURL(/\/explore\?view=graduated$/);
  await expect(page.getByRole("heading", { level: 1, name: "Explore" })).toBeVisible();
  await expect(page.getByRole("link", { name: "Graduated", exact: true })).toHaveAttribute(
    "aria-current",
    "page",
  );
});

test("old home-page views land on the matching /explore state", async ({ page }) => {
  await page.goto("/?view=watchlist");
  await expect(page).toHaveURL(/\/explore\?view=watchlist$/);
  await page.goto("/?view=marketcap");
  await expect(page).toHaveURL(/\/explore\?view=trending&sort=market_cap$/);
  await page.goto("/");
  await expect(page).toHaveURL(/127\.0\.0\.1:\d+\/$/);
});

test("view tabs live in the URL and Back restores the previous tab", async ({ page }) => {
  test.skip(!ready, "Needs the READY fixture configuration.");
  await installApi(page, []);
  await page.goto("/explore");
  const tabs = page.getByRole("navigation", { name: "Explore views" });
  await expect(tabs.getByRole("link", { name: "Trending" })).toHaveAttribute(
    "aria-current",
    "page",
  );
  await tabs.getByRole("link", { name: "New" }).click();
  await expect(page).toHaveURL(/\/explore\?view=new$/);
  await expect(page.getByRole("link", { name: "Token 1" })).toBeVisible();
  await page.goBack();
  await expect(page).toHaveURL(/\/explore$/);
  await expect(tabs.getByRole("link", { name: "Trending" })).toHaveAttribute(
    "aria-current",
    "page",
  );
  await expect(page.getByRole("link", { name: "Token 9" })).toBeVisible();
});

test("sort turns Trending into a ranked list and the grid keeps the same tokens", async ({
  page,
}) => {
  test.skip(!ready, "Needs the READY fixture configuration.");
  const calls: string[] = [];
  await installApi(page, calls);
  await page.goto("/explore");
  await page.getByLabel("Sort").selectOption("market_cap");
  await expect(page).toHaveURL(/\/explore\?sort=market_cap$/);
  await expect(page.getByRole("link", { name: "Token 2" })).toBeVisible();
  expect(calls.at(-1)).toContain("sort=market_cap");
  const names = async () =>
    page
      .locator(".token-list .token-identity h3")
      .evaluateAll((nodes) => nodes.map((node) => node.textContent));
  // The list may refetch once on mount (live recovery), so wait for settled rows each time.
  await expect.poll(names).toEqual(["Token 1", "Token 2"]);
  await expect(page.locator(".token-list")).toHaveAttribute("data-layout", "grid");
  await page.getByRole("group", { name: "Layout" }).getByRole("button", { name: "List" }).click();
  await expect(page.locator(".token-list")).toHaveAttribute("data-layout", "list");
  await expect.poll(names).toEqual(["Token 1", "Token 2"]);
});

test("a 120-character name truncates without horizontal overflow at 360 px", async ({ page }) => {
  test.skip(!ready, "Needs the READY fixture configuration.");
  await installApi(page, []);
  await page.setViewportSize({ width: 360, height: 800 });
  await page.goto("/explore?view=graduated");
  const name = page.getByRole("link", { name: longName });
  await expect(name).toBeVisible();
  const heading = page.locator(".token-identity h3").first();
  // Measured once layout has settled at the new viewport width.
  await expect
    .poll(() =>
      heading.evaluate((node) => ({
        truncated: node.scrollWidth > node.clientWidth,
        overflow: document.documentElement.scrollWidth - window.innerWidth,
      })),
    )
    .toEqual({ truncated: true, overflow: 0 });
});

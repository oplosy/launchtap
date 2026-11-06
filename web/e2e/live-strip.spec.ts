import { expect, test, type Page } from "@playwright/test";

// The strip renders only with a ready configuration, which the E2E fixture build provides when
// the TASK6_ANVIL_* variables are set (any valid addresses and a URL; the API is mocked here).
test.skip(
  !process.env.TASK6_ANVIL_API_URL,
  "Needs the fixture build (TASK6_ANVIL_* variables) so the configuration is ready.",
);

const CHAIN_ID = 31337;
const DEPLOYMENT_ID = "task6-anvil";
const snapshot = {
  chain_id: CHAIN_ID,
  as_of_block: 10,
  as_of_block_hash: "0xabc",
  finality: "safe",
};
const longName = "🚀".repeat(64);
const tokenA = "0x00000000000000000000000000000000000000Aa";

function trade(n: number, over: Record<string, unknown> = {}) {
  return {
    chain_id: CHAIN_ID,
    deployment_id: DEPLOYMENT_ID,
    token: tokenA,
    name: longName,
    symbol: "ROCKET",
    source: "curve",
    trader: null,
    side: "buy",
    eth_volume: "1500000000000000000",
    token_volume: "5000000000000000000",
    block_number: n,
    transaction_index: 0,
    tx_hash: `0x${n.toString(16).padStart(64, "0")}`,
    log_index: 0,
    time: new Date().toISOString(),
    finality: "observed",
    ...over,
  };
}

async function installApi(page: Page) {
  await page.route("**/v1/**", async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname.endsWith("/events")) {
      await route.fulfill({ status: 200, contentType: "text/event-stream", body: "" });
    } else if (url.pathname.endsWith("/trades/recent")) {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ snapshot, items: [trade(2), trade(1, { side: "sell" })] }),
      });
    } else if (url.pathname.endsWith("/eth-usd")) {
      await route.fulfill({ status: 404, contentType: "application/json", body: "{}" });
    } else {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ snapshot, items: [], next_cursor: undefined }),
      });
    }
  });
}

test("live strip lists recent trades, links to the token and truncates long names", async ({
  page,
}) => {
  await installApi(page);
  await page.goto("/");
  const strip = page.getByRole("region", { name: "Live activity" });
  await expect(strip).toBeVisible();
  const items = strip.locator("li");
  await expect(items).toHaveCount(2);
  await expect(items.first().getByRole("link")).toHaveAttribute(
    "href",
    `/token/${tokenA.toLowerCase()}`,
  );
  await expect(items.first()).toContainText("—");
  const ticker = items.first().locator(".live-strip-ticker");
  const style = await ticker.evaluate((element) => {
    const computed = getComputedStyle(element);
    return { overflow: computed.overflow, textOverflow: computed.textOverflow };
  });
  expect(style).toEqual({ overflow: "hidden", textOverflow: "ellipsis" });
  // Observed items read as pending until finality.
  await expect(items.first()).toHaveAttribute("data-pending", "true");
});

test("live strip is a static scrollable row under reduced motion", async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== "reduced-motion", "Reduced-motion project only.");
  await installApi(page);
  await page.goto("/");
  const track = page.locator(".live-strip-track");
  await expect(track).toBeVisible();
  const result = await track.evaluate((element) => ({
    overflowX: getComputedStyle(element).overflowX,
    animated: [...element.querySelectorAll("li")].some(
      (li) => getComputedStyle(li).animationName !== "none",
    ),
  }));
  expect(result).toEqual({ overflowX: "auto", animated: false });
});

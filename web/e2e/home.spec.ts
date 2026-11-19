import { expect, test, type Page } from "@playwright/test";

// The hero renders in every build; the data sections need the READY fixture configuration
// (TASK6_ANVIL_* variables) and mock every API call with page.route.
const ready = Boolean(process.env.TASK6_ANVIL_API_URL);

const snapshot = { chain_id: 31337, as_of_block: 10, as_of_block_hash: "0xabc", finality: "safe" };

function token(n: number, over: Record<string, unknown> = {}) {
  return {
    address: `0x${n.toString(16).padStart(40, "0")}`,
    holder_count: n,
    launch_block: n,
    launch_time: new Date().toISOString(),
    market_cap_eth: `${BigInt(n) * 10n ** 18n}`,
    name: `Token ${n}`,
    phase: "curve",
    symbol: `T${n}`,
    total_supply: `${10n ** 27n}`,
    volume_24h_eth: "0",
    ...over,
  };
}

async function installApi(page: Page, empty: boolean) {
  await page.route("**/v1/**", async (route) => {
    const url = new URL(route.request().url());
    const json = (body: unknown) =>
      route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(body) });
    if (url.pathname.endsWith("/events")) {
      await route.fulfill({ status: 200, contentType: "text/event-stream", body: "" });
    } else if (url.pathname.endsWith("/tokens/highlights")) {
      await json({
        as_of: new Date().toISOString(),
        finality: "safe",
        king_of_the_hill: empty
          ? null
          : {
              ...token(7, { name: "<b>King</b>" }),
              graduation_progress_bps: 4200,
              remaining_eth: `${10n ** 18n}`,
            },
        snapshot,
        trending: empty
          ? []
          : [{ ...token(8, { name: "<b>Hot</b>" }), window_volume_eth: "0", distinct_buyers: 2 }],
        window_start: new Date().toISOString(),
      });
    } else if (url.pathname.endsWith("/tokens")) {
      await json({ snapshot, items: empty ? [] : [token(9, { phase: "graduated" })] });
    } else if (url.pathname.endsWith("/forum/posts")) {
      await json({
        items: empty
          ? []
          : [
              {
                id: "1",
                token: token(9).address,
                name: "Token 9",
                symbol: "T9",
                body: "First post",
                created_at: new Date().toISOString(),
                author: { username: "ana", wallet: null, role: "holder" },
              },
            ],
      });
    } else if (url.pathname.endsWith("/trades/recent")) {
      await json({ snapshot, items: [] });
    } else {
      await route.fulfill({ status: 404, contentType: "application/json", body: "{}" });
    }
  });
}

test("the home page opens on its sections with a screen-reader h1", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { level: 1, name: "Launchpad" })).toBeAttached();
  await expect(page.getByText("Trade the curve.")).toHaveCount(0);
});

test("sections link to their full views and render names as text", async ({ page }) => {
  test.skip(!ready, "Needs the READY fixture configuration.");
  await installApi(page, false);
  await page.goto("/");
  // A name is user-controlled text: markup in it shows literally.
  await expect(
    page.getByRole("region", { name: "Trending" }).getByText("<b>Hot</b>"),
  ).toBeVisible();
  await expect(page.getByRole("region", { name: "Featured" })).toHaveCount(0);
  for (const [title, href] of [
    ["Trending", "/explore?view=trending"],
    ["Graduated", "/explore?view=graduated"],
    ["From the forum", "/forum"],
  ] as const) {
    const section = page.getByRole("region", { name: title });
    await expect(
      section.getByRole("heading", { level: 2 }).getByRole("link", { name: title }),
    ).toHaveAttribute("href", href);
  }
  await expect(
    page.getByRole("region", { name: "Graduated" }).getByRole("link", { name: "Token 9" }),
  ).toBeVisible();
  await expect(
    page.getByRole("region", { name: "From the forum" }).getByText("First post"),
  ).toBeVisible();
});

test("empty lists show their empty states instead of a blank area", async ({ page }) => {
  test.skip(!ready, "Needs the READY fixture configuration.");
  await installApi(page, true);
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Nothing is trending yet" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "No graduated tokens yet" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "No forum posts yet" })).toBeVisible();
});

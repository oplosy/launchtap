import { expect, test, type Page } from "@playwright/test";

// Needs the fixture build (TASK6_ANVIL_* variables) so the configuration is ready. Every API call
// is mocked with page.route; the forum reads run in the browser.
test.skip(
  !process.env.TASK6_ANVIL_API_URL,
  "Needs the fixture build (TASK6_ANVIL_* variables) so the configuration is ready.",
);

const TOKEN = "0x00000000000000000000000000000000000000Aa";
const OTHER = "0x00000000000000000000000000000000000000Bb";
const WALLET = "0x00000000000000000000000000000000000000c1";

const post = (id: number, overrides: Record<string, unknown> = {}) => ({
  id: String(id),
  token: TOKEN,
  body: `post ${id}`,
  created_at: "2026-11-10T09:00:00.000000Z",
  author: { username: null, wallet: WALLET, role: "holder" },
  name: "Pons",
  symbol: "PONS",
  score: 0,
  ...overrides,
});

type Calls = { posts: string[]; communities: string[] };

async function installApi(page: Page, calls: Calls) {
  await page.route("**/v1/**", async (route) => {
    const url = new URL(route.request().url());
    const json = (body: unknown) =>
      route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(body) });
    if (url.pathname.endsWith("/forum/posts")) {
      calls.posts.push(url.search);
      if (url.searchParams.get("sort") === "best")
        return json({ items: [post(7, { body: "most liked", score: 12 }), post(3, { score: 1 })] });
      if (url.searchParams.get("cursor") === "page2")
        return json({ items: [post(3), post(1, { body: "oldest" })] });
      return json({
        items: [
          post(5, {
            body: "<img src=x onerror=window.__pwned=1>",
            symbol: "<b>EVIL</b>",
            author: { username: "alice", wallet: null, role: "creator" },
          }),
          post(4, { token: OTHER, symbol: "OTH" }),
          post(3),
        ],
        next_cursor: "page2",
      });
    }
    if (url.pathname.endsWith("/forum/communities")) {
      calls.communities.push(url.searchParams.get("sort") ?? "");
      return json({
        items: [
          {
            token: TOKEN,
            name: "Pons",
            symbol: "PONS",
            comment_count: 12,
            last_comment_at: "2026-11-10T09:00:00.000000Z",
          },
        ],
      });
    }
    if (url.pathname.endsWith("/tokens"))
      return json({
        snapshot: { chain_id: 31337, as_of_block: 10, as_of_block_hash: "0xabc", finality: "safe" },
        items: [],
      });
    if (url.pathname.endsWith("/eth-usd"))
      return route.fulfill({ status: 404, contentType: "application/json", body: "{}" });
    if (url.pathname.endsWith("/events"))
      return route.fulfill({ status: 200, contentType: "text/event-stream", body: "" });
    return json({ items: [] });
  });
}

test("the forum feed lists posts as text, links tokens and loads more", async ({ page }) => {
  const calls: Calls = { posts: [], communities: [] };
  await installApi(page, calls);
  await page.goto("/forum");
  await expect(page.getByRole("heading", { level: 1, name: "Forum" })).toBeAttached();
  const feed = page.getByRole("region", { name: "Forum posts" });
  await expect(feed.getByText("<img src=x onerror=window.__pwned=1>")).toBeVisible();
  await expect(feed.getByRole("link", { name: "s/<b>EVIL</b>" })).toHaveAttribute(
    "href",
    new RegExp(`/token/${TOKEN}$`, "i"),
  );
  await expect(feed.getByText("alice")).toBeVisible();
  await expect(feed.getByRole("link", { name: "s/OTH" })).toHaveAttribute(
    "href",
    new RegExp(`/token/${OTHER}$`, "i"),
  );
  await expect(feed.getByRole("link", { name: "Trade PONS" }).first()).toHaveAttribute(
    "href",
    new RegExp(`/token/${TOKEN}$`, "i"),
  );

  await feed.getByRole("button", { name: "Load more" }).click();
  await expect(feed.getByText("oldest")).toBeVisible();
  // post 3 came on both pages and is shown once.
  await expect(feed.getByText("post 3", { exact: true })).toHaveCount(1);
  await expect(feed.getByRole("button", { name: "Load more" })).toHaveCount(0);
  expect(calls.posts).toEqual(["?limit=20", "?cursor=page2&limit=20"]);
  expect(await page.evaluate(() => (window as unknown as { __pwned?: number }).__pwned)).toBe(
    undefined,
  );

  await expect(page.getByRole("heading", { name: "Biggest launches" })).toBeVisible();
  await expect(page.getByRole("link", { name: "Browse the launchpad" })).toBeVisible();
  // Header link on desktop, menu/footer link on mobile: some link to /forum is always present.
  await expect(page.locator('a[href="/forum"]').first()).toBeAttached();
});

test("the best tab lists posts ranked by their votes", async ({ page }) => {
  const calls: Calls = { posts: [], communities: [] };
  await installApi(page, calls);
  await page.goto("/forum");
  const feed = page.getByRole("region", { name: "Forum posts" });
  await feed.getByRole("link", { name: "Best" }).click();
  await expect(page).toHaveURL(/\/forum\?tab=best$/);
  const posts = feed.getByRole("listitem");
  await expect(posts.first()).toContainText("most liked");
  await expect(posts.first()).toContainText("12 points");
  await expect(posts.nth(1)).toContainText("1 point");
  await expect(feed.getByRole("button", { name: "Load more" })).toHaveCount(0);
  expect(calls.posts).toEqual(["?limit=20", "?sort=best&limit=20"]);
});

test("hot and top tabs rank communities from the URL", async ({ page }) => {
  const calls: Calls = { posts: [], communities: [] };
  await installApi(page, calls);
  await page.goto("/forum?tab=hot");
  const feed = page.getByRole("region", { name: "Forum posts" });
  await expect(feed.getByText("12 posts today")).toBeVisible();
  await expect(feed.getByRole("link", { name: "Hot" })).toHaveAttribute("aria-current", "page");
  await feed.getByRole("link", { name: "Top" }).click();
  await expect(page).toHaveURL(/\/forum\?tab=top$/);
  await expect(feed.getByText("12 posts", { exact: true })).toBeVisible();
  expect(calls.communities).toEqual(["hot", "top"]);
  expect(calls.posts).toEqual([]);
});

test("forum tabs are centred over the feed and a whole post card opens its token", async ({
  page,
}, testInfo) => {
  await installApi(page, { posts: [], communities: [] });
  await page.goto("/forum");
  const tabs = page.getByRole("navigation", { name: "Forum view" });
  await expect(tabs.getByRole("link", { name: "New" })).toBeVisible();
  // Centred over the feed column (the launches rail sits beside it on wide screens).
  const box = (await tabs.boundingBox())!;
  const feed = (await page.getByRole("region", { name: "Forum posts" }).boundingBox())!;
  expect(Math.abs(box.x + box.width / 2 - (feed.x + feed.width / 2))).toBeLessThanOrEqual(2);
  await page.screenshot({
    path: `../.impeccable/review/rebuild-forum-${testInfo.project.name}.png`,
    fullPage: true,
    animations: "disabled",
  });
  // The body text is not a link itself: the row is one target that leads to the token.
  // A real pointer click on the text: the stretched row link sits above it and takes the click.
  const text = page.getByText("post 3", { exact: true });
  await text.scrollIntoViewIfNeeded();
  const body = (await text.boundingBox())!;
  await page.mouse.click(body.x + body.width / 2, body.y + body.height / 2);
  await expect(page).toHaveURL(new RegExp(`/token/${TOKEN}$`, "i"));
});

test("a failed Biggest launches load reads as a centred line", async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== "small-laptop", "One viewport is enough.");
  await page.route("**/v1/**", (route) =>
    new URL(route.request().url()).pathname.endsWith("/tokens")
      ? route.fulfill({ status: 500, contentType: "application/json", body: "{}" })
      : route.fulfill({ status: 200, contentType: "application/json", body: '{"items":[]}' }),
  );
  await page.goto("/forum");
  const error = page.getByText("Launches could not be loaded.");
  await expect(error).toBeVisible();
  expect(await error.evaluate((node) => getComputedStyle(node).textAlign)).toBe("center");
});

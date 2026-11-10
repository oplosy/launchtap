import { expect, test, type Page } from "@playwright/test";

// Every API call is mocked with page.route. The fixture build mounts no Privy provider, so the
// settings form itself is covered by public-profile-model unit tests; these specs pin how a
// published username is displayed on the wallet page.
test.skip(
  !process.env.TASK6_ANVIL_API_URL,
  "Needs the fixture build (TASK6_ANVIL_* variables) so the configuration is ready.",
);

const snapshot = { chain_id: 31337, as_of_block: 10, as_of_block_hash: "0xabc", finality: "safe" };
const NAMED = "0x00000000000000000000000000000000000000ab";
const PLAIN = "0x00000000000000000000000000000000000000ac";

async function installApi(page: Page) {
  await page.route("**/v1/**", async (route) => {
    const url = new URL(route.request().url());
    const path = url.pathname.replace(/^.*\/v1/, "");
    const json = (body: unknown, status = 200) =>
      route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });
    if (path === `/wallets/${NAMED}` || path === `/wallets/${PLAIN}`) {
      const address = path.split("/").pop()!;
      return json({
        snapshot,
        address,
        realized_pnl_wei: "0",
        unrealized_pnl_wei: "0",
        portfolio_value_wei: "0",
        held_count: 0,
        created_count: 0,
        tokens_traded: 0,
        first_block: null,
        last_block: null,
        attribution_complete: true,
        username: address === NAMED ? "alice" : null,
      });
    }
    if (path.endsWith("/eth-usd")) return json({}, 404);
    return json({ snapshot, items: [], next_cursor: undefined });
  });
  await page.route("**/v1/events**", (route) =>
    route.fulfill({ status: 200, contentType: "text/event-stream", body: "" }),
  );
}

test("a published username heads the wallet page", async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== "small-laptop", "One viewport is enough.");
  await installApi(page);

  await page.goto(`/wallet/${NAMED}`);
  await expect(page.getByRole("heading", { level: 1, name: "@alice" })).toBeVisible();
  await page.goto(`/wallet/${PLAIN}`);
  await expect(page.getByRole("heading", { level: 1 })).not.toContainText("@");
  // The leaderboard is rendered on the server, outside page.route; leaderboard-view.test.ts
  // pins its username label.
});

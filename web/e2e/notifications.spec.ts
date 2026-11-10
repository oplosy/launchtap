import { expect, test, type Page } from "@playwright/test";

// Needs the fixture build (TASK6_ANVIL_* variables) so the configuration is ready and the
// injected-wallet connector exists. Every API call is mocked with page.route.
test.skip(
  !process.env.TASK6_ANVIL_API_URL,
  "Needs the fixture build (TASK6_ANVIL_* variables) so the configuration is ready.",
);

const CHAIN_ID = 31337;
const DEPLOYMENT_ID = "task6-anvil";
const ACCOUNT = "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266";
const TOKEN = "0x00000000000000000000000000000000000000Aa";
const snapshot = {
  chain_id: CHAIN_ID,
  as_of_block: 10,
  as_of_block_hash: "0xabc",
  finality: "safe",
};

function sse(events: { event: string; data: unknown }[]) {
  return events.map((e) => `event: ${e.event}\ndata: ${JSON.stringify(e.data)}\n\n`).join("");
}

const ownTrade = {
  chain_id: CHAIN_ID,
  deployment_id: DEPLOYMENT_ID,
  token: TOKEN,
  name: "<b>Name</b>",
  symbol: "<i>TKN</i>",
  source: "curve",
  trader: ACCOUNT.toLowerCase(),
  side: "buy",
  eth_volume: "1000",
  token_volume: "1",
  block_number: 10,
  transaction_index: 0,
  tx_hash: `0x${"a".repeat(64)}`,
  log_index: 0,
  time: new Date().toISOString(),
  finality: "observed",
};

/** Mocks the API; the event stream stays open-pending until `release` is called. */
async function installApi(page: Page) {
  let release: () => void = () => undefined;
  const gate = new Promise<void>((resolve) => (release = resolve));
  await page.route("**/v1/**", async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname.endsWith("/events")) {
      await gate;
      await route
        .fulfill({
          status: 200,
          contentType: "text/event-stream",
          body: sse([
            { event: "trade", data: ownTrade },
            {
              event: "finality",
              data: {
                chain_id: CHAIN_ID,
                deployment_id: DEPLOYMENT_ID,
                safe_block: 10,
                finalized_block: 5,
              },
            },
          ]),
        })
        .catch(() => undefined);
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
  return release;
}

async function installWallet(page: Page) {
  await page.addInitScript((account) => {
    const listeners = new Map<string, Set<(...args: unknown[]) => void>>();
    Object.defineProperty(window, "ethereum", {
      configurable: true,
      value: {
        isMetaMask: false,
        on(event: string, listener: (...args: unknown[]) => void) {
          const set = listeners.get(event) ?? new Set();
          set.add(listener);
          listeners.set(event, set);
        },
        removeListener(event: string, listener: (...args: unknown[]) => void) {
          listeners.get(event)?.delete(listener);
        },
        request: async ({ method }: { method: string }) => {
          if (method === "eth_accounts" || method === "eth_requestAccounts") return [account];
          if (method === "eth_chainId") return "0x7a69";
          if (method === "wallet_requestPermissions") return [{ caveats: [] }];
          if (method === "wallet_getPermissions") return [];
          return null;
        },
      },
    });
  }, ACCOUNT);
}

test("a visitor sees no notification bell", async ({ page }) => {
  await installApi(page);
  await page.goto("/");
  await expect(page.getByRole("button", { name: /^Notifications/ })).toHaveCount(0);
});

test("a signed-out reader of the inbox gets a sign-in prompt and no inbox request", async ({
  page,
}) => {
  const inboxRequests: string[] = [];
  page.on("request", (request) => {
    if (request.url().includes("/v1/me/notifications")) inboxRequests.push(request.url());
  });
  await installApi(page);
  await page.goto("/notifications");
  await expect(page.getByRole("heading", { name: "Your notifications." })).toBeVisible();
  await expect(page.getByText(/Sign in to see your inbox|Inbox unavailable/).first()).toBeVisible();
  expect(inboxRequests).toEqual([]);
});

test("own trade reaching safe finality toasts and lists, with keyboard-accessible bell", async ({
  page,
}) => {
  const release = await installApi(page);
  await installWallet(page);
  await page.goto("/");
  await page.getByRole("button", { name: "Connect wallet" }).last().click();
  const dialog = page.getByRole("dialog", { name: "Choose a wallet" });
  await expect(dialog).toBeVisible();
  await dialog.getByRole("button", { name: "Browser wallet" }).click();
  const bell = page.getByRole("button", { name: /^Notifications/ });
  await expect(bell).toBeVisible();

  const status = page.locator(".notify-toasts");
  await expect(status).toHaveAttribute("role", "status");
  await expect(status).toHaveAttribute("aria-live", "polite");

  release();
  // Names are user input: rendered as text, never as markup.
  await expect(status).toContainText("<i>TKN</i>");
  await expect(status.locator("i")).toHaveCount(0);
  await expect(bell).toHaveAccessibleName(/1 unread/);

  await bell.focus();
  await page.keyboard.press("Enter");
  await expect(bell).toHaveAttribute("aria-expanded", "true");
  const panel = page.getByRole("region", { name: "Notifications" });
  await expect(panel).toContainText("Your <i>TKN</i> trade is safe");
  await expect(bell).toHaveAccessibleName("Notifications");

  await page.keyboard.press("Escape");
  await expect(panel).toHaveCount(0);
  await expect(bell).toBeFocused();
});

test("the bell panel closes when focus leaves it", async ({ page }) => {
  const release = await installApi(page);
  await installWallet(page);
  await page.goto("/");
  await page.getByRole("button", { name: "Connect wallet" }).last().click();
  const dialog = page.getByRole("dialog", { name: "Choose a wallet" });
  await dialog.getByRole("button", { name: "Browser wallet" }).click();
  const bell = page.getByRole("button", { name: /^Notifications/ });
  await expect(bell).toBeVisible();
  release();
  await expect(bell).toHaveAccessibleName(/1 unread/);

  await bell.click();
  const panel = page.getByRole("region", { name: "Notifications" });
  await expect(panel).toBeVisible();
  // Tabbing into the panel keeps it open: focus is still inside the bell.
  await page.keyboard.press("Tab");
  await expect(panel.getByRole("link")).toBeFocused();
  await expect(panel).toBeVisible();
  // Moving focus elsewhere (keyboard or programmatic) closes it.
  await page.getByRole("link", { name: "Open home" }).focus();
  await expect(panel).toHaveCount(0);
  await expect(bell).toHaveAttribute("aria-expanded", "false");
});

test("a connected wallet is notified of a held token's graduation without a watchlist entry", async ({
  page,
}) => {
  let release: () => void = () => undefined;
  const gate = new Promise<void>((resolve) => (release = resolve));
  let positionsServed: () => void = () => undefined;
  const positionsDone = new Promise<void>((resolve) => (positionsServed = resolve));
  await page.route("**/v1/**", async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname.endsWith("/events")) {
      await gate;
      await route
        .fulfill({
          status: 200,
          contentType: "text/event-stream",
          body: sse([
            {
              event: "graduation",
              data: {
                chain_id: CHAIN_ID,
                deployment_id: DEPLOYMENT_ID,
                token: TOKEN,
                name: "Held",
                symbol: "HELD",
                pair: "0x0000000000000000000000000000000000000001",
                block_number: 10,
                tx_hash: `0x${"b".repeat(64)}`,
              },
            },
          ]),
        })
        .catch(() => undefined);
    } else if (url.pathname.endsWith(`/wallets/${ACCOUNT.toLowerCase()}/positions`)) {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ snapshot, items: [{ token: TOKEN }] }),
      });
      positionsServed();
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
  await installWallet(page);
  await page.goto("/");
  await page.getByRole("button", { name: "Connect wallet" }).last().click();
  const dialog = page.getByRole("dialog", { name: "Choose a wallet" });
  await dialog.getByRole("button", { name: "Browser wallet" }).click();
  await positionsDone;
  release();
  await expect(page.locator(".notify-toasts")).toContainText("HELD graduated");
});

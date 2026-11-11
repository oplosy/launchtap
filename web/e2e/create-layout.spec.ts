import { expect, test, type Page } from "@playwright/test";

// The launch form renders only with the READY fixture configuration (TASK6_ANVIL_* variables).
// The RPC is mocked so the factory reads succeed (zero words: no fee, nothing paused).
test.skip(
  !process.env.TASK6_ANVIL_API_URL,
  "Needs the fixture build (TASK6_ANVIL_* variables) so the configuration is ready.",
);

const ZERO_WORDS = `0x${"0".repeat(64 * 16)}`;

async function installRpc(page: Page) {
  // The browser reaches the RPC through the app's same-origin /e2e/rpc proxy.
  await page.route("**/e2e/rpc", async (route) => {
    const body = JSON.parse(route.request().postData() ?? "{}") as
      { id: number; method: string } | { id: number; method: string }[];
    const answer = ({ id, method }: { id: number; method: string }) => ({
      jsonrpc: "2.0",
      id,
      result: method === "eth_chainId" ? "0x7a69" : method === "eth_call" ? ZERO_WORDS : null,
    });
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(Array.isArray(body) ? body.map(answer) : answer(body)),
    });
  });
  await page.route("**/v1/**", (route) =>
    route.fulfill({ status: 404, contentType: "application/json", body: "{}" }),
  );
}

test("the launch form sits left of the token preview on a laptop", async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== "small-laptop", "One viewport is enough.");
  await installRpc(page);
  await page.goto("/create");
  await expect(page.getByRole("textbox", { name: "Name" })).toBeVisible();
  const form = (await page.locator(".launch-main > .transaction-form").boundingBox())!;
  const preview = (await page.getByRole("complementary", { name: "Token preview" }).boundingBox())!;
  // 60/40: the form column is wider and ends before the preview column starts.
  expect(form.x + form.width).toBeLessThanOrEqual(preview.x);
  expect(form.width).toBeGreaterThan(preview.width);
  await page.screenshot({
    path: "../.impeccable/review/rebuild-create-wide.png",
    fullPage: true,
    animations: "disabled",
  });
});

test("the four sections appear in order", async ({ page }) => {
  await installRpc(page);
  await page.goto("/create");
  const form = page.getByRole("region", { name: "Launch token" });
  await expect(page.getByRole("textbox", { name: "Name" })).toBeVisible();
  await expect(form.getByRole("heading", { level: 2 })).toHaveText([
    "Image",
    "Name and symbol",
    // The Links heading also carries its info tip's text.
    /^Links/,
    "Initial buy",
  ]);
  await expect
    .poll(() => page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth))
    .toBe(0);
});

test("focusing an input changes its border colour", async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== "small-laptop", "One viewport is enough.");
  await installRpc(page);
  await page.goto("/create");
  const name = page.getByRole("textbox", { name: "Name" });
  await expect(name).toBeVisible();
  const border = () => name.evaluate((node) => getComputedStyle(node).borderColor);
  const before = await border();
  await name.focus();
  await expect.poll(border).not.toBe(before);
});

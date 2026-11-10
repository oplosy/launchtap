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

test("the launch form is one centred column of at most 640 px", async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== "small-laptop", "One viewport is enough.");
  await installRpc(page);
  await page.goto("/create");
  const form = page.getByRole("region", { name: "Launch token" });
  await expect(page.getByRole("textbox", { name: "Name" })).toBeVisible();
  const box = (await form.boundingBox())!;
  expect(box.width).toBeLessThanOrEqual(640);
  expect(Math.abs(box.x + box.width / 2 - 640)).toBeLessThanOrEqual(2);
  await page.screenshot({
    path: "../.impeccable/review/rebuild-create-wide.png",
    fullPage: true,
    animations: "disabled",
  });
});

test("the four numbered sections appear in order", async ({ page }) => {
  await installRpc(page);
  await page.goto("/create");
  const form = page.getByRole("region", { name: "Launch token" });
  await expect(page.getByRole("textbox", { name: "Name" })).toBeVisible();
  await expect(form.getByRole("heading", { level: 2 })).toHaveText([
    "01 Image",
    "02 Name and symbol",
    "03 Description and links",
    "04 Initial buy",
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

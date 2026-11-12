import { expect, test, type Page } from "@playwright/test";
import {
  createPublicClient,
  createWalletClient,
  decodeFunctionData,
  encodeFunctionData,
  http,
  parseAbi,
  parseEventLogs,
} from "viem";
import fs from "node:fs";
import path from "node:path";
import type { WalletResponse } from "../src/api/client";
import { browserAbis } from "../src/contracts/generated";
import { formatSignedEth } from "../src/portfolio/wallet-model";

const sender = "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266" as const;

async function installWallet(page: Page, initialChainId = "0x7a69") {
  await page.addInitScript(
    ({ account, initialChainId }) => {
      let rejectNext = false;
      let chainId = initialChainId;
      const writeRequests: { from?: string; to?: string; data?: string; value?: string }[] = [];
      Object.defineProperty(window, "__task6WriteRequests", {
        configurable: true,
        get: () => writeRequests,
      });
      Object.defineProperty(window, "__task6RejectNext", {
        configurable: true,
        get: () => rejectNext,
        set: (value) => {
          rejectNext = Boolean(value);
        },
      });
      Object.defineProperty(window, "__task6ChainId", {
        configurable: true,
        get: () => chainId,
        set: (value) => {
          chainId = String(value);
          for (const listener of listeners.get("chainChanged") ?? []) listener(chainId);
        },
      });
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
          request: async ({ method, params = [] }: { method: string; params?: unknown[] }) => {
            if (method === "eth_accounts" || method === "eth_requestAccounts") return [account];
            if (method === "wallet_requestPermissions") return [{ caveats: [] }];
            if (method === "wallet_getPermissions") return [];
            if (method === "wallet_switchEthereumChain") {
              chainId = "0x7a69";
              for (const listener of listeners.get("chainChanged") ?? []) listener(chainId);
              return null;
            }
            if (method === "eth_chainId") return chainId;
            if (method === "eth_sendTransaction" && rejectNext) {
              rejectNext = false;
              const error = new Error("User rejected the request");
              Object.assign(error, { code: 4001 });
              throw error;
            }
            if (method === "eth_sendTransaction")
              writeRequests.push(params[0] as { to?: string; data?: string; value?: string });
            const response = await fetch("/e2e/rpc", {
              method: "POST",
              headers: { "content-type": "application/json" },
              body: JSON.stringify({ jsonrpc: "2.0", id: Date.now(), method, params }),
            });
            const body = (await response.json()) as {
              result?: unknown;
              error?: { message?: string; code?: number; data?: unknown };
            };
            if (body.error) {
              const error = new Error(
                `${body.error.message ?? "RPC error"} ${JSON.stringify(body.error)}`,
              );
              Object.assign(error, body.error);
              throw error;
            }
            return body.result;
          },
        },
      });
    },
    { account: sender, initialChainId },
  );
}

// The notification toast region is also a polite live region (role="status") on every
// wallet route, so transaction status excludes it.
function transactionStatus(page: Page) {
  return page.locator('[role="status"]:not(.notify-toasts)');
}

async function connectWallet(page: Page) {
  await page.getByRole("button", { name: "Connect wallet" }).last().click();
  const dialog = page.getByRole("dialog", { name: "Choose a wallet" });
  await expect(dialog).toBeVisible();
  await dialog.getByRole("button", { name: "Browser wallet" }).click();
}

async function canonicalTokenForLaunch(page: Page, api: string, hash: string, name: string) {
  const response = await page.request.get(`${api}/v1/transactions/${hash}`);
  if (response.ok()) {
    const body = (await response.json()) as { events?: { kind: string; token?: string }[] };
    const eventToken = body.events?.find((event) => event.kind === "token_launch")?.token;
    if (eventToken) return eventToken;
  }
  const tokens = await page.request.get(
    `${api}/v1/tokens?phase=curve&q=${encodeURIComponent(name)}`,
  );
  if (!tokens.ok()) return "";
  const body = (await tokens.json()) as { items?: { name: string; address: string }[] };
  return body.items?.find((item) => item.name === name)?.address ?? "";
}

test.describe("Task 6 Anvil transaction gate", () => {
  const configured = Boolean(
    process.env.TASK6_ANVIL_RPC_URL &&
    process.env.TASK6_ANVIL_FACTORY &&
    process.env.TASK6_ANVIL_WETH &&
    process.env.TASK6_ANVIL_ROUTER &&
    process.env.TASK6_ANVIL_UNISWAP_FACTORY &&
    process.env.TASK6_ANVIL_API_URL,
  );
  test.beforeAll(() => {
    if (
      process.env.TASK6_ANVIL_REQUIRED === "1" &&
      (!process.env.TASK6_ANVIL_RPC_URL ||
        !process.env.TASK6_ANVIL_FACTORY ||
        !process.env.TASK6_ANVIL_WETH ||
        !process.env.TASK6_ANVIL_ROUTER ||
        !process.env.TASK6_ANVIL_UNISWAP_FACTORY ||
        !process.env.TASK6_ANVIL_API_URL)
    )
      throw new Error(
        "Task 6 Anvil gate requires the real RPC, API, factory, WETH, router, and pair factory configuration",
      );
  });
  test.beforeEach(({}, testInfo) => {
    if (!configured) testInfo.skip(true, "Run through the real Task 6 Anvil gate");
  });

  test("creates a token through the real create controls and reaches canonical observation", async ({
    page,
  }, testInfo) => {
    await installWallet(page);
    await page.goto("/create");
    await expect(page.getByRole("heading", { name: "Launch token" })).toBeVisible();
    await connectWallet(page);
    await page.getByLabel("Name", { exact: true }).fill("Browser Task 6");
    await page.getByLabel("Ticker", { exact: true }).fill("B6");
    await page.getByRole("button", { name: "Review launch" }).click();
    await expect(page.getByRole("heading", { name: "Confirm launch" })).toBeVisible();
    const evidenceDir = path.resolve(process.cwd(), "..", ".impeccable", "review");
    fs.mkdirSync(evidenceDir, { recursive: true });
    await page.screenshot({
      path: path.join(evidenceDir, `task6-create-confirm-${testInfo.project.name}.png`),
      fullPage: true,
    });
    await page.getByRole("button", { name: "Sign launch" }).click();
    const status = transactionStatus(page);
    await expect(status).toContainText(/indexing|indexed|safe|finalized/i, { timeout: 30_000 });
    const hash = (await status.textContent())?.match(/0x[0-9a-f]{64}/i)?.[0];
    expect(hash).toBeTruthy();
    await expect
      .poll(
        async () =>
          (
            await page.request.get(`${process.env.TASK6_ANVIL_API_URL}/v1/transactions/${hash}`)
          ).status(),
        { timeout: 30_000 },
      )
      .toBe(200);
  });

  test("buys and sells through curve controls with a separate approval transaction", async ({
    page,
  }, testInfo) => {
    await installWallet(page);
    await page.goto("/create");
    await connectWallet(page);
    await page.getByLabel("Name", { exact: true }).fill("Browser Trade Task 6");
    await page.getByLabel("Ticker", { exact: true }).fill("BT6");
    await page.getByRole("button", { name: "Review launch" }).click();
    await page.getByRole("button", { name: "Sign launch" }).click();
    const launchStatus = transactionStatus(page).last();
    await expect(launchStatus).toContainText(/indexing|indexed|safe|finalized/i, {
      timeout: 30_000,
    });
    const launchHash = (await launchStatus.textContent())?.match(/0x[0-9a-f]{64}/i)?.[0];
    expect(launchHash).toBeTruthy();
    const api = process.env.TASK6_ANVIL_API_URL!;
    let tokenAddress = "";
    await expect
      .poll(
        async () => {
          tokenAddress = await canonicalTokenForLaunch(
            page,
            api,
            launchHash!,
            "Browser Trade Task 6",
          );
          return tokenAddress;
        },
        { timeout: 30_000 },
      )
      .toMatch(/^0x[0-9a-f]{40}$/i);
    await page.goto(`/token/${tokenAddress}`);
    await expect(page.getByRole("heading", { name: "Browser Trade Task 6" })).toBeVisible({
      timeout: 30_000,
    });
    const buyTab = page.getByRole("tab", { name: "Buy", exact: true });
    await buyTab.focus();
    await expect(buyTab).toHaveAttribute("tabindex", "0");
    await page.keyboard.press("ArrowRight");
    const sellTab = page.getByRole("tab", { name: "Sell", exact: true });
    await expect(sellTab).toHaveAttribute("aria-selected", "true");
    await expect(sellTab).toHaveAttribute("tabindex", "0");
    await expect(sellTab).toBeFocused();
    const tabPanel = page.getByRole("tabpanel", { name: "Sell", exact: true });
    await expect(tabPanel).toHaveCount(1);
    const panelId = await tabPanel.getAttribute("id");
    const sellTabId = await sellTab.getAttribute("id");
    expect(panelId).toBeTruthy();
    expect(sellTabId).toBeTruthy();
    await expect(sellTab).toHaveAttribute("aria-controls", panelId!);
    await expect(tabPanel).toHaveAttribute("aria-labelledby", sellTabId!);
    await page.keyboard.press("Home");
    await expect(buyTab).toHaveAttribute("aria-selected", "true");
    await expect(buyTab).toBeFocused();
    await page.getByLabel("ETH input").fill("0.001");
    await page.getByRole("button", { name: "Review buy" }).click();
    const tradeReview = page.getByRole("region", { name: "Trade confirmation" });
    const reviewedField = (name: string) =>
      tradeReview.getByText(name, { exact: true }).locator("xpath=following-sibling::dd[1]");
    await expect(tradeReview.getByText("Chain ID", { exact: true })).toBeVisible();
    await expect(tradeReview.getByText("Wallet", { exact: true })).toBeVisible();
    await expect(tradeReview.getByText("Target", { exact: true })).toBeVisible();
    await expect(tradeReview.getByText("Function and arguments", { exact: true })).toBeVisible();
    await expect(tradeReview.getByText("Native value", { exact: true })).toBeVisible();
    await expect(tradeReview.getByText("Deadline", { exact: true })).toBeVisible();
    // The curve fee is listed as its protocol and creator shares (CurveMath.splitFees).
    await expect(reviewedField("Protocol fee")).toHaveText(/^\d+(\.\d+)? ETH$/);
    await expect(reviewedField("Creator fee")).toHaveText(/^\d+(\.\d+)? ETH$/);
    await expect(reviewedField("Network fee")).toHaveText("Shown and paid in your wallet");

    // Change the on-chain quote while the original confirmation remains open.
    const reviewedDeadline = BigInt(
      (await reviewedField("Deadline").textContent())!.split(" ")[0]!,
    );
    const curveRiskText = (await page.locator(".transaction-risk").first().textContent()) ?? "";
    const curveAddress = curveRiskText.match(/0x[0-9a-f]{40}/i)?.[0];
    expect(curveAddress).toBeTruthy();
    const externalBuyData = encodeFunctionData({
      abi: browserAbis.curve,
      functionName: "buy",
      args: [sender, sender, 0n, reviewedDeadline],
    });
    const externalWrite = await page.request.post("/e2e/rpc", {
      data: {
        jsonrpc: "2.0",
        id: Date.now(),
        method: "eth_sendTransaction",
        params: [
          { from: sender, to: curveAddress, data: externalBuyData, value: "0x38d7ea4c68000" },
        ],
      },
    });
    expect(externalWrite.ok()).toBeTruthy();
    const externalBody = (await externalWrite.json()) as { result?: string; error?: unknown };
    expect(externalBody.result).toMatch(/^0x[0-9a-f]{64}$/i);
    await expect
      .poll(async () => {
        const response = await page.request.post("/e2e/rpc", {
          data: {
            jsonrpc: "2.0",
            id: Date.now(),
            method: "eth_getTransactionReceipt",
            params: [externalBody.result],
          },
        });
        const body = (await response.json()) as { result?: { status?: string } | null };
        return body.result?.status;
      })
      .toBe("0x1");

    const pageWriteCount = () =>
      page.evaluate(
        () =>
          (window as unknown as { __task6WriteRequests: unknown[] }).__task6WriteRequests.length,
      );
    expect(await pageWriteCount()).toBe(0);
    const evidenceDir = path.resolve(process.cwd(), "..", ".impeccable", "review");
    fs.mkdirSync(evidenceDir, { recursive: true });
    await page.screenshot({
      path: path.join(evidenceDir, `task6-trade-confirm-${testInfo.project.name}.png`),
      fullPage: true,
    });
    await page.getByRole("button", { name: "Sign buy" }).click();
    await expect(tradeReview.getByRole("status")).toContainText("details changed");
    expect(await pageWriteCount()).toBe(0);
    const reviewedWallet = (await reviewedField("Wallet").textContent())?.trim();
    const reviewedChainId = Number((await reviewedField("Chain ID").textContent())?.trim());
    const reviewedTarget = (await reviewedField("Target").textContent())?.trim();
    const reviewedCall = (await reviewedField("Function and arguments").textContent())?.trim();
    const reviewedValueText = (await reviewedField("Native value").textContent()) ?? "";
    const reviewedValueWei = BigInt(reviewedValueText.match(/\((\d+) wei\)/)?.[1] ?? "-1");
    const finalReviewedDeadline = BigInt(
      (await reviewedField("Deadline").textContent())!.split(" ")[0]!,
    );
    expect(reviewedWallet).toBeTruthy();
    expect(reviewedChainId).toBe(31337);
    expect(reviewedTarget?.toLowerCase()).toBe(curveAddress?.toLowerCase());
    expect(reviewedCall).toMatch(/^buy\(/);
    expect(reviewedValueWei).toBeGreaterThan(0n);
    await page.getByRole("button", { name: "Sign buy" }).click();
    await expect(transactionStatus(page).last()).toContainText(/indexing|indexed|safe|finalized/i, {
      timeout: 30_000,
    });
    const writeRequests = await page.evaluate(
      () =>
        (
          window as unknown as {
            __task6WriteRequests: { from?: string; to?: string; data?: string; value?: string }[];
          }
        ).__task6WriteRequests,
    );
    const signedRequest = writeRequests.at(-1);
    const signedCall = decodeFunctionData({
      abi: browserAbis.curve,
      data: signedRequest?.data as `0x${string}`,
    });
    expect(signedCall.functionName).toBe("buy");
    expect(reviewedCall).toBe(
      `${signedCall.functionName}(${signedCall.args.map(String).join(", ")})`,
    );
    expect(signedRequest?.from?.toLowerCase()).toBe(reviewedWallet?.toLowerCase());
    expect(signedRequest?.to?.toLowerCase()).toBe(reviewedTarget?.toLowerCase());
    expect(BigInt(signedRequest?.value ?? "0x0")).toBe(reviewedValueWei);
    expect(String(signedCall.args.at(-1))).toBe(finalReviewedDeadline.toString());
    const signingChainId = await page.evaluate(() =>
      (
        window as unknown as {
          ethereum: { request: (args: { method: string }) => Promise<unknown> };
        }
      ).ethereum.request({ method: "eth_chainId" }),
    );
    expect(Number(BigInt(String(signingChainId)))).toBe(reviewedChainId);
    const savedHash = (await page.locator(".transaction-progress").last().textContent())?.match(
      /0x[0-9a-f]{64}/i,
    )?.[0];
    expect(savedHash).toBeTruthy();

    await page.reload();
    await expect(page.locator(".transaction-progress").last()).toContainText(savedHash!);
    await page.getByRole("tab", { name: "Sell" }).click();
    await page.getByLabel(/BT6 input/).fill("1");
    await page.getByRole("button", { name: "Review sell" }).click();
    await page.getByRole("button", { name: "Sign sell" }).click();
    const tradeStatus = page.locator(".transaction-progress").first();
    const approvalStatus = page.locator(".transaction-progress").last();
    await expect(approvalStatus).toContainText(/approval mined/i, { timeout: 30_000 });
    const priorTradeHash = (await tradeStatus.textContent())?.match(/0x[0-9a-f]{64}/i)?.[0];
    expect(priorTradeHash).toBeTruthy();
    await page.getByRole("button", { name: "Resume sell" }).click();
    await page.screenshot({
      path: path.join(evidenceDir, `task6-approval-confirm-${testInfo.project.name}.png`),
      fullPage: true,
    });
    const writesBeforeResumedSell = await pageWriteCount();
    await page.getByRole("button", { name: "Sign sell" }).click();
    await expect.poll(pageWriteCount, { timeout: 10_000 }).toBeGreaterThan(writesBeforeResumedSell);
    await expect(tradeStatus).not.toContainText(priorTradeHash!);
    await expect(tradeStatus).toContainText(/indexed|safe|finalized/i, { timeout: 30_000 });
    const savedSellHash = (await tradeStatus.textContent())?.match(/0x[0-9a-f]{64}/i)?.[0];
    expect(savedSellHash).toBeTruthy();
    const seededCanonicalStatus = await page.evaluate((hash) => {
      const records = JSON.parse(
        window.localStorage.getItem("launchpad.transactions.v1") ?? "[]",
      ) as { action?: string; hash?: string; status?: string }[];
      const record = records.find(
        (value) => value.action === "trade" && value.hash?.toLowerCase() === hash.toLowerCase(),
      );
      if (!record) return false;
      record.status = "safe";
      window.localStorage.setItem("launchpad.transactions.v1", JSON.stringify(records));
      return true;
    }, savedSellHash!);
    expect(seededCanonicalStatus).toBe(true);

    const canonicalRoute = "**/v1/transactions/*";
    let canonicalAbsenceResponses = 0;
    await page.route(canonicalRoute, async (route) => {
      if (!route.request().url().toLowerCase().endsWith(savedSellHash!.toLowerCase())) {
        await route.continue();
        return;
      }
      canonicalAbsenceResponses += 1;
      await route.fulfill({
        status: 404,
        contentType: "application/problem+json",
        body: JSON.stringify({ type: "about:blank", status: 404, title: "Not found" }),
      });
    });
    await page.reload();
    await expect(page.locator(".transaction-progress").first()).toContainText(savedSellHash!);
    await expect.poll(() => canonicalAbsenceResponses).toBeGreaterThan(0);
    await expect(page.locator(".transaction-progress").first()).toContainText(/indexing/i);
    await expect(page.getByRole("button", { name: "Review buy" })).toBeDisabled();
    expect(await pageWriteCount()).toBe(0);
    await page.unroute(canonicalRoute);
  });

  test("switches from a wrong chain before signing", async ({ page }) => {
    await installWallet(page, "0x1");
    await page.goto("/create");
    await connectWallet(page);
    await expect(page.getByText(/Wrong network/i)).toBeVisible();
    await page.getByRole("button", { name: "Switch network" }).click();
    await expect(page.getByText(/Wrong network/i)).toHaveCount(0);
    await page.getByLabel("Name", { exact: true }).fill("Wrong Chain Task 6");
    await page.getByLabel("Ticker", { exact: true }).fill("WC6");
    await page.getByRole("button", { name: "Review launch" }).click();
    await page.evaluate(() => {
      (window as unknown as { __task6ChainId: string }).__task6ChainId = "0x1";
    });
    await page.waitForTimeout(250);
    await expect(page.getByText(/Wrong network/i)).toBeVisible();
    await page.getByRole("button", { name: "Switch network" }).click();
    await expect(page.getByText(/Wrong network/i)).toHaveCount(0);
  });

  test("keeps wallet rejection distinct while using the launch controls", async ({ page }) => {
    await installWallet(page);
    await page.goto("/create");
    await connectWallet(page);
    await page.getByLabel("Name", { exact: true }).fill("Rejected Task 6");
    await page.getByLabel("Ticker", { exact: true }).fill("R6");
    await page.getByRole("button", { name: "Review launch" }).click();
    await page.evaluate(() => {
      (window as unknown as { __task6RejectNext: boolean }).__task6RejectNext = true;
    });
    await page.getByRole("button", { name: "Sign launch" }).click();
    await expect(transactionStatus(page)).toContainText(/rejected-signature/i, {
      timeout: 10_000,
    });
  });

  test("surfaces a decoded contract cap revert from the real launch controls", async ({ page }) => {
    await installWallet(page);
    await page.goto("/create");
    await connectWallet(page);
    await page.getByLabel("Name", { exact: true }).fill("Revert Task 6");
    await page.getByLabel("Ticker", { exact: true }).fill("RV6");
    await page.getByLabel("Developer buy", { exact: true }).fill("0.02");
    await page.getByRole("button", { name: "Review launch" }).click();
    await page.getByRole("button", { name: "Sign launch" }).click();
    await expect(page.locator(".transaction-error")).toContainText(
      /developer buy exceeds the contract cap/i,
      { timeout: 15_000 },
    );
    await expect(transactionStatus(page)).toContainText(/simulation-reverted/i);
  });

  test("uses the reviewed graduated router for wrong-chain, buy, approval, and sell", async ({
    page,
  }, testInfo) => {
    await installWallet(page);
    await page.goto("/create");
    await connectWallet(page);
    await page.getByLabel("Name", { exact: true }).fill("Graduated Task 6");
    await page.getByLabel("Ticker", { exact: true }).fill("GR6");
    await page.getByRole("button", { name: "Review launch" }).click();
    await page.getByRole("button", { name: "Sign launch" }).click();
    const launchStatus = transactionStatus(page).last();
    await expect(launchStatus).toContainText(/indexing|indexed|safe|finalized/i, {
      timeout: 30_000,
    });
    const launchHash = (await launchStatus.textContent())?.match(/0x[0-9a-f]{64}/i)?.[0];
    expect(launchHash).toBeTruthy();
    const api = process.env.TASK6_ANVIL_API_URL!;
    let tokenAddress = "";
    await expect
      .poll(
        async () => {
          tokenAddress = await canonicalTokenForLaunch(page, api, launchHash!, "Graduated Task 6");
          return tokenAddress;
        },
        { timeout: 30_000 },
      )
      .toMatch(/^0x[0-9a-f]{40}$/i);
    await page.goto(`/token/${tokenAddress}`);
    await page.getByLabel("ETH input").fill("5");
    await page.getByRole("button", { name: "Review buy" }).click();
    await page.getByRole("button", { name: "Sign buy" }).click();
    await expect(transactionStatus(page).last()).toContainText(/indexing|indexed|safe|finalized/i, {
      timeout: 45_000,
    });
    const graduationHash = (await transactionStatus(page).last().textContent())?.match(
      /0x[0-9a-f]{64}/i,
    )?.[0];
    expect(graduationHash).toBeTruthy();
    const client = createPublicClient({ transport: http(process.env.TASK6_ANVIL_RPC_URL!) });
    const receipt = await client.getTransactionReceipt({ hash: graduationHash as `0x${string}` });
    expect(receipt.status).toBe("success");
    const events = parseEventLogs({
      abi: parseAbi([
        "event Graduated(address indexed token, address indexed lpPair, uint256 ethToPool, uint256 tokensToPool, uint256 lpLiquidityBurned)",
      ]),
      logs: receipt.logs,
    });
    expect(events).toHaveLength(1);
    const graduation = events[0];
    if (!graduation) throw new Error("Graduation receipt is missing its Graduated event");
    expect(graduation.args.token.toLowerCase()).toBe(tokenAddress.toLowerCase());
    expect(graduation.args.ethToPool).toBe(4_200_000_000_000_000_000n);
    expect(graduation.args.tokensToPool).toBe(200_000_000n * 10n ** 18n);
    expect(graduation.args.lpLiquidityBurned).toBeGreaterThan(0n);
    const erc20Abi = parseAbi([
      "function balanceOf(address) view returns (uint256)",
      "function totalSupply() view returns (uint256)",
    ]);
    const pair = graduation.args.lpPair;
    const burnAddress = "0x000000000000000000000000000000000000dEaD" as const;
    const balance = (address: `0x${string}`, owner: `0x${string}`) =>
      client.readContract({
        address,
        abi: erc20Abi,
        functionName: "balanceOf",
        args: [owner],
        blockNumber: receipt.blockNumber,
      });
    const burned = await balance(pair, burnAddress);
    expect(burned).toBe(graduation.args.lpLiquidityBurned);
    expect(await balance(pair, sender)).toBe(0n);
    expect(await balance(pair, graduation.address)).toBe(0n);
    const minimumLiquidity = await balance(pair, "0x0000000000000000000000000000000000000000");
    expect(minimumLiquidity).toBe(1000n);
    expect(
      await client.readContract({
        address: pair,
        abi: erc20Abi,
        functionName: "totalSupply",
        blockNumber: receipt.blockNumber,
      }),
    ).toBe(burned + minimumLiquidity);
    expect(await balance(tokenAddress as `0x${string}`, pair)).toBe(graduation.args.tokensToPool);
    expect(await balance(process.env.TASK6_ANVIL_WETH as `0x${string}`, pair)).toBe(
      graduation.args.ethToPool,
    );
    const evidencePath = testInfo.outputPath("graduation-lp-burn.json");
    fs.writeFileSync(
      evidencePath,
      JSON.stringify(
        {
          chainId: await client.getChainId(),
          launchHash,
          graduationHash,
          blockNumber: receipt.blockNumber.toString(),
          token: tokenAddress,
          curve: graduation.address,
          pair,
          burnAddress,
          ethToPool: graduation.args.ethToPool.toString(),
          tokensToPool: graduation.args.tokensToPool.toString(),
          lpAtBurnAddress: burned.toString(),
          minimumLiquidity: minimumLiquidity.toString(),
        },
        null,
        2,
      ),
    );
    await testInfo.attach("graduation-lp-burn", {
      path: evidencePath,
      contentType: "application/json",
    });
    await expect
      .poll(
        async () => {
          const response = await page.request.get(`${api}/v1/tokens/${tokenAddress}`);
          if (!response.ok()) return "";
          return ((await response.json()) as { phase?: string }).phase ?? "";
        },
        { timeout: 30_000 },
      )
      .toBe("graduated");
    await page.reload();
    await expect(page.getByRole("heading", { name: "Router handoff" })).toBeVisible({
      timeout: 30_000,
    });
    if (await page.getByRole("button", { name: "Connect wallet", exact: true }).isVisible())
      await connectWallet(page);
    await expect(page.getByRole("button", { name: "Connect wallet", exact: true })).toHaveCount(0, {
      timeout: 15_000,
    });
    await page.evaluate(() => {
      (window as unknown as { __task6ChainId: string }).__task6ChainId = "0x1";
    });
    await expect(page.getByText(/Wrong network/i)).toBeVisible();
    await page.getByRole("button", { name: "Switch network" }).click();
    await expect(page.getByText(/Wrong network/i)).toHaveCount(0);
    await page.getByLabel("ETH input").fill("0.001");
    await page.getByRole("button", { name: "Review buy" }).click();
    await expect(page.getByText(/0 ETH router fee/i)).toBeVisible();
    const evidenceDir = path.resolve(process.cwd(), "..", ".impeccable", "review");
    fs.mkdirSync(evidenceDir, { recursive: true });
    await page.screenshot({
      path: path.join(evidenceDir, `task6-graduated-confirm-${testInfo.project.name}.png`),
      fullPage: true,
    });
    await page.getByRole("button", { name: "Sign buy" }).click();
    await expect(transactionStatus(page).last()).toContainText(/indexing|indexed|safe|finalized/i, {
      timeout: 30_000,
    });
    await page.getByRole("tab", { name: "Sell" }).click();
    await page.getByLabel(/GR6 input/).fill("1");
    await page.getByRole("button", { name: "Review sell" }).click();
    await page.getByRole("button", { name: "Sign sell" }).click();
    const routerHandoff = page.getByRole("region", { name: "Router handoff" });
    const tradeStatus = routerHandoff.getByRole("status").first();
    await expect(tradeStatus).toContainText(/indexing|indexed|safe|finalized/i, {
      timeout: 30_000,
    });
    await expect(routerHandoff.getByRole("status").last()).toContainText(/approval mined/i, {
      timeout: 30_000,
    });
    const priorTradeText = (await tradeStatus.textContent()) ?? "";
    const priorTradeHash = priorTradeText.match(/0x[0-9a-f]{64}/i)?.[0];
    expect(priorTradeHash).toBeTruthy();
    await page.getByRole("button", { name: "Resume sell" }).click();
    await page.screenshot({
      path: path.join(evidenceDir, `task6-graduated-approval-${testInfo.project.name}.png`),
      fullPage: true,
    });
    const writesBeforeResumedSell = await page.evaluate(
      () => (window as unknown as { __task6WriteRequests: unknown[] }).__task6WriteRequests.length,
    );
    await page.getByRole("button", { name: "Sign sell" }).click();
    await expect
      .poll(
        () =>
          page.evaluate(
            () =>
              (window as unknown as { __task6WriteRequests: unknown[] }).__task6WriteRequests
                .length,
          ),
        { timeout: 10_000 },
      )
      .toBeGreaterThan(writesBeforeResumedSell);
    await expect(tradeStatus).not.toContainText(priorTradeHash!);
    await expect(tradeStatus).toContainText(/indexing|indexed|safe|finalized/i, {
      timeout: 30_000,
    });
  });

  test("wallet page shows realized PnL after curve buy, curve sell and pool sell without reload", async ({
    page,
    context,
  }, testInfo) => {
    // Launch, five transactions and the live refresh together exceed the 30 s default.
    test.setTimeout(120_000);
    // A fresh, unlocked Anvil account per project keeps the expected PnL exact: it starts with no
    // history, and the other scenarios only trade with the first account.
    const trader = (
      testInfo.project.name === "mobile"
        ? "0x9965507D1a55bcC2695C58ba16FB37d819B0A4dc"
        : "0x15d34AAf54267DB7D7c367839AAf71A00a2C6A65"
    ) as `0x${string}`;
    const rpcUrl = process.env.TASK6_ANVIL_RPC_URL!;
    const weth = process.env.TASK6_ANVIL_WETH!.toLowerCase() as `0x${string}`;
    const router = process.env.TASK6_ANVIL_ROUTER as `0x${string}`;
    const api = process.env.TASK6_ANVIL_API_URL!;

    await installWallet(page);
    await page.goto("/create");
    await connectWallet(page);
    await page.getByLabel("Name", { exact: true }).fill("Wallet PnL Task 11");
    await page.getByLabel("Ticker", { exact: true }).fill("WP11");
    await page.getByRole("button", { name: "Review launch" }).click();
    await page.getByRole("button", { name: "Sign launch" }).click();
    const launchStatus = transactionStatus(page).last();
    await expect(launchStatus).toContainText(/indexing|indexed|safe|finalized/i, {
      timeout: 30_000,
    });
    const launchHash = (await launchStatus.textContent())?.match(/0x[0-9a-f]{64}/i)?.[0];
    expect(launchHash).toBeTruthy();
    let tokenAddress = "";
    await expect
      .poll(
        async () => {
          tokenAddress = await canonicalTokenForLaunch(
            page,
            api,
            launchHash!,
            "Wallet PnL Task 11",
          );
          return tokenAddress;
        },
        { timeout: 30_000 },
      )
      .toMatch(/^0x[0-9a-f]{40}$/i);
    const token = tokenAddress as `0x${string}`;
    const tokenResponse = await page.request.get(`${api}/v1/tokens/${tokenAddress}`);
    const curveFromApi = ((await tokenResponse.json()) as { curve?: string }).curve;
    expect(curveFromApi).toMatch(/^0x[0-9a-f]{40}$/i);
    const curve = curveFromApi as `0x${string}`;

    // The wallet page is opened before any trade and is never reloaded or navigated afterwards.
    const walletPage = await context.newPage();
    await walletPage.goto(`/wallet/${trader.toLowerCase()}`);
    const totals = walletPage.getByRole("region", { name: "Wallet totals" });
    // The signed ETH figure only: the USD line is a separate element inside the same cell.
    const realizedText = totals
      .getByText("Realized PnL", { exact: true })
      .locator("xpath=following-sibling::dd[1]")
      .locator(".pnl-value");
    await expect(realizedText).toHaveText("0 ETH", { timeout: 30_000 });

    const publicClient = createPublicClient({ transport: http(rpcUrl) });
    const walletClient = createWalletClient({ transport: http(rpcUrl) });
    const erc20Abi = parseAbi([
      "function approve(address spender, uint256 amount) returns (bool)",
      "function balanceOf(address) view returns (uint256)",
      "event Transfer(address indexed from, address indexed to, uint256 value)",
    ]);
    const tradeAbi = parseAbi([
      "event Trade(address indexed token, address indexed trader, bool isBuy, uint256 ethGross, uint256 ethRefund, uint256 tokenAmount, uint256 protocolFee, uint256 creatorFee, uint256 newEthReserve, uint256 newTokenReserve)",
    ]);
    const swapAbi = parseAbi([
      "event Swap(address indexed sender, uint256 amount0In, uint256 amount1In, uint256 amount0Out, uint256 amount1Out, address indexed to)",
    ]);
    const pairAbi = parseAbi(["function token0() view returns (address)"]);
    const deadline = () => BigInt(Math.floor(Date.now() / 1000) + 3600);
    const write = async (request: {
      address: `0x${string}`;
      abi: readonly unknown[];
      functionName: string;
      args: readonly unknown[];
      value?: bigint;
    }) => {
      const hash = await walletClient.writeContract({
        account: trader,
        chain: null,
        ...request,
      } as never);
      const receipt = await publicClient.waitForTransactionReceipt({ hash });
      expect(receipt.status).toBe("success");
      return receipt;
    };
    const balanceOfTrader = () =>
      publicClient.readContract({
        address: token,
        abi: erc20Abi,
        functionName: "balanceOf",
        args: [trader],
      });

    // One leg per transaction (ruling R1): TokenDelta from the trader's own ERC-20 transfers,
    // the ETH legs from the Trade / Swap event amounts.
    type Leg = { tokenDelta: bigint; ethPaid: bigint; ethReceived: bigint };
    const legs: Leg[] = [];
    const trade = async (request: Parameters<typeof write>[0]) => {
      const receipt = await write(request);
      let tokenDelta = 0n;
      for (const log of parseEventLogs({
        abi: erc20Abi,
        eventName: "Transfer",
        logs: receipt.logs.filter((entry) => entry.address.toLowerCase() === token.toLowerCase()),
      })) {
        if (log.args.to.toLowerCase() === trader.toLowerCase()) tokenDelta += log.args.value;
        if (log.args.from.toLowerCase() === trader.toLowerCase()) tokenDelta -= log.args.value;
      }
      let ethPaid = 0n;
      let ethReceived = 0n;
      for (const log of parseEventLogs({ abi: tradeAbi, logs: receipt.logs })) {
        if (log.args.trader.toLowerCase() !== trader.toLowerCase()) continue;
        if (log.args.isBuy) ethPaid += log.args.ethGross;
        else ethReceived += log.args.ethGross - log.args.protocolFee - log.args.creatorFee;
      }
      for (const log of parseEventLogs({ abi: swapAbi, logs: receipt.logs })) {
        const token0 = await publicClient.readContract({
          address: log.address,
          abi: pairAbi,
          functionName: "token0",
        });
        const wethIsToken0 = token0.toLowerCase() === weth;
        ethPaid += wethIsToken0 ? log.args.amount0In : log.args.amount1In;
        ethReceived += wethIsToken0 ? log.args.amount0Out : log.args.amount1Out;
      }
      legs.push({ tokenDelta, ethPaid, ethReceived });
    };

    // Curve buy, curve sell, then a graduating curve buy.
    const curveBuy = (value: bigint) =>
      trade({
        address: curve,
        abi: browserAbis.curve,
        functionName: "buy",
        args: [trader, trader, 0n, deadline()],
        value,
      });
    await curveBuy(1_000_000_000_000_000_000n);
    const curveSellAmount = (await balanceOfTrader()) / 2n;
    await write({
      address: token,
      abi: erc20Abi,
      functionName: "approve",
      args: [curve, curveSellAmount],
    });
    await trade({
      address: curve,
      abi: browserAbis.curve,
      functionName: "sell",
      args: [curveSellAmount, trader, 0n, deadline()],
    });
    await curveBuy(5_000_000_000_000_000_000n);
    await expect
      .poll(
        async () => {
          const response = await page.request.get(`${api}/v1/tokens/${tokenAddress}`);
          if (!response.ok()) return "";
          return ((await response.json()) as { phase?: string }).phase ?? "";
        },
        { timeout: 30_000 },
      )
      .toBe("graduated");

    // Pool buy and pool sell through the Uniswap v2 router.
    await trade({
      address: router,
      abi: browserAbis.router,
      functionName: "swapExactETHForTokens",
      args: [0n, [weth, token], trader, deadline()],
      value: 10_000_000_000_000_000n,
    });
    const poolSellAmount = (await balanceOfTrader()) / 3n;
    await write({
      address: token,
      abi: erc20Abi,
      functionName: "approve",
      args: [router, poolSellAmount],
    });
    await trade({
      address: router,
      abi: browserAbis.router,
      functionName: "swapExactTokensForETH",
      args: [poolSellAmount, 0n, [token, weth], trader, deadline()],
    });

    // mirrors position.Compute (backend/internal/position/position.go): average-cost accounting,
    // floor division on the removed cost, and the exact-balance shortcut.
    let balance = 0n;
    let remainingCost = 0n;
    let realized = 0n;
    for (const leg of legs) {
      const attributed = leg.ethPaid > 0n || leg.ethReceived > 0n;
      const net = leg.ethPaid - leg.ethReceived;
      if (leg.tokenDelta > 0n) {
        balance += leg.tokenDelta;
        if (attributed) {
          if (net > 0n) remainingCost += net;
          else realized -= net;
        }
      } else if (leg.tokenDelta < 0n) {
        const sold = -leg.tokenDelta;
        expect(sold <= balance).toBe(true);
        const removed = sold === balance ? remainingCost : (remainingCost * sold) / balance;
        balance -= sold;
        remainingCost -= removed;
        if (attributed) realized -= net + removed;
      } else {
        realized -= net;
      }
    }
    expect(legs).toHaveLength(5);
    expect(realized).not.toBe(0n);
    testInfo.annotations.push({
      type: "expected-realized-pnl-wei",
      description: `${realized} from legs ${JSON.stringify(legs, (_, value) => (typeof value === "bigint" ? value.toString() : value))}`,
    });

    // Spec AC4: the API's realized PnL equals the receipt-derived value exactly, in wei.
    await expect
      .poll(
        async () => {
          const response = await page.request.get(`${api}/v1/wallets/${trader.toLowerCase()}`);
          if (!response.ok()) return `HTTP ${response.status()}`;
          return ((await response.json()) as WalletResponse).realized_pnl_wei;
        },
        { timeout: 10_000 },
      )
      .toBe(realized.toString());

    // The same helper the page renders with; there is no page.reload() after the page opened.
    const expectedText = formatSignedEth(realized.toString());
    await expect(realizedText).toHaveText(
      new RegExp(`^${expectedText.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}$`),
      { timeout: 10_000 },
    );
  });

  test("keeps the home page inside the viewport on small phones", async ({ page }) => {
    for (const width of [320, 375]) {
      await page.setViewportSize({ width, height: 800 });
      await page.goto("/");
      await expect(page.getByRole("heading", { level: 1 }).first()).toBeVisible();
      const overflow = await page.evaluate(
        () => document.documentElement.scrollWidth - window.innerWidth,
      );
      expect(overflow, `horizontal overflow at ${width}px`).toBeLessThanOrEqual(0);
    }
  });

  test("lists a traded token in Trending and keeps wallet-less watchlist and USD hidden", async ({
    page,
  }, testInfo) => {
    const tokenName = "Discovery Task 12";
    test.setTimeout(120_000);
    await installWallet(page);
    await page.goto("/create");
    await connectWallet(page);
    await page.getByLabel("Name", { exact: true }).fill(tokenName);
    await page.getByLabel("Ticker", { exact: true }).fill("DS12");
    await page.getByRole("button", { name: "Review launch" }).click();
    await page.getByRole("button", { name: "Sign launch" }).click();
    const launchStatus = transactionStatus(page).last();
    await expect(launchStatus).toContainText(/indexing|indexed|safe|finalized/i, {
      timeout: 30_000,
    });
    const launchHash = (await launchStatus.textContent())?.match(/0x[0-9a-f]{64}/i)?.[0];
    expect(launchHash).toBeTruthy();
    const api = process.env.TASK6_ANVIL_API_URL!;
    let tokenAddress = "";
    await expect
      .poll(
        async () => {
          tokenAddress = await canonicalTokenForLaunch(page, api, launchHash!, tokenName);
          return tokenAddress;
        },
        { timeout: 30_000 },
      )
      .toMatch(/^0x[0-9a-f]{40}$/i);
    await page.goto(`/token/${tokenAddress}`);
    await expect(page.getByRole("heading", { name: tokenName })).toBeVisible({ timeout: 30_000 });
    await page.getByLabel("ETH input").fill("0.001");
    await page.getByRole("button", { name: "Review buy" }).click();
    await page.getByRole("button", { name: "Sign buy" }).click();
    await expect(transactionStatus(page).last()).toContainText(/indexing|indexed|safe|finalized/i, {
      timeout: 30_000,
    });

    // The token detail page shows no USD line or CoinGecko attribution without a price source.
    await expect(page.getByText("Data provided by CoinGecko")).toHaveCount(0);
    await expect(page.getByText(/≈ \$/)).toHaveCount(0);
    await expect(page.getByRole("button", { name: /to watchlist$/ })).toHaveCount(0);
    const detailUrl = page.url();

    const screenshot = async (name: string) => {
      const dir = path.resolve(
        process.cwd(),
        "..",
        ".superpowers",
        "sdd",
        "2026-11-05-p0-market-data-discovery",
        "screens",
      );
      try {
        fs.mkdirSync(dir, { recursive: true });
        await page.screenshot({ path: path.join(dir, name), fullPage: true });
      } catch {
        // Evidence screenshots are best effort and never fail the gate.
      }
    };

    // Explore -> Trending lists the traded token after indexing.
    await page.goto("/explore");
    const views = page.getByRole("navigation", { name: "Explore views" });
    const tokenCard = page.getByRole("link", { name: tokenName });
    await expect(tokenCard.first()).toBeVisible({ timeout: 45_000 });
    await expect(views.getByRole("link", { name: "Trending" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    await expect(page.getByText("Data provided by CoinGecko")).toHaveCount(0);
    await expect(page.getByText(/≈ \$/)).toHaveCount(0);

    // The age filter lives on the ranked views and is reflected in the URL.
    await views.getByRole("link", { name: "New" }).click();
    await page
      .getByRole("group", { name: "Launch age" })
      .getByRole("button", { name: "24h", exact: true })
      .click();
    await expect(page).toHaveURL(/age=24h/);
    await expect(page.getByRole("link", { name: tokenName }).first()).toBeVisible({
      timeout: 30_000,
    });

    // Without Privy sign-in the watchlist reports itself unavailable and shows no stars.
    await views.getByRole("link", { name: "Watchlist" }).click();
    await expect(page.getByRole("heading", { name: "Watchlist unavailable" })).toBeVisible({
      timeout: 15_000,
    });
    // Opening the tab mounts the wallet stack, which provides no sign-in here, so stars go away.
    await views.getByRole("link", { name: "Trending" }).click();
    await expect(page.getByRole("link", { name: tokenName }).first()).toBeVisible();
    await expect(page.getByRole("button", { name: /to watchlist$/ })).toHaveCount(0);

    // Evidence screenshots at the two reference viewports.
    for (const [label, size] of [
      ["375x812", { width: 375, height: 812 }],
      ["1440x900", { width: 1440, height: 900 }],
    ] as const) {
      await page.setViewportSize(size);
      await page.goto("/explore?view=trending");
      await expect(page.getByRole("link", { name: tokenName }).first()).toBeVisible({
        timeout: 30_000,
      });
      await screenshot(`explore-trending-${label}-${testInfo.project.name}.png`);
      await page.goto("/explore?view=watchlist");
      await expect(page.getByRole("heading", { name: "Watchlist unavailable" })).toBeVisible({
        timeout: 15_000,
      });
      await screenshot(`explore-watchlist-${label}-${testInfo.project.name}.png`);
      await page.goto(detailUrl);
      await expect(page.getByRole("heading", { name: tokenName })).toBeVisible({ timeout: 30_000 });
      await screenshot(`token-detail-${label}-${testInfo.project.name}.png`);
    }
  });

  test("a buy appears in the Explore strip and Recent trades without reload within 3 seconds of commit", async ({
    page,
    context,
  }) => {
    test.setTimeout(120_000);
    const tokenName = "Live Feed Task 8";
    await installWallet(page);
    await page.goto("/create");
    await connectWallet(page);
    await page.getByLabel("Name", { exact: true }).fill(tokenName);
    await page.getByLabel("Ticker", { exact: true }).fill("LF8");
    await page.getByRole("button", { name: "Review launch" }).click();
    await page.getByRole("button", { name: "Sign launch" }).click();
    const launchStatus = transactionStatus(page).last();
    await expect(launchStatus).toContainText(/indexing|indexed|safe|finalized/i, {
      timeout: 30_000,
    });
    const launchHash = (await launchStatus.textContent())?.match(/0x[0-9a-f]{64}/i)?.[0];
    expect(launchHash).toBeTruthy();
    const api = process.env.TASK6_ANVIL_API_URL!;
    let tokenAddress = "";
    await expect
      .poll(
        async () => {
          tokenAddress = await canonicalTokenForLaunch(page, api, launchHash!, tokenName);
          return tokenAddress;
        },
        { timeout: 30_000 },
      )
      .toMatch(/^0x[0-9a-f]{40}$/i);

    // A second tab stays on Explore and must never reload while the buy happens.
    const explore = await context.newPage();
    await explore.goto("/");
    await explore.evaluate(() => {
      (window as unknown as { __noReloadMarker: boolean }).__noReloadMarker = true;
    });

    await page.goto(`/token/${tokenAddress}`);
    await expect(page.getByRole("heading", { name: tokenName })).toBeVisible({ timeout: 30_000 });
    await page.getByRole("tab", { name: "Recent trades" }).click();
    await page.getByLabel("ETH input").fill("0.001");
    await page.getByRole("button", { name: "Review buy" }).click();
    await page.getByRole("button", { name: "Sign buy" }).click();
    // The wallet stub forwards eth_sendTransaction to Anvil, which commits on receipt of the
    // request; start the clock when the transaction hash exists.
    await expect
      .poll(
        () =>
          page.evaluate(
            () =>
              (window as unknown as { __task6WriteRequests: unknown[] }).__task6WriteRequests
                .length,
          ),
        { timeout: 30_000 },
      )
      .toBeGreaterThan(0);
    const committedAt = Date.now();

    const strip = explore.getByRole("region", { name: "Live activity" });
    await expect(
      strip.getByRole("listitem").filter({ hasText: "LF8" }).filter({ hasText: "buy" }).first(),
    ).toBeVisible({
      timeout: 3_000,
    });
    const trades = page.getByRole("table", { name: "Recent trades" });
    await expect(trades.getByText("buy").first()).toBeVisible({ timeout: 3_000 });
    const elapsed = Date.now() - committedAt;
    expect(elapsed, "live delivery after commit").toBeLessThanOrEqual(3_500);
    expect(
      await explore.evaluate(() => (window as unknown as Record<string, unknown>).__noReloadMarker),
    ).toBe(true);
    await explore.close();
  });

  test("regresses to unavailable after the canonical launch disappears in an Anvil reorg", async ({
    page,
  }) => {
    await installWallet(page);
    const snapshotResponse = await page.request.post("/e2e/rpc", {
      data: { jsonrpc: "2.0", id: 1, method: "evm_snapshot", params: [] },
    });
    const snapshot = ((await snapshotResponse.json()) as { result: string }).result;
    await page.goto("/create");
    await connectWallet(page);
    await page.getByLabel("Name", { exact: true }).fill("Reorg Task 6");
    await page.getByLabel("Ticker", { exact: true }).fill("RG6");
    await page.getByRole("button", { name: "Review launch" }).click();
    await page.getByRole("button", { name: "Sign launch" }).click();
    const status = transactionStatus(page).last();
    await expect(status).toContainText(/indexing|indexed|safe|finalized/i, { timeout: 30_000 });
    const hash = (await status.textContent())?.match(/0x[0-9a-f]{64}/i)?.[0];
    expect(hash).toBeTruthy();
    await expect
      .poll(
        async () =>
          (
            await page.request.get(`${process.env.TASK6_ANVIL_API_URL}/v1/transactions/${hash}`)
          ).status(),
        { timeout: 30_000 },
      )
      .toBe(200);
    const reverted = await page.request.post("/e2e/rpc", {
      data: { jsonrpc: "2.0", id: 2, method: "evm_revert", params: [snapshot] },
    });
    expect(((await reverted.json()) as { result: boolean }).result).toBe(true);
    await page.request.post("/e2e/rpc", {
      data: {
        jsonrpc: "2.0",
        id: 3,
        method: "eth_sendTransaction",
        params: [{ from: sender, to: sender, value: "0x1" }],
      },
    });
    await expect
      .poll(
        async () =>
          (
            await page.request.get(`${process.env.TASK6_ANVIL_API_URL}/v1/transactions/${hash}`)
          ).status(),
        { timeout: 30_000 },
      )
      .toBe(404);
    await expect(status).toContainText(/indexing/i, { timeout: 30_000 });
  });
});

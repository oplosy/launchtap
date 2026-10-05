import { expect, test, type Page } from "@playwright/test";
import {
  createPublicClient,
  decodeFunctionData,
  encodeFunctionData,
  http,
  parseAbi,
  parseEventLogs,
} from "viem";
import fs from "node:fs";
import path from "node:path";
import { browserAbis } from "../src/contracts/generated";

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
    await page.getByLabel("Name").fill("Browser Task 6");
    await page.getByLabel("Ticker").fill("B6");
    await page.getByRole("button", { name: "Review launch" }).click();
    await expect(page.getByRole("heading", { name: "Confirm launch" })).toBeVisible();
    const evidenceDir = path.resolve(process.cwd(), "..", ".impeccable", "review");
    fs.mkdirSync(evidenceDir, { recursive: true });
    await page.screenshot({
      path: path.join(evidenceDir, `task6-create-confirm-${testInfo.project.name}.png`),
      fullPage: true,
    });
    await page.getByRole("button", { name: "Sign launch" }).click();
    const status = page.getByRole("status");
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
    await page.getByLabel("Name").fill("Browser Trade Task 6");
    await page.getByLabel("Ticker").fill("BT6");
    await page.getByRole("button", { name: "Review launch" }).click();
    await page.getByRole("button", { name: "Sign launch" }).click();
    const launchStatus = page.getByRole("status").last();
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
    await expect(page.getByRole("status").last()).toContainText(
      /indexing|indexed|safe|finalized/i,
      { timeout: 30_000 },
    );
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
    await page.getByLabel("Name").fill("Wrong Chain Task 6");
    await page.getByLabel("Ticker").fill("WC6");
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
    await page.getByLabel("Name").fill("Rejected Task 6");
    await page.getByLabel("Ticker").fill("R6");
    await page.getByRole("button", { name: "Review launch" }).click();
    await page.evaluate(() => {
      (window as unknown as { __task6RejectNext: boolean }).__task6RejectNext = true;
    });
    await page.getByRole("button", { name: "Sign launch" }).click();
    await expect(page.getByRole("status")).toContainText(/rejected-signature/i, {
      timeout: 10_000,
    });
  });

  test("surfaces a decoded contract cap revert from the real launch controls", async ({ page }) => {
    await installWallet(page);
    await page.goto("/create");
    await connectWallet(page);
    await page.getByLabel("Name").fill("Revert Task 6");
    await page.getByLabel("Ticker").fill("RV6");
    await page.getByLabel("Developer buy (ETH, optional)").fill("0.02");
    await page.getByRole("button", { name: "Review launch" }).click();
    await page.getByRole("button", { name: "Sign launch" }).click();
    await expect(page.locator(".transaction-error")).toContainText(
      /developer buy exceeds the contract cap/i,
      { timeout: 15_000 },
    );
    await expect(page.getByRole("status")).toContainText(/simulation-reverted/i);
  });

  test("uses the reviewed graduated router for wrong-chain, buy, approval, and sell", async ({
    page,
  }, testInfo) => {
    await installWallet(page);
    await page.goto("/create");
    await connectWallet(page);
    await page.getByLabel("Name").fill("Graduated Task 6");
    await page.getByLabel("Ticker").fill("GR6");
    await page.getByRole("button", { name: "Review launch" }).click();
    await page.getByRole("button", { name: "Sign launch" }).click();
    const launchStatus = page.getByRole("status").last();
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
    await expect(page.getByRole("status").last()).toContainText(
      /indexing|indexed|safe|finalized/i,
      { timeout: 45_000 },
    );
    const graduationHash = (await page.getByRole("status").last().textContent())?.match(
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
    await expect(page.getByRole("status").last()).toContainText(
      /indexing|indexed|safe|finalized/i,
      { timeout: 30_000 },
    );
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
    await page.getByLabel("Name").fill("Reorg Task 6");
    await page.getByLabel("Ticker").fill("RG6");
    await page.getByRole("button", { name: "Review launch" }).click();
    await page.getByRole("button", { name: "Sign launch" }).click();
    const status = page.getByRole("status").last();
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

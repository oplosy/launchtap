import { expect, test, type Page } from "@playwright/test";
import {
  createPublicClient,
  createTestClient,
  http,
  parseAbi,
  parseEventLogs,
  type Address,
  type Hash,
} from "viem";
import { browserAbis } from "../src/contracts/generated";
import { formatBaseUnits } from "../src/amounts";

// Plan v2-C Task 9: the engine-2 lifecycle driven through the browser against a local v2
// stack (Uniswap V4 + Universal Router 2.1.2 built from the mainnet source revision), the
// backend indexer and the API. Run through `npm run test:anvil-v2`.

const creator = "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266" as const;
// Anvil account 5: never a protocol role in the local deployment.
const nextRecipient = "0x9965507D1a55bcC2695C58ba16FB37d819B0A4dc" as const;

const env = {
  rpc: process.env.TASK6_ANVIL_RPC_URL ?? "",
  api: process.env.TASK6_ANVIL_API_URL ?? "",
  factory: process.env.V2_ANVIL_FACTORY ?? "",
  locker: process.env.V2_ANVIL_LOCKER ?? "",
  hook: process.env.V2_ANVIL_HOOK ?? "",
  router: process.env.V2_ANVIL_UNIVERSAL_ROUTER ?? "",
};
const configured = Object.values(env).every(Boolean);

const events = parseAbi([
  "event Trade(address indexed token, address indexed trader, bool isBuy, uint256 ethGross, uint256 ethRefund, uint256 tokenAmount, uint256 protocolFee, uint256 creatorFee, uint256 newEthReserve, uint256 newTokenReserve, uint256 snipeTax)",
  "event Graduated(address indexed token, bytes32 indexed poolId, uint256 positionTokenId, uint256 ethToPool, uint256 tokensToPool, uint128 liquidity, uint160 sqrtPriceX96, uint256 ethRemainder, uint256 tokenRemainder)",
  "event CreatorFeesClaimed(address indexed token, address indexed account, uint256 amount)",
  "event LpFeesCollected(address indexed token, uint256 ethAmount, uint256 tokenAmount)",
  "event LpFeesClaimed(address indexed token, address indexed recipient, uint256 ethAmount, uint256 tokenAmount)",
  "event FeeRecipientAccepted(address indexed token, address indexed previous, address indexed next)",
]);
const factoryReads = parseAbi(["function curveOfToken(address) view returns (address)"]);

const eth = (value: bigint) => `${formatBaseUnits(value, 18, 6)} ETH`;

const sentByPage = new WeakMap<Page, string[]>();

/**
 * An injected EIP-1193 wallet for one unlocked Anvil account. Every sent hash is recorded on
 * the test side, so the history survives reloads.
 */
async function installWallet(page: Page, account: Address) {
  const sent: string[] = [];
  sentByPage.set(page, sent);
  await page.exposeFunction("__v2RecordHash", (hash: string) => {
    sent.push(hash);
  });
  await page.addInitScript((account) => {
    const record = (window as unknown as { __v2RecordHash: (hash: string) => Promise<void> })
      .__v2RecordHash;
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
          if (method === "wallet_switchEthereumChain") return null;
          if (method === "eth_chainId") return "0x7a69";
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
          if (method === "eth_sendTransaction") await record(String(body.result));
          return body.result;
        },
      },
    });
  }, account);
}

async function connectWallet(page: Page) {
  await page.getByRole("button", { name: "Connect wallet" }).last().click();
  const dialog = page.getByRole("dialog", { name: "Choose a wallet" });
  await expect(dialog).toBeVisible();
  await dialog.getByRole("button", { name: "Browser wallet" }).click();
  await expect(page.getByRole("button", { name: "Connect wallet", exact: true })).toHaveCount(0, {
    timeout: 15_000,
  });
}

async function sentHashes(page: Page) {
  return (sentByPage.get(page) ?? []).slice();
}

/** The newest hash this page's wallet sent, once it has sent more than `before`. */
async function nextSentHash(page: Page, before: number): Promise<Hash> {
  await expect
    .poll(async () => (await sentHashes(page)).length, { timeout: 30_000 })
    .toBeGreaterThan(before);
  return (await sentHashes(page)).at(-1) as Hash;
}

test.describe("Anvil v2 lifecycle gate", () => {
  test.beforeAll(() => {
    if (process.env.V2_ANVIL_REQUIRED === "1" && !configured)
      throw new Error("The Anvil v2 gate requires the RPC, API, factory, locker, hook and router");
  });
  test.beforeEach(({}, testInfo) => {
    if (!configured) testInfo.skip(true, "Run through the Anvil v2 gate (npm run test:anvil-v2)");
  });

  test("launch, taxed and untaxed buys, graduation, V4 trades, claims and a recipient change", async ({
    page,
    browser,
  }) => {
    test.setTimeout(8 * 60_000);
    const client = createPublicClient({ transport: http(env.rpc) });
    const anvil = createTestClient({ mode: "anvil", transport: http(env.rpc) });
    const receiptOf = async (hash: Hash) => {
      const receipt = await client.waitForTransactionReceipt({ hash });
      expect(receipt.status, `transaction ${hash} reverted`).toBe("success");
      return receipt;
    };
    await installWallet(page, creator);

    // 1. Launch v2 with a developer buy.
    let token = "" as Address;
    let curve = "" as Address;
    await test.step("launch with a developer buy", async () => {
      await page.goto("/create");
      await connectWallet(page);
      const preview = page.getByRole("complementary", { name: "Token preview" });
      await expect(preview).toContainText("50%, decaying to 0 over 1 min");
      await page.getByLabel("Name", { exact: true }).fill("Lifecycle V2");
      await page.getByLabel("Ticker", { exact: true }).fill("LV2");
      await page.getByLabel("Developer buy", { exact: true }).fill("0.01");
      await page.getByRole("button", { name: "Review launch" }).click();
      const before = (await sentHashes(page)).length;
      await page.getByRole("button", { name: "Sign launch" }).click();
      const receipt = await receiptOf(await nextSentHash(page, before));
      const [launch] = parseEventLogs({
        abi: browserAbis.factoryV2,
        eventName: "TokenLaunched",
        logs: receipt.logs,
      });
      expect(launch, "launch receipt has no TokenLaunched event").toBeTruthy();
      token = launch!.args.token as Address;
      curve = launch!.args.curve as Address;
      expect(Number(launch!.args.engineVersion)).toBe(2);
      expect(
        await client.readContract({
          address: env.factory as Address,
          abi: factoryReads,
          functionName: "curveOfToken",
          args: [token],
        }),
      ).toBe(curve);
      const [developerBuy] = parseEventLogs({
        abi: events,
        eventName: "Trade",
        logs: receipt.logs,
      });
      expect(developerBuy?.args.snipeTax).toBe(0n);
      await expect
        .poll(async () => (await page.request.get(`${env.api}/v1/tokens/${token}`)).status(), {
          timeout: 30_000,
        })
        .toBe(200);
    });

    // 2. A taxed buy inside the 60 s anti-snipe window: the tax line and countdown are shown.
    await test.step("taxed buy inside the anti-snipe window", async () => {
      await page.goto(`/token/${token}`);
      await expect(page.getByRole("heading", { name: "Trade LV2" })).toBeVisible({
        timeout: 30_000,
      });
      await page.getByLabel("ETH input").fill("0.1");
      await page.getByRole("button", { name: "Review buy" }).click();
      const taxLine = page.locator(".quote-summary").getByText(/Anti-snipe tax/);
      await expect(taxLine).toBeVisible();
      await expect(taxLine).toContainText(/ends in/);
      const minimum = await page
        .locator(".quote-summary")
        .getByText(/Minimum output/)
        .locator("strong")
        .textContent();
      const before = (await sentHashes(page)).length;
      await page.getByRole("button", { name: "Sign buy" }).click();
      const receipt = await receiptOf(await nextSentHash(page, before));
      const [trade] = parseEventLogs({ abi: events, eventName: "Trade", logs: receipt.logs });
      expect(trade!.args.snipeTax).toBeGreaterThan(0n);
      // Within the stated slippage: never fewer tokens than the reviewed minimum.
      expect(Number(formatBaseUnits(trade!.args.tokenAmount, 18, 6))).toBeGreaterThanOrEqual(
        Number(minimum),
      );
    });

    // 3. An untaxed buy after the window (chain time advanced past it).
    await test.step("untaxed buy after the window", async () => {
      await anvil.increaseTime({ seconds: 61 });
      await anvil.mine({ blocks: 1 });
      await page.reload();
      await page.getByLabel("ETH input").fill("0.1");
      await page.getByRole("button", { name: "Review buy" }).click();
      await expect(page.locator(".quote-summary")).toBeVisible();
      await expect(page.locator(".quote-summary").getByText(/Anti-snipe tax/)).toHaveCount(0);
      const before = (await sentHashes(page)).length;
      await page.getByRole("button", { name: "Sign buy" }).click();
      const receipt = await receiptOf(await nextSentHash(page, before));
      const [trade] = parseEventLogs({ abi: events, eventName: "Trade", logs: receipt.logs });
      expect(trade!.args.snipeTax).toBe(0n);
    });

    // 4. The final buy graduates the token into its guarded V4 pool.
    let poolId = "" as Hash;
    await test.step("graduating buy", async () => {
      await page.reload();
      await page.getByLabel("ETH input").fill("10");
      await page.getByRole("button", { name: "Review buy" }).click();
      const before = (await sentHashes(page)).length;
      await page.getByRole("button", { name: "Sign buy" }).click();
      const receipt = await receiptOf(await nextSentHash(page, before));
      const [graduated] = parseEventLogs({
        abi: events,
        eventName: "Graduated",
        logs: receipt.logs,
      });
      expect(graduated, "graduating buy emitted no Graduated event").toBeTruthy();
      poolId = graduated!.args.poolId;
      await expect
        .poll(
          async () => {
            const response = await page.request.get(`${env.api}/v1/tokens/${token}`);
            if (!response.ok()) return "";
            const body = (await response.json()) as { v2?: { pool?: { pool_id?: string } } };
            return body.v2?.pool?.pool_id?.toLowerCase() ?? "";
          },
          { timeout: 30_000 },
        )
        .toBe(poolId.toLowerCase());
      await page.reload();
      await expect(page.getByRole("heading", { name: "Router handoff" })).toBeVisible({
        timeout: 30_000,
      });
      // Plan v2-C D5: the panel renders only when the locally computed pool id equals the
      // indexed one, and it names that pool.
      await expect(page.locator(".graduated-swap .transaction-risk")).toContainText(poolId);
    });

    // 5. A V4 buy, then a V4 sell through the exact ERC-20 → Permit2 → router allowances.
    await test.step("V4 buy and Permit2 sell through the Universal Router", async () => {
      await page.getByLabel("ETH input").fill("0.05");
      await page.getByRole("button", { name: "Review buy" }).click();
      let before = (await sentHashes(page)).length;
      await page.getByRole("button", { name: "Sign buy" }).click();
      const buy = await receiptOf(await nextSentHash(page, before));
      expect(buy.to?.toLowerCase()).toBe(env.router.toLowerCase());

      await page.getByRole("tab", { name: "Sell", exact: true }).click();
      await page.getByLabel("LV2 input").fill("1000");
      await page.getByRole("button", { name: "Review sell" }).click();
      before = (await sentHashes(page)).length;
      await page.getByRole("button", { name: "Sign sell" }).click();
      // The exact ERC-20 and Permit2 allowances are mined; the swap is then reviewed again.
      await expect(page.getByRole("button", { name: "Resume sell" })).toBeVisible({
        timeout: 30_000,
      });
      const approvals = (await sentHashes(page)).slice(before);
      expect(approvals.length).toBeGreaterThanOrEqual(2);
      for (const hash of approvals) await receiptOf(hash as Hash);
      await page.getByRole("button", { name: "Resume sell" }).click();
      before = (await sentHashes(page)).length;
      await page.getByRole("button", { name: "Sign sell" }).click();
      const sell = await receiptOf(await nextSentHash(page, before));
      expect(sell.to?.toLowerCase()).toBe(env.router.toLowerCase());
    });

    // 6. A locker claim and a curve creator fee claim, each equal to the on-chain view.
    await test.step("LP fee locker claim and curve creator fee claim", async () => {
      await page.reload();
      const creatorPanel = page.locator(".v2-creator");
      await expect(creatorPanel).toBeVisible({ timeout: 30_000 });
      let before = (await sentHashes(page)).length;
      await creatorPanel.getByRole("button", { name: "Collect and claim LP fees" }).click();
      const lpClaim = await receiptOf(await nextSentHash(page, before));
      const [collected] = parseEventLogs({
        abi: events,
        eventName: "LpFeesCollected",
        logs: lpClaim.logs,
      });
      const [claimed] = parseEventLogs({
        abi: events,
        eventName: "LpFeesClaimed",
        logs: lpClaim.logs,
      });
      const shareBps = BigInt(
        await client.readContract({
          address: curve,
          abi: browserAbis.curveV2,
          functionName: "lpCreatorShareBps",
        }),
      );
      expect(claimed!.args.recipient).toBe(creator);
      expect(claimed!.args.ethAmount).toBeGreaterThan(0n);
      expect(claimed!.args.ethAmount).toBe((collected!.args.ethAmount * shareBps) / 10_000n);
      expect(claimed!.args.tokenAmount).toBe((collected!.args.tokenAmount * shareBps) / 10_000n);

      const owed = (await client.readContract({
        address: curve,
        abi: browserAbis.curveV2,
        functionName: "creatorFeesOwed",
        args: [creator],
      })) as bigint;
      expect(owed).toBeGreaterThan(0n);
      await expect(page.getByText(`Claimable creator fees: ${eth(owed)}`)).toBeVisible();
      before = (await sentHashes(page)).length;
      await page.getByRole("button", { name: "Claim creator fees" }).click();
      const curveClaim = await receiptOf(await nextSentHash(page, before));
      const [creatorClaim] = parseEventLogs({
        abi: events,
        eventName: "CreatorFeesClaimed",
        logs: curveClaim.logs,
      });
      expect(creatorClaim!.args.amount).toBe(owed);
    });

    // 7. Propose a recipient, accept from the second account, then that account claims.
    await test.step("two-step fee recipient change and a claim by the new recipient", async () => {
      await page.reload();
      const creatorPanel = page.locator(".v2-creator");
      await creatorPanel.getByLabel("New fee recipient").fill(nextRecipient);
      let before = (await sentHashes(page)).length;
      await creatorPanel.getByRole("button", { name: "Propose recipient" }).click();
      await receiptOf(await nextSentHash(page, before));
      await expect(creatorPanel).toContainText(nextRecipient, { timeout: 15_000 });

      const context = await browser.newContext({ baseURL: page.url() });
      const recipientPage = await context.newPage();
      try {
        await installWallet(recipientPage, nextRecipient);
        await recipientPage.goto(`/token/${token}`);
        await connectWallet(recipientPage);
        const recipientPanel = recipientPage.locator(".v2-creator");
        before = (await sentHashes(recipientPage)).length;
        await recipientPanel.getByRole("button", { name: "Accept fee recipient role" }).click();
        const accept = await receiptOf(await nextSentHash(recipientPage, before));
        const [accepted] = parseEventLogs({
          abi: events,
          eventName: "FeeRecipientAccepted",
          logs: accept.logs,
        });
        expect(accepted!.args.next).toBe(nextRecipient);
        expect(
          await client.readContract({
            address: curve,
            abi: browserAbis.curveV2,
            functionName: "feeRecipient",
          }),
        ).toBe(nextRecipient);

        // New pool fees accrue after the change; the creator's buy produces them.
        await page.reload();
        await page.getByLabel("ETH input").fill("0.05");
        await page.getByRole("button", { name: "Review buy" }).click();
        before = (await sentHashes(page)).length;
        await page.getByRole("button", { name: "Sign buy" }).click();
        await receiptOf(await nextSentHash(page, before));

        await recipientPage.reload();
        await expect(recipientPage.locator(".v2-creator")).toContainText(
          `Fee recipient${nextRecipient}`,
          { timeout: 15_000 },
        );
        before = (await sentHashes(recipientPage)).length;
        await recipientPage
          .locator(".v2-creator")
          .getByRole("button", { name: "Collect and claim LP fees" })
          .click();
        const claim = await receiptOf(await nextSentHash(recipientPage, before));
        const [claimed] = parseEventLogs({
          abi: events,
          eventName: "LpFeesClaimed",
          logs: claim.logs,
        });
        expect(claimed!.args.recipient).toBe(nextRecipient);
        expect(claimed!.args.ethAmount).toBeGreaterThan(0n);
        for (const hash of await sentHashes(recipientPage)) await receiptOf(hash as Hash);
      } finally {
        await context.close();
      }
    });

    // No transaction the browser sent reverted.
    for (const hash of await sentHashes(page)) await receiptOf(hash as Hash);
  });
});

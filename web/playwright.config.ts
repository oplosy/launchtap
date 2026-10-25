import fs from "node:fs";
import { defineConfig, devices } from "@playwright/test";

const configuredExecutablePath = process.env.PLAYWRIGHT_EXECUTABLE_PATH?.trim();
const windowsChromePath =
  process.platform === "win32" ? "C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe" : "";
const windowsChromeFallback =
  windowsChromePath && fs.existsSync(windowsChromePath) ? windowsChromePath : undefined;
const browserExecutablePath = configuredExecutablePath || windowsChromeFallback;
const desktopBrowserOptions = browserExecutablePath
  ? { launchOptions: { executablePath: browserExecutablePath } }
  : {};

const webPort = Number(process.env.TASK6_WEB_PORT ?? "3417");
const fixtureFactory = process.env.TASK6_ANVIL_FACTORY ?? "";
const fixtureWeth = process.env.TASK6_ANVIL_WETH ?? "";
const fixtureRouter = process.env.TASK6_ANVIL_ROUTER ?? "";
const fixtureUniswapFactory = process.env.TASK6_ANVIL_UNISWAP_FACTORY ?? "";
const fixtureRpc = process.env.TASK6_ANVIL_RPC_URL ?? "";
const fixtureApi = process.env.TASK6_ANVIL_API_URL ?? "";

export default defineConfig({
  testDir: "./e2e",
  fullyParallel: true,
  workers: 1,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 2 : 0,
  reporter: "line",
  use: { baseURL: `http://127.0.0.1:${webPort}`, trace: "on-first-retry" },
  webServer: {
    // Build with the E2E-only fixture flag, then exercise the production server and CSP.
    command: "npm run build && npm run start",
    env: {
      NEXT_PUBLIC_E2E_FIXTURE: "1",
      TASK6_ANVIL_FACTORY: fixtureFactory,
      TASK6_ANVIL_WETH: fixtureWeth,
      TASK6_ANVIL_ROUTER: fixtureRouter,
      TASK6_ANVIL_UNISWAP_FACTORY: fixtureUniswapFactory,
      TASK6_ANVIL_RPC_URL: fixtureRpc,
      TASK6_ANVIL_API_URL: fixtureApi,
      NEXT_PUBLIC_TASK6_ANVIL_FACTORY: fixtureFactory,
      NEXT_PUBLIC_TASK6_ANVIL_WETH: fixtureWeth,
      NEXT_PUBLIC_TASK6_ANVIL_ROUTER: fixtureRouter,
      NEXT_PUBLIC_TASK6_ANVIL_UNISWAP_FACTORY: fixtureUniswapFactory,
      NEXT_PUBLIC_TASK6_ANVIL_RPC_URL: fixtureRpc,
      NEXT_PUBLIC_TASK6_ANVIL_API_URL: fixtureApi,
      NEXT_PUBLIC_TASK6_ANVIL_WEB_URL: `http://127.0.0.1:${webPort}`,
      PORT: String(webPort),
    },
    url: `http://127.0.0.1:${webPort}`,
    reuseExistingServer: false,
  },
  projects: [
    {
      name: "small-laptop",
      use: {
        ...devices["Desktop Chrome"],
        ...desktopBrowserOptions,
      },
    },
    {
      name: "mobile",
      use: {
        ...devices["Desktop Chrome"],
        viewport: { width: 360, height: 800 },
        ...desktopBrowserOptions,
      },
    },
    {
      name: "reduced-motion",
      use: {
        ...devices["Desktop Chrome"],
        reducedMotion: "reduce",
        ...desktopBrowserOptions,
      } as never,
    },
  ],
});

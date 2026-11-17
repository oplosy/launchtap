import { describe, expect, it, vi } from "vitest";
import {
  encodeAbiParameters,
  encodeEventTopics,
  type Abi,
  type Address,
  type TransactionReceipt,
} from "viem";

type Log = TransactionReceipt["logs"][number];
import { browserAbis } from "@/contracts/generated";
import { ApiProblem } from "@/api/problems";
import {
  AttachError,
  attachLaunchMetadata,
  hasLaunchExtras,
  launchedToken,
  type LaunchExtras,
} from "./launch-metadata";

const abi = browserAbis.factory as Abi;
const FACTORY = "0x1111111111111111111111111111111111111111" as Address;
const OTHER = "0x9999999999999999999999999999999999999999" as Address;
const CREATOR = "0x2222222222222222222222222222222222222222" as Address;
const TOKEN = "0x3333333333333333333333333333333333333333" as Address;
const CURVE = "0x4444444444444444444444444444444444444444" as Address;
const AUTH = { accessToken: "access", identityToken: "identity" };

function launchLog(emitter: Address, creator: Address): Log {
  const topics = encodeEventTopics({
    abi,
    eventName: "TokenLaunched",
    args: { token: TOKEN, curve: CURVE, creator },
  } as never);
  const data = encodeAbiParameters(
    [
      { type: "address" },
      { type: "address" },
      { type: "address" },
      { type: "uint16" },
      { type: "string" },
      { type: "string" },
      { type: "uint256" },
      { type: "uint256" },
      { type: "uint256" },
      { type: "uint256" },
      { type: "uint256" },
      { type: "uint256" },
      { type: "uint256" },
      { type: "uint16" },
      { type: "uint16" },
    ],
    [OTHER, OTHER, OTHER, 1, "Name", "SYM", 1n, 1n, 1n, 1n, 1n, 1n, 1n, 100, 5000],
  );
  return { address: emitter, topics, data } as unknown as Log;
}

const extras = (overrides: Partial<LaunchExtras> = {}): LaunchExtras => ({
  description: "About",
  xUrl: "https://x.com/a",
  telegramUrl: "",
  file: new File([new Uint8Array([137, 80, 78, 71])], "a.png", { type: "image/png" }),
  ...overrides,
});

function client() {
  return {
    getToken: vi.fn().mockResolvedValue({}),
    getTokenMetadata: vi.fn(),
    updateTokenMetadata: vi.fn().mockResolvedValue({ body: { revision: 1 }, etag: '"1"' }),
    updateTokenImage: vi.fn().mockResolvedValue({ body: { revision: 1 }, etag: '"1"' }),
  };
}

describe("hasLaunchExtras", () => {
  it("is false for an empty form and true for any detail", () => {
    const none = { description: " ", xUrl: "", telegramUrl: "", file: null };
    expect(hasLaunchExtras(none)).toBe(false);
    expect(hasLaunchExtras({ ...none, telegramUrl: "https://t.me/a" })).toBe(true);
    expect(hasLaunchExtras(extras({ description: "", xUrl: "" }))).toBe(true);
  });
});

describe("launchedToken", () => {
  it("reads the token from the factory's log for the signing creator", () => {
    const receipt = { logs: [launchLog(FACTORY, CREATOR)] };
    expect(launchedToken(receipt, abi, FACTORY, CREATOR)?.toLowerCase()).toBe(TOKEN);
  });

  it("ignores a log from another contract or for another creator", () => {
    expect(launchedToken({ logs: [launchLog(OTHER, CREATOR)] }, abi, FACTORY, CREATOR)).toBeNull();
    expect(launchedToken({ logs: [launchLog(FACTORY, OTHER)] }, abi, FACTORY, CREATOR)).toBeNull();
  });
});

describe("attachLaunchMetadata", () => {
  it("waits for the index, then writes metadata and the image at revision 0", async () => {
    const api = client();
    api.getToken.mockRejectedValueOnce(new ApiProblem(404, "unknown")).mockResolvedValueOnce({});
    const sleep = vi.fn().mockResolvedValue(undefined);
    const steps: string[] = [];
    await attachLaunchMetadata({
      client: api as never,
      token: TOKEN,
      extras: extras(),
      getAuth: async () => AUTH,
      sleep,
      onStep: (step) => steps.push(step),
    });
    expect(steps).toEqual(["index", "metadata", "image"]);
    expect(sleep).toHaveBeenCalledTimes(1);
    expect(api.updateTokenMetadata).toHaveBeenCalledWith(
      TOKEN,
      { description: "About", x_url: "https://x.com/a", telegram_url: "" },
      0,
      null,
      AUTH,
    );
    expect(api.updateTokenImage).toHaveBeenCalledWith(TOKEN, expect.any(File), 0, AUTH);
    expect(api.updateTokenMetadata.mock.invocationCallOrder[0]).toBeLessThan(
      api.updateTokenImage.mock.invocationCallOrder[0]!,
    );
  });

  it("skips the image write when no file was chosen", async () => {
    const api = client();
    await attachLaunchMetadata({
      client: api as never,
      token: TOKEN,
      extras: extras({ file: null }),
      getAuth: async () => AUTH,
    });
    expect(api.updateTokenImage).not.toHaveBeenCalled();
  });

  it("gives up on the index after the bound and reports the index step", async () => {
    const api = client();
    api.getToken.mockRejectedValue(new ApiProblem(404, "unknown"));
    const run = attachLaunchMetadata({
      client: api as never,
      token: TOKEN,
      extras: extras(),
      getAuth: async () => AUTH,
      pollMs: 10,
      maxWaitMs: 30,
      sleep: async () => undefined,
    });
    await expect(run).rejects.toMatchObject({ step: "index" });
    expect(api.getToken).toHaveBeenCalledTimes(4);
    expect(api.updateTokenMetadata).not.toHaveBeenCalled();
  });

  it("does not retry a non-404 index read", async () => {
    const api = client();
    api.getToken.mockRejectedValue(new ApiProblem(503, "service_unavailable"));
    await expect(
      attachLaunchMetadata({
        client: api as never,
        token: TOKEN,
        extras: extras(),
        getAuth: async () => AUTH,
        sleep: async () => undefined,
      }),
    ).rejects.toMatchObject({ step: "index" });
    expect(api.getToken).toHaveBeenCalledTimes(1);
  });

  it("reports the image step and resumes there without rewriting metadata", async () => {
    const api = client();
    api.updateTokenImage.mockRejectedValueOnce(new ApiProblem(503, "service_unavailable"));
    const options = {
      client: api as never,
      token: TOKEN,
      extras: extras(),
      getAuth: async () => AUTH,
    };
    const failure = await attachLaunchMetadata(options).catch((cause: unknown) => cause);
    expect(failure).toBeInstanceOf(AttachError);
    expect((failure as AttachError).step).toBe("image");
    await attachLaunchMetadata({ ...options, from: "image" });
    expect(api.updateTokenMetadata).toHaveBeenCalledTimes(1);
    expect(api.updateTokenImage).toHaveBeenCalledTimes(2);
  });

  it("explains a refused creator check", async () => {
    const api = client();
    api.updateTokenMetadata.mockRejectedValue(
      new ApiProblem(403, "forbidden", null, "creator_required"),
    );
    await expect(
      attachLaunchMetadata({
        client: api as never,
        token: TOKEN,
        extras: extras(),
        getAuth: async () => AUTH,
      }),
    ).rejects.toMatchObject({ step: "metadata", message: expect.stringMatching(/does not own/) });
  });

  it("fails the write step without a session", async () => {
    const api = client();
    await expect(
      attachLaunchMetadata({
        client: api as never,
        token: TOKEN,
        extras: extras(),
        getAuth: async () => null,
      }),
    ).rejects.toMatchObject({ step: "metadata" });
    expect(api.updateTokenMetadata).not.toHaveBeenCalled();
  });

  it("accepts a revision conflict only when the stored values are the ones sent", async () => {
    const conflict = new ApiProblem(412, "revision_conflict");
    const same = client();
    same.updateTokenMetadata.mockRejectedValue(conflict);
    same.getTokenMetadata.mockResolvedValue({
      body: { description: "About", x_url: "https://x.com/a", telegram_url: "", revision: 1 },
    });
    await attachLaunchMetadata({
      client: same as never,
      token: TOKEN,
      extras: extras({ file: null }),
      getAuth: async () => AUTH,
    });

    const different = client();
    different.updateTokenMetadata.mockRejectedValue(conflict);
    different.getTokenMetadata.mockResolvedValue({
      body: { description: "Someone else", revision: 1 },
    });
    await expect(
      attachLaunchMetadata({
        client: different as never,
        token: TOKEN,
        extras: extras({ file: null }),
        getAuth: async () => AUTH,
      }),
    ).rejects.toMatchObject({ step: "metadata" });
  });
});

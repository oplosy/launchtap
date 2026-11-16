// @vitest-environment jsdom
import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import type { Address } from "viem";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiProblem } from "@/api/problems";
import { WatchlistAuthContext } from "@/watchlist/use-watchlist";

const api = {
  getToken: vi.fn(),
  getTokenMetadata: vi.fn(),
  updateTokenMetadata: vi.fn(),
  updateTokenImage: vi.fn(),
};

vi.mock("@/api/client", () => ({
  ApiClient: class {
    getToken = api.getToken;
    getTokenMetadata = api.getTokenMetadata;
    updateTokenMetadata = api.updateTokenMetadata;
    updateTokenImage = api.updateTokenImage;
  },
}));
vi.mock("@/config/public", () => ({
  publicConfiguration: () => ({ status: "ready", apiBaseUrl: "https://api.example.invalid" }),
}));

const { useLaunchExtras } = await import("./launch-extras");

const FIRST = "0x00000000000000000000000000000000000000a1" as Address;
const SECOND = "0x00000000000000000000000000000000000000b2" as Address;
const AUTH = { accessToken: "access", identityToken: "identity" };
const auth = {
  available: true,
  ready: true,
  authenticated: true,
  getAuth: async () => AUTH,
  login: () => undefined,
};
const wrapper = ({ children }: { children: ReactNode }) => (
  <WatchlistAuthContext.Provider value={auth}>{children}</WatchlistAuthContext.Provider>
);
const links = { xUrl: "", telegramUrl: "" };

/** The description each metadata PUT saved, by token. */
const savedDescriptions = () =>
  api.updateTokenMetadata.mock.calls.map(([token, body]) => [token, body.description]);

beforeEach(() => {
  vi.clearAllMocks();
  api.getToken.mockResolvedValue({});
  api.updateTokenMetadata.mockResolvedValue({});
  api.updateTokenImage.mockResolvedValue({});
});

describe("useLaunchExtras retry", () => {
  it("retries from the index step with the values the launch was signed with", async () => {
    const { result } = renderHook(() => useLaunchExtras(links), { wrapper });
    act(() => result.current.setDescription("signed"));
    api.getToken.mockRejectedValueOnce(new ApiProblem(503, "service_unavailable"));
    await act(() => result.current.run(FIRST));
    expect(result.current.attach).toMatchObject({ status: "failed", step: "index" });

    act(() => result.current.setDescription("edited after the launch"));
    await act(() => result.current.run(FIRST, "index"));

    expect(savedDescriptions()).toEqual([[FIRST, "signed"]]);
    expect(result.current.attach).toEqual({ status: "done", token: FIRST, image: false });
  });

  it("never saves a later launch's details to an earlier token on retry", async () => {
    const { result } = renderHook(() => useLaunchExtras(links), { wrapper });
    act(() => result.current.setDescription("first launch"));
    api.updateTokenMetadata.mockRejectedValueOnce(new ApiProblem(503, "service_unavailable"));
    await act(() => result.current.run(FIRST));
    expect(result.current.attach).toMatchObject({ status: "failed", step: "metadata" });

    act(() => result.current.setDescription("second launch"));
    await act(() => result.current.run(SECOND));
    await act(() => result.current.run(FIRST, "metadata"));

    expect(savedDescriptions()).toEqual([
      [FIRST, "first launch"],
      [SECOND, "second launch"],
      [FIRST, "first launch"],
    ]);
  });
});

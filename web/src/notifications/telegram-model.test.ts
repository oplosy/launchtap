import { describe, expect, it } from "vitest";
import {
  isTelegramLink,
  telegramProblemMessage,
  thresholdToWei,
  weiToThreshold,
} from "./telegram-model";

describe("telegram model", () => {
  it("parses the ETH threshold into wei within 0.5-10,000 ETH", () => {
    expect(thresholdToWei("0.5")).toBe("500000000000000000");
    expect(thresholdToWei(" 2 ")).toBe("2000000000000000000");
    expect(thresholdToWei("10000")).toBe("10000000000000000000000");
    for (const bad of [
      "",
      "0.49",
      "10000.000000000000000001",
      "abc",
      "-1",
      "1e18",
      "1.0000000000000000001",
    ])
      expect(thresholdToWei(bad)).toBeNull();
  });

  it("shows wei as ETH", () => {
    expect(weiToThreshold("500000000000000000")).toBe("0.5");
    expect(weiToThreshold("1234500000000000000")).toBe("1.2345");
    expect(weiToThreshold("nope")).toBe("");
  });

  it("accepts only Telegram deep links", () => {
    expect(isTelegramLink("https://t.me/LaunchBot?start=abc")).toBe(true);
    expect(isTelegramLink("http://t.me/LaunchBot?start=abc")).toBe(false);
    expect(isTelegramLink("https://evil.example/t.me")).toBe(false);
    expect(isTelegramLink("javascript:alert(1)")).toBe(false);
  });

  it("explains refusals", () => {
    expect(telegramProblemMessage("telegram_not_linked")).toMatch(/not connected/);
    expect(telegramProblemMessage("invalid_threshold")).toMatch(/0\.5/);
    expect(telegramProblemMessage("whatever")).toMatch(/could not be saved/);
  });
});

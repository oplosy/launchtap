import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import type { TransactionState } from "@/transactions";
import { TransactionProgress } from "./shared";

function render(props: Parameters<typeof TransactionProgress>[0]) {
  return renderToStaticMarkup(createElement(TransactionProgress, props));
}

describe("TransactionProgress", () => {
  it("renders nothing while disconnected", () => {
    expect(render({ state: { status: "disconnected" }, tone: "warning" })).toBe("");
  });

  it("shows the status, hash, error and stale-status notice", () => {
    const state: TransactionState = {
      status: "indexing",
      hash: "0xabc",
      error: "execution reverted",
      canonicalRefreshUnavailable: true,
    };
    const html = render({ state, tone: "warning" });
    expect(html).toContain('class="transaction-progress" role="status"');
    expect(html).toContain('class="ui-badge ui-badge-warning"');
    expect(html).toContain("Indexing");
    expect(html).toContain('<span class="mono">0xabc</span>');
    expect(html).toContain('class="transaction-error" role="alert"');
    expect(html).toContain("Canonical status refresh unavailable");
  });

  it("prefixes the label and can leave out the stale-status notice", () => {
    const html = render({
      state: { status: "mined", canonicalRefreshUnavailable: true },
      tone: "success",
      prefix: "Approval ",
      refreshNotice: false,
    });
    expect(html).toContain("Approval Mined");
    expect(html).toContain("ui-badge-success");
    expect(html).not.toContain("Canonical status refresh unavailable");
  });
});

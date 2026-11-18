// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import documents from "./documents.json";
import { RISK_ACK_COOKIE, legalDocument, riskAcknowledgementCookie } from "./documents";
import { RiskNotice } from "./risk-notice";

afterEach(() => {
  cleanup();
  document.cookie = `${RISK_ACK_COOKIE}=; Max-Age=0; Path=/`;
});

describe("RiskNotice", () => {
  it("links to the risks and terms and hides after acknowledgement with a cookie", () => {
    render(<RiskNotice initiallyOpen />);
    const notice = screen.getByRole("region", { name: "Risk notice" });
    expect(screen.getByRole("link", { name: "Read the risks" }).getAttribute("href")).toBe(
      "/docs#risk",
    );
    expect(screen.getByRole("link", { name: "terms" }).getAttribute("href")).toBe("/terms");
    fireEvent.click(screen.getByRole("button", { name: "I understand" }));
    expect(notice.isConnected).toBe(false);
    expect(document.cookie).toContain(`${RISK_ACK_COOKIE}=1`);
  });

  it("renders nothing once acknowledged", () => {
    const { container } = render(<RiskNotice initiallyOpen={false} />);
    expect(container.innerHTML).toBe("");
  });
});

describe("legal documents", () => {
  it("builds a one-year Lax cookie, Secure only on https", () => {
    expect(riskAcknowledgementCookie(true)).toBe(
      `${RISK_ACK_COOKIE}=1; Max-Age=31536000; Path=/; SameSite=Lax; Secure`,
    );
    expect(riskAcknowledgementCookie(false)).not.toContain("Secure");
  });

  it("treats anything but approved as a draft", () => {
    expect(legalDocument("terms").status).toBe(
      documents.terms.status === "approved" ? "approved" : "draft",
    );
    expect(legalDocument("privacy").title).toBe("Privacy Policy");
  });
});

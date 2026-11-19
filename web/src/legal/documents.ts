import documents from "./documents.json";

/**
 * Legal document status (docs/specs/2026-11-18-legal-pages-preflight.md). Only counsel moves a
 * document to "approved"; `validate-release.mjs --production` refuses to ship a draft.
 */
export type LegalDocumentId = keyof typeof documents;
export type LegalDocument = {
  title: string;
  status: "draft" | "approved";
  effectiveDate: string | null;
};

export function legalDocument(id: LegalDocumentId): LegalDocument {
  const document = documents[id];
  return {
    title: document.title,
    status: document.status === "approved" ? "approved" : "draft",
    effectiveDate: document.effectiveDate,
  };
}

/** Read on the server by the root layout, so the notice's state is right on the first paint. */
export const RISK_ACK_COOKIE = "launchpad-risk-ack";
export const RISK_ACK_MAX_AGE_SECONDS = 365 * 24 * 60 * 60;

export function riskAcknowledgementCookie(secure: boolean) {
  return `${RISK_ACK_COOKIE}=1; Max-Age=${RISK_ACK_MAX_AGE_SECONDS}; Path=/; SameSite=Lax${secure ? "; Secure" : ""}`;
}

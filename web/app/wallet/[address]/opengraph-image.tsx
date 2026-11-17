import { renderWalletCard } from "@/share/wallet-card";
import { loadWalletCardData } from "@/share/wallet-card-data";
import { notFound } from "next/navigation";
import { isCanonicalAddressSegment } from "@/share/canonical-address";

export const alt = "Wallet on Launchpad";
export const size = { width: 1200, height: 630 };
export const contentType = "image/png";
export const revalidate = 300;

// An empty list makes this an ISR route: each address renders on first request and is then served
// from the route cache for `revalidate` seconds.
export function generateStaticParams() {
  return [];
}

export default async function Image({ params }: { params: Promise<{ address: string }> }) {
  const { address } = await params;
  // A malformed or non-canonically cased address gets a 404 instead of a render: each distinct
  // path is its own ISR entry, so a render per spelling would be free load (security-audit run-4).
  if (!isCanonicalAddressSegment(address)) notFound();
  const card = await loadWalletCardData(address);
  try {
    return await renderWalletCard(card);
  } catch (cause) {
    console.error("wallet share card: render failed, using the generic card", cause);
    return renderWalletCard({ kind: "generic" });
  }
}

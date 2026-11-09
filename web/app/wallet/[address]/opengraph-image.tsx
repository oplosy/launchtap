import { renderWalletCard } from "@/share/wallet-card";
import { loadWalletCardData } from "@/share/wallet-card-data";

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
  const card = await loadWalletCardData(address);
  try {
    return await renderWalletCard(card);
  } catch (cause) {
    console.error("wallet share card: render failed, using the generic card", cause);
    return renderWalletCard({ kind: "generic" });
  }
}

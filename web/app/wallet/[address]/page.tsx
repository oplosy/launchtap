import type { Metadata } from "next";
import { notFound } from "next/navigation";
import { WalletView } from "@/portfolio/wallet-view";
import { walletPath } from "@/portfolio/wallet-model";
import { publicWebOrigin } from "@/share/share-links";
import { parseTokenAddress, shortAddress } from "@/token/address";

export async function generateMetadata({
  params,
}: {
  params: Promise<{ address: string }>;
}): Promise<Metadata> {
  const parsed = parseTokenAddress((await params).address);
  if (!parsed) return { title: "Wallet not found" };
  const title = `Wallet ${shortAddress(parsed)}`;
  const description = "Realized PnL and holdings on Launchpad.";
  const origin = publicWebOrigin();
  // The card image comes from the opengraph-image / twitter-image routes next to this page. They
  // bound their own API reads to 3 s and fall back to the generic card; metadata makes no fetch.
  return {
    title,
    description,
    ...(origin
      ? {
          metadataBase: new URL(origin),
          alternates: { canonical: `${origin}${walletPath(parsed)}` },
        }
      : {}),
    openGraph: { title, description, type: "website", siteName: "Launchpad" },
    twitter: { card: "summary_large_image", title, description },
  };
}

export default async function WalletPage({ params }: { params: Promise<{ address: string }> }) {
  const parsed = parseTokenAddress((await params).address);
  if (!parsed) notFound();
  return <WalletView address={parsed} />;
}

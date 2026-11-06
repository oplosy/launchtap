import type { Metadata } from "next";
import { webOrigin } from "@/config/server";
import { loadCardData } from "@/share/card-data";
import { canonicalTokenUrl } from "@/share/share-links";
import { parseTokenAddress } from "@/token/address";
import { TokenDetail } from "@/token/token-detail";
import Link from "next/link";

export async function generateMetadata({
  params,
}: {
  params: Promise<{ address: string }>;
}): Promise<Metadata> {
  const { address } = await params;
  const parsed = parseTokenAddress(address);
  if (!parsed) return { title: "Token not found" };
  const origin = webOrigin();
  const card = await loadCardData(parsed);
  const title =
    card.kind === "token" ? (card.symbol ? `$${card.symbol} - ${card.name}` : card.name) : "Token";
  const description =
    card.kind === "token"
      ? `${card.name} on Launchpad: market cap ${card.marketCapEth} ETH, ${
          card.phase === "graduated"
            ? "graduated to a liquidity pool"
            : "trading on its bonding curve"
        }.`
      : "A fixed-supply token on Launchpad.";
  return {
    title,
    description,
    ...(origin
      ? {
          metadataBase: new URL(origin),
          alternates: { canonical: canonicalTokenUrl(origin, parsed) },
        }
      : {}),
    openGraph: { title, description, type: "website", siteName: "Launchpad" },
    twitter: { card: "summary_large_image", title, description },
  };
}

export default async function TokenPage({
  params,
  searchParams,
}: {
  params: Promise<{ address: string }>;
  searchParams?: Promise<{ fixture?: string }>;
}) {
  const { address } = await params;
  const query = searchParams ? await searchParams : undefined;
  const parsed = parseTokenAddress(address);
  if (!parsed) return <TokenNotFound />;
  return <TokenDetail address={parsed} fixture={query?.fixture === "populated"} />;
}

function TokenNotFound() {
  return (
    <div className="page-stack token-page">
      <section className="ui-state ui-error-state" role="alert">
        <div>
          <p className="panel-kicker">Token route</p>
          <h1>Token not found</h1>
          <p>This address is not a valid EVM token address.</p>
          <Link className="ui-button ui-button-secondary ui-button-md" href="/">
            Return to Explore
          </Link>
        </div>
      </section>
    </div>
  );
}

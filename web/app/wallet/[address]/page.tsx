import type { Metadata } from "next";
import { notFound } from "next/navigation";
import { WalletView } from "@/portfolio/wallet-view";
import { parseTokenAddress, shortAddress } from "@/token/address";

export async function generateMetadata({
  params,
}: {
  params: Promise<{ address: string }>;
}): Promise<Metadata> {
  const parsed = parseTokenAddress((await params).address);
  return { title: parsed ? `Wallet ${shortAddress(parsed)}` : "Wallet not found" };
}

export default async function WalletPage({ params }: { params: Promise<{ address: string }> }) {
  const parsed = parseTokenAddress((await params).address);
  if (!parsed) notFound();
  return <WalletView address={parsed} />;
}

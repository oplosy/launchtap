import type { components } from "@/api/generated";
import { percentOfSupply, positiveSupply } from "./holder-concentration";

// Copy for the early-buyer clusters read (docs/specs/2026-11-12-early-buyer-clusters-design.md).
// It states the on-chain links and never calls a wallet a sniper; shares use the same exact,
// rounded-down percentages as the other holder lines.

type Body = components["schemas"]["EarlyBuyersBody"];

export type EarlyBuyerGroup = {
  title: string;
  links: string;
  includesCreator: boolean;
  wallets: { address: string; balance: string }[];
  /** Wallets in the group beyond the listed ones. */
  unlisted: number;
};

export type EarlyBuyersView = {
  summary: string;
  groups: EarlyBuyerGroup[];
  /** Shown when early wallets exist but none are linked. */
  emptyGroups: string | null;
  /** Groups beyond the listed ones. */
  moreGroups: number;
  /** Funding-source coverage; null while no early wallet has been checked (worker off). */
  funding: string | null;
};

const CANONICAL = /^(0|[1-9][0-9]*)$/;
const LINK_TEXT: Record<string, string> = {
  same_transaction: "received tokens in the same transaction",
  token_transfer: "moved tokens between them",
  funding_source: "funded by the same wallet",
};

export function earlyBuyersView(body: Body, totalSupply: string): EarlyBuyersView | null {
  const supply = positiveSupply(totalSupply);
  const clusters = body.clusters ?? [];
  if (supply === null || !CANONICAL.test(body.early_balance)) return null;
  if (!clusters.every((cluster) => CANONICAL.test(cluster.balance))) return null;
  const seconds = body.window_seconds;
  if (body.early_wallet_count === 0)
    return {
      summary: body.window_open
        ? `No wallet has got tokens from the curve in the first ${seconds} seconds yet.`
        : `No wallet got tokens from the curve in the first ${seconds} seconds.`,
      groups: [],
      emptyGroups: null,
      moreGroups: 0,
      funding: null,
    };
  const one = body.early_wallet_count === 1;
  const summary =
    `${body.early_wallet_count} ${one ? "wallet" : "wallets"} got tokens from the curve in the ` +
    `first ${seconds} seconds and ${one ? "holds" : "hold"} ` +
    `${percentOfSupply(BigInt(body.early_balance), supply)} of the supply now.` +
    (body.window_open ? ` The ${seconds} seconds have not passed yet.` : "");
  const groups = clusters.map((cluster) => {
    const wallets = cluster.wallets ?? [];
    return {
      title: `${cluster.wallet_count} wallets hold ${percentOfSupply(BigInt(cluster.balance), supply)} of the supply`,
      links: (cluster.links ?? [])
        .map((link) => LINK_TEXT[link])
        .filter(Boolean)
        .join(" and "),
      includesCreator: cluster.includes_creator,
      wallets: wallets.map(({ address, balance }) => ({ address, balance })),
      unlisted: Math.max(0, cluster.wallet_count - wallets.length),
    };
  });
  return {
    summary,
    groups,
    emptyGroups: groups.length ? null : "No linked early wallets found.",
    moreGroups: Math.max(0, body.cluster_count - groups.length),
    funding:
      body.funding_checked > 0
        ? `Funding sources checked for ${body.funding_checked} of ${body.early_wallet_count} early wallets.`
        : null,
  };
}

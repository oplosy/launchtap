import type { components } from "@/api/generated";
import { formatCanonicalBaseUnits } from "@/amounts";

// Rows for the Analytics "Protocol revenue" card (docs/specs/2026-11-12-protocol-revenue-design.md).
// Amounts are wei strings; a malformed one hides the card instead of showing a guess.

type Body = components["schemas"]["RevenueBody"];
type SourceKey = keyof Body["sources"];

export type RevenueRow = { label: string; engine1: string; engine2: string; total: string };
export type ProtocolRevenueView = { total: string; rows: RevenueRow[]; block: number };

const SOURCES: [SourceKey, string][] = [
  ["launch_fees", "Launch fees"],
  ["trade_fees", "Trade fees"],
  ["snipe_tax", "Snipe tax"],
  ["graduation_remainder", "Graduation remainder"],
  ["lp_fees", "LP fees"],
];
const CANONICAL = /^(0|[1-9][0-9]*)$/;

function eth(value: string): string | null {
  if (!CANONICAL.test(value)) return null;
  const formatted = formatCanonicalBaseUnits(value, 6);
  return formatted === null ? null : `${formatted} ETH`;
}

export function protocolRevenueView(body: Body): ProtocolRevenueView | null {
  const total = eth(body.total);
  if (total === null) return null;
  const rows: RevenueRow[] = [];
  for (const [key, label] of SOURCES) {
    const source = body.sources[key];
    const engine1 = eth(source.engine_1);
    const engine2 = eth(source.engine_2);
    const sourceTotal = eth(source.total);
    if (engine1 === null || engine2 === null || sourceTotal === null) return null;
    if (source.total !== "0") rows.push({ label, engine1, engine2, total: sourceTotal });
  }
  return { total, rows, block: body.snapshot.as_of_block };
}

import type { components } from "@/api/generated";
import { formatBps, formatDuration } from "@/launch-v2";

// Engine-2 token facts for the token page (Plan v2-C Task 8). Indexed API values only; the
// trading panel reads the authoritative on-chain roles and balances itself.

type TokenV2 = components["schemas"]["TokenV2DTO"];

export type V2Facts = {
  antiSnipe: string;
  antiSnipeEndsAt: string;
  lpCreatorShare: string;
  feeRecipient: string;
  pendingFeeRecipient: string | null;
  pool: { poolId: string; hook: string } | null;
  lockerOwed: { account: string; eth: string; token: string }[];
  /**
   * False for a graduated v2 token: the backend does not project V4 pool liquidity yet
   * (Plan v2-B known limitation), so its liquidity and reserve fields are placeholders.
   */
  liquidityIndexed: boolean;
};

export function v2Facts(token: {
  engine_version: number;
  phase: string;
  v2?: TokenV2;
}): V2Facts | null {
  if (token.engine_version !== 2 || !token.v2) return null;
  const { v2 } = token;
  const graduated = token.phase.toLowerCase() === "graduated";
  return {
    antiSnipe: `${formatBps(v2.anti_snipe.start_bps)}, decaying to 0 over ${formatDuration(
      v2.anti_snipe.duration_seconds,
    )}`,
    antiSnipeEndsAt: v2.anti_snipe.ends_at,
    lpCreatorShare: formatBps(v2.lp_creator_share_bps),
    feeRecipient: v2.fee_recipient,
    pendingFeeRecipient: v2.pending_fee_recipient,
    pool: graduated && v2.pool ? { poolId: v2.pool.pool_id, hook: v2.pool.hook } : null,
    lockerOwed: v2.locker_owed ?? [],
    liquidityIndexed: !graduated,
  };
}

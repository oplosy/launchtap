import { getAddress, isAddress } from "viem";

// Engine-2 creator fee recipient change (Plan v2-C Task 6, spec §5.3). The creator address is
// identity only; the on-chain `feeRecipient` is the payout address and the only account that
// can propose, replace or cancel a change. Only `pendingFeeRecipient` can accept it.

const ZERO = "0x0000000000000000000000000000000000000000";

export type RecipientAction = "propose" | "replace" | "cancel" | "accept";

function same(a: string, b: string) {
  return a.toLowerCase() === b.toLowerCase();
}

/** The recipient actions the connected account may take; mirrors the curve's access checks. */
export function recipientActions(args: {
  account: string | undefined;
  feeRecipient: string;
  pendingFeeRecipient: string;
}): RecipientAction[] {
  const { account, feeRecipient, pendingFeeRecipient } = args;
  if (!account || same(account, ZERO)) return [];
  const hasProposal = !same(pendingFeeRecipient, ZERO);
  if (same(account, feeRecipient)) return hasProposal ? ["replace", "cancel"] : ["propose"];
  if (hasProposal && same(account, pendingFeeRecipient)) return ["accept"];
  return [];
}

export type RecipientValidation =
  { ok: true; address: `0x${string}` } | { ok: false; error: string };

/**
 * A proposal must be a well-formed address (a mixed-case one must carry a valid EIP-55
 * checksum), non-zero, and different from the current recipient.
 */
export function validateProposedRecipient(
  input: string,
  currentRecipient: string,
): RecipientValidation {
  const value = input.trim();
  if (!isAddress(value, { strict: true }))
    return { ok: false, error: "Enter a valid address with a correct checksum." };
  if (same(value, ZERO)) return { ok: false, error: "The zero address cannot receive fees." };
  if (same(value, currentRecipient))
    return { ok: false, error: "This address is already the fee recipient." };
  return { ok: true, address: getAddress(value) };
}

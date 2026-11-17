import { getAddress } from "viem";
import { parseTokenAddress } from "@/token/address";

/**
 * True for an address route segment in one of its two canonical spellings: all lowercase (the
 * form the app links to) or EIP-55 checksummed. Each spelling of a path is its own ISR entry, so
 * share-card routes render only these and answer 404 for every other casing (security-audit
 * run-4).
 */
export function isCanonicalAddressSegment(value: string): boolean {
  const parsed = parseTokenAddress(value);
  if (!parsed) return false;
  return value === parsed || getAddress(parsed) === value;
}

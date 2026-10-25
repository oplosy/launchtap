/** Accept only a canonical EVM address; malformed route segments fail closed. */
export function parseTokenAddress(value: string | null | undefined): `0x${string}` | null {
  if (!value || !/^0x[0-9a-fA-F]{40}$/.test(value)) return null;
  return value.toLowerCase() as `0x${string}`;
}

export function shortAddress(address: string, edge = 6) {
  if (!parseTokenAddress(address) || edge < 2) return "Unavailable";
  return `${address.slice(0, edge + 2)}…${address.slice(-edge)}`;
}

/** A stable hue per address for artwork fallbacks, so tokens stay distinct without images. */
export function addressHue(address: string) {
  // FNV-1a plus a murmur finaliser, so addresses one character apart land on distant hues.
  let hash = 0x811c9dc5;
  for (const char of address.toLowerCase()) hash = Math.imul(hash ^ char.charCodeAt(0), 0x01000193);
  hash = Math.imul(hash ^ (hash >>> 16), 0x85ebca6b);
  hash = Math.imul(hash ^ (hash >>> 13), 0xc2b2ae35);
  return ((hash ^ (hash >>> 16)) >>> 0) % 360;
}

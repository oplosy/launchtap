// Client mirrors of the username rules (backend/internal/social/username.go). The server stays
// authoritative: reserved and taken names are server answers, not client checks.

export const USERNAME_PATTERN = /^[a-z0-9_]{3,20}$/;
export const MAX_PUBLIC_WALLETS = 5;

/** Trims and lowercases, as the server does before validating. */
export function normalizeUsername(raw: string): string {
  return raw.trim().toLowerCase();
}

/** A format hint for the input, or null when the format is acceptable. */
export function usernameHint(raw: string): string | null {
  const name = normalizeUsername(raw);
  if (!name) return null;
  if (name.startsWith("0x")) return "A username cannot start with 0x.";
  if (!USERNAME_PATTERN.test(name)) return "Use 3-20 characters: a-z, 0-9 and _.";
  return null;
}

/** "You can change it again on 16 Nov 2026", or null when a change is allowed now. */
export function nextChangeLabel(iso: string | null, now: Date): string | null {
  if (!iso) return null;
  const at = new Date(iso);
  if (Number.isNaN(at.getTime()) || at.getTime() <= now.getTime()) return null;
  const date = new Intl.DateTimeFormat("en-GB", {
    day: "numeric",
    month: "short",
    year: "numeric",
    timeZone: "UTC",
  }).format(at);
  return `You can change it again on ${date}`;
}

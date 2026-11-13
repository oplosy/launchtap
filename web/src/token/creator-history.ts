// One line on the token page about the creator's other launches, from the public created-tokens
// list (`/v1/wallets/{address}/created`). Only on-chain facts: how many other tokens this wallet
// launched here and how many graduated. When the list was cut off at one page, both counts are
// stated as lower bounds rather than guessed.

export function creatorHistoryLine(
  launches: readonly { token: string; phase: string }[],
  currentToken: string,
  hasMore: boolean,
): string {
  const current = currentToken.toLowerCase();
  const others = launches.filter((launch) => launch.token.toLowerCase() !== current);
  if (!others.length && !hasMore) return "This is the creator's first launch on Launchpad.";
  const graduated = others.filter((launch) => launch.phase.toLowerCase() === "graduated").length;
  const atLeast = hasMore ? "at least " : "";
  const noun = others.length === 1 && !hasMore ? "token" : "tokens";
  return `Launched ${atLeast}${others.length} other ${noun} on Launchpad; ${atLeast}${graduated} graduated.`;
}

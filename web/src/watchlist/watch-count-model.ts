/**
 * The count shown next to the star: the server's count, adjusted by the reader's own toggles
 * since the count was fetched. `baseline` is whether the reader watched the token when the
 * count was taken (null while unknown: signed out, or the watchlist has not loaded yet).
 */
export function displayWatchCount(
  count: number,
  baseline: boolean | null,
  listReady: boolean,
  starred: boolean,
): number {
  if (baseline === null || !listReady) return count;
  return Math.max(0, count + Number(starred) - Number(baseline));
}

export function watchCountLabel(count: number): string {
  return `${count.toLocaleString("en-US")} watching`;
}

/** How long one post stays in the field, fog in and fog out included. */
export const FIELD_LIFETIME_MS = 15_000;
/** Most posts on screen at once; one cell is always left free so a new post has somewhere new
 *  to appear. */
export const FIELD_TARGET = 9;
/** Recent posts the field cycles through. */
export const FIELD_POOL = 30;

export type Spot = { x: number; y: number; w: number; h: number };

/**
 * Splits the field into a grid of cells, as many as fit a card of `size` with `gap` between them;
 * the spare room is shared out, so each cell is a little larger than a card.
 */
export function layoutCells(
  area: { width: number; height: number },
  size: { w: number; h: number },
  gap: number,
): Spot[] {
  const columns = Math.floor((area.width + gap) / (size.w + gap));
  const rows = Math.floor((area.height + gap) / (size.h + gap));
  if (columns < 1 || rows < 1) return [];
  const w = Math.floor(area.width / columns);
  const h = Math.floor(area.height / rows);
  const cells: Spot[] = [];
  for (let row = 0; row < rows; row++)
    for (let column = 0; column < columns; column++)
      cells.push({ x: column * w, y: row * h, w, h });
  return cells;
}

/** A card's box at a random offset inside its cell, so the grid never reads as a grid. */
export function placeInCell(
  cell: Spot,
  size: { w: number; h: number },
  random: () => number,
): Spot {
  return {
    x: cell.x + Math.round(random() * Math.max(0, cell.w - size.w)),
    y: cell.y + Math.round(random() * Math.max(0, cell.h - size.h)),
    w: size.w,
    h: size.h,
  };
}

/**
 * A random free cell, avoiding the one a card just left so the next card appears elsewhere; that
 * cell is used only when it is the last one free. Null when every cell is taken.
 */
export function pickCell(
  count: number,
  taken: ReadonlySet<number>,
  avoid: number | null,
  random: () => number,
): number | null {
  const free: number[] = [];
  for (let index = 0; index < count; index++)
    if (!taken.has(index) && index !== avoid) free.push(index);
  if (free.length > 0) return free[Math.floor(random() * free.length)] ?? null;
  return avoid !== null && avoid < count && !taken.has(avoid) ? avoid : null;
}

/**
 * The next post to show: one never shown yet (the list is newest first), otherwise the one shown
 * longest ago. A post already on screen is never picked twice.
 */
export function nextPost<T extends { id: string }>(
  posts: readonly T[],
  onScreen: ReadonlySet<string>,
  lastShown: ReadonlyMap<string, number>,
): T | null {
  let best: T | null = null;
  let bestAt = Infinity;
  for (const post of posts) {
    if (onScreen.has(post.id)) continue;
    const at = lastShown.get(post.id) ?? -Infinity;
    if (at < bestAt) {
      best = post;
      bestAt = at;
    }
  }
  return best;
}

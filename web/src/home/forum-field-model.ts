/** How long one post stays in the field, fog in and fog out included. */
export const FIELD_LIFETIME_MS = 15_000;
/** Posts on screen at once: wide screens, and narrow ones where fewer boxes fit. */
export const FIELD_TARGET_WIDE = 9;
export const FIELD_TARGET_NARROW = 4;
/** Recent posts the field cycles through. */
export const FIELD_POOL = 30;

export type Spot = { x: number; y: number; w: number; h: number };

/** Whether two boxes touch, counting `gap` pixels around them as taken. */
export function overlaps(a: Spot, b: Spot, gap: number): boolean {
  return (
    a.x < b.x + b.w + gap && b.x < a.x + a.w + gap && a.y < b.y + b.h + gap && b.y < a.y + a.h + gap
  );
}

/**
 * A random top-left position for a box of `size` inside `area` that keeps clear of every box in
 * `taken`, or null after `tries` misses (the caller tries again on its next tick).
 */
export function pickSpot(
  taken: readonly Spot[],
  area: { width: number; height: number },
  size: { w: number; h: number },
  random: () => number,
  tries = 80,
  gap = 16,
): Spot | null {
  const freeX = area.width - size.w;
  const freeY = area.height - size.h;
  if (freeX < 0 || freeY < 0) return null;
  for (let attempt = 0; attempt < tries; attempt++) {
    const spot = {
      x: Math.round(random() * freeX),
      y: Math.round(random() * freeY),
      w: size.w,
      h: size.h,
    };
    if (!taken.some((box) => overlaps(box, spot, gap))) return spot;
  }
  return null;
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

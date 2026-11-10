type Zone<T> = { contains(node: T): boolean } | null;

/**
 * Whether focus moving to `next` has left a disclosure menu made of `zones` (its trigger and its
 * panel). `next` is null when focus goes nowhere focusable, such as a click on panel text; that
 * does not count as leaving, so reading the menu never closes it.
 */
export function focusLeftMenu<T>(next: T | null, zones: ReadonlyArray<Zone<T>>): boolean {
  if (next === null) return false;
  return !zones.some((zone) => zone?.contains(next));
}

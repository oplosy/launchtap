// Mirrors social.NormalizeBody (backend/internal/social/comment.go) so the composer can explain a
// rule before posting. The server stays authoritative; these constants mirror
// backend/internal/social/policy.go and a test pins them to the spec values.

export const MAX_BODY_CODE_POINTS = 500;
export const MAX_NEWLINE_RUN = 3;

export type BodyRule = "empty" | "too_long" | "control" | "bidi" | "newlines";

/** Counts Unicode code points, as the server does (an emoji is one, not two UTF-16 units). */
export function codePointCount(text: string): number {
  return Array.from(text).length;
}

const ALLOWED_INVISIBLE = new Set([0x0a, 0x200c, 0x200d]);
const REJECTED_INVISIBLE = new Set([0x2028, 0x2029, 0x200e, 0x200f, 0x061c, 0x200b, 0xfeff]);

function isControl(cp: number) {
  if (ALLOWED_INVISIBLE.has(cp)) return false;
  if (REJECTED_INVISIBLE.has(cp)) return true;
  return cp <= 0x1f || (cp >= 0x7f && cp <= 0x9f);
}

function isBidi(cp: number) {
  return (cp >= 0x202a && cp <= 0x202e) || (cp >= 0x2066 && cp <= 0x2069);
}

/** Applies the comment body rules in the server's order and returns the normalized body. */
export function checkBody(raw: string): { ok: true; body: string } | { ok: false; rule: BodyRule } {
  const unified = raw.replace(/\r\n/g, "\n").replace(/\r/g, "\n");
  for (const ch of unified) {
    if (isControl(ch.codePointAt(0) ?? 0)) return { ok: false, rule: "control" };
  }
  const body = unified.trim();
  if (!body) return { ok: false, rule: "empty" };
  let run = 0;
  let longRun = false;
  let count = 0;
  for (const ch of body) {
    const cp = ch.codePointAt(0) ?? 0;
    count += 1;
    if (isBidi(cp)) return { ok: false, rule: "bidi" };
    if (cp === 0x0a) {
      run += 1;
      if (run > MAX_NEWLINE_RUN) longRun = true;
    } else {
      run = 0;
    }
  }
  if (longRun) return { ok: false, rule: "newlines" };
  if (count > MAX_BODY_CODE_POINTS) return { ok: false, rule: "too_long" };
  return { ok: true, body };
}

export const BODY_RULE_MESSAGES: Record<BodyRule, string> = {
  empty: "Write something first.",
  too_long: `Comments are limited to ${MAX_BODY_CODE_POINTS} characters.`,
  control: "Remove invisible or control characters.",
  bidi: "Remove text-direction override characters.",
  newlines: `Use at most ${MAX_NEWLINE_RUN} line breaks in a row.`,
};

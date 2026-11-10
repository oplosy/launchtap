import { describe, expect, it } from "vitest";
import { checkBody, codePointCount, MAX_BODY_CODE_POINTS, MAX_NEWLINE_RUN } from "./comment-rules";

describe("comment rules", () => {
  it("pins the spec constants", () => {
    expect(MAX_BODY_CODE_POINTS).toBe(500);
    expect(MAX_NEWLINE_RUN).toBe(3);
  });

  it("counts code points", () => {
    expect(codePointCount("😀".repeat(500))).toBe(500);
    expect(codePointCount("é")).toBe(1);
  });

  // Mirrors TestNormalizeBodyVectors in backend/internal/social/comment_test.go (the invalid UTF-8
  // vector is not expressible in a JS string).
  const vectors: Array<[string, string, string | null]> = [
    ["trim", "  hi  ", "hi"],
    ["crlf", "a\r\nb", "a\nb"],
    ["lone cr", "a\rb", "a\nb"],
    ["three newlines", "a\n\n\nb", "a\n\n\nb"],
    ["zwnj kept", "a‌b", "a‌b"],
    ["zwj kept", "a‍b", "a‍b"],
    ["500 two-byte runes", "é".repeat(500), "é".repeat(500)],
    ["500 emoji", "😀".repeat(500), "😀".repeat(500)],
  ];
  it.each(vectors)("accepts %s", (_, input, want) => {
    expect(checkBody(input)).toEqual({ ok: true, body: want });
  });

  const rejected: Array<[string, string, string]> = [
    ["four newlines", "a\n\n\n\nb", "newlines"],
    ["nul", "\u0000", "control"],
    ["tab", "\t", "control"],
    ["tab inside", "a\tb", "control"],
    ["line separator", "a b", "control"],
    ["paragraph separator", "a b", "control"],
    ["lrm", "a‎b", "control"],
    ["rlm", "a‏b", "control"],
    ["arabic letter mark", "a؜b", "control"],
    ["zero width space", "a​b", "control"],
    ["bom inside", "a﻿b", "control"],
    ["bidi override", "a‮b", "bidi"],
    ["bidi isolate", "a⁦b", "bidi"],
    ["bidi range edge", "a⁩b", "bidi"],
    ["501 runes", "a".repeat(501), "too_long"],
    ["blank", "   ", "empty"],
    ["empty", "", "empty"],
  ];
  it.each(rejected)("rejects %s", (_, input, rule) => {
    expect(checkBody(input)).toEqual({ ok: false, rule });
  });
});

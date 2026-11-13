/**
 * Client-side checks for creator metadata, shared by the launch form and the token page editor.
 * The API stays the authority (`apiserver.detectImage`, `validateMetadata`); these only keep an
 * obviously invalid upload from being sent.
 */

export const maxImageBytes = 5 * 1024 * 1024;
export const imageTypes = new Set(["image/png", "image/jpeg", "image/webp"]);
export const maxDescriptionBytes = 2000;

export function safeHttps(value: string) {
  if (!value.trim()) return true;
  if (new TextEncoder().encode(value).length > 2048) return false;
  try {
    const url = new URL(value);
    return url.protocol === "https:" && Boolean(url.hostname) && !url.username && !url.password;
  } catch {
    return false;
  }
}

export function descriptionTooLong(value: string) {
  return new TextEncoder().encode(value).length > maxDescriptionBytes;
}

/** Declared type and size, before the bytes are read. */
export function acceptableImageFile(file: { type: string; size: number }) {
  return imageTypes.has(file.type) && file.size >= 1 && file.size <= maxImageBytes;
}

/** The magic bytes of a PNG, JPEG or WebP file; mirrors the API's `detectImage` prefixes. */
export async function hasValidImageSignature(file: Blob) {
  const bytes = new Uint8Array(await file.slice(0, 12).arrayBuffer());
  const png =
    bytes.length >= 8 && [137, 80, 78, 71, 13, 10, 26, 10].every((v, i) => bytes[i] === v);
  const jpeg = bytes.length >= 3 && bytes[0] === 255 && bytes[1] === 216 && bytes[2] === 255;
  const webp =
    bytes.length >= 12 &&
    new TextDecoder().decode(bytes.slice(0, 4)) === "RIFF" &&
    new TextDecoder().decode(bytes.slice(8, 12)) === "WEBP";
  return png || jpeg || webp;
}

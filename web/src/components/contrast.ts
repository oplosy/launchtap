/**
 * The colour tokens that carry text and data meaning, per theme. `app/styles/tokens.css` uses the
 * same hex values; `contrast.test.ts` keeps them at WCAG AA (AAA for primary text).
 */
export const THEME_TOKENS = {
  dark: {
    canvas: "#000000",
    fill: "#1a1a1a",
    text: "#ffffff",
    text2: "#a3a3a3",
    text3: "#8a8a8a",
    up: "#34c77b",
    down: "#ff6461",
  },
  light: {
    canvas: "#ffffff",
    fill: "#f2f2f2",
    text: "#0a0a0a",
    text2: "#5c5c5c",
    text3: "#6b6b6b",
    up: "#0f8a4a",
    down: "#d1352f",
  },
} as const;

function luminance(hex: string): number {
  const value = hex.replace("#", "");
  const linear = (offset: number) => {
    const c = Number.parseInt(value.slice(offset, offset + 2), 16) / 255;
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  };
  return 0.2126 * linear(0) + 0.7152 * linear(2) + 0.0722 * linear(4);
}

/** WCAG 2 contrast ratio between two `#rrggbb` colours. */
export function contrastRatio(foreground: string, background: string): number {
  const a = luminance(foreground);
  const b = luminance(background);
  return (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05);
}

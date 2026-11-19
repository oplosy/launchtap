/**
 * Small line drawings for section headings, taken from the card artwork's motifs. Every stroke
 * has `pathLength="1"` so CSS can draw it in as its section is revealed (see `.section-motif`).
 */
const motifs = {
  // A sun rising over the horizon (Trending).
  sun: ["M4 26H44", "M12 26A12 12 0 0 1 36 26", "M24 9V4", "M13.5 13.5L10 10", "M34.5 13.5L38 10"],
  // A classical portico (Graduated).
  columns: [
    "M6 7H42",
    "M8 10H40",
    "M12 10V25",
    "M15 10V25",
    "M22.5 10V25",
    "M25.5 10V25",
    "M33 10V25",
    "M36 10V25",
    "M6 28H42",
  ],
  // A shell spiral (From the forum).
  spiral: [
    "M24 16a2 2 0 0 1 2 2a4 4 0 0 1-4 4a6 6 0 0 1-6-6a8 8 0 0 1 8-8a10 10 0 0 1 10 10a12 12 0 0 1-12 12",
  ],
} as const;

export type MotifName = keyof typeof motifs;

export function SectionMotif({ name }: { name: MotifName }) {
  return (
    <svg className="section-motif" viewBox="0 0 48 32" aria-hidden="true" focusable="false">
      {motifs[name].map((d) => (
        <path key={d} d={d} pathLength={1} />
      ))}
    </svg>
  );
}

// Illustrative V1 curve for the landing visuals only. Floats are fine here: nothing is quoted,
// signed, or compared with chain state. Parameters mirror notes.md "Bonding curve".
export const TOTAL_SUPPLY = 1_000_000_000;
export const CURVE_TOKENS = 800_000_000;
export const LP_TOKENS = 200_000_000;
export const GRADUATION_ETH = 4.2;
const X0 = (GRADUATION_ETH * LP_TOKENS) / (CURVE_TOKENS - LP_TOKENS);
const Y0 = (CURVE_TOKENS * (X0 + GRADUATION_ETH)) / GRADUATION_ETH;
const K = X0 * Y0;

export type CurvePoint = {
  ethRaised: number;
  tokensSold: number;
  priceEth: number;
  marketCapEth: number;
  progress: number;
};

/** Virtual-reserve constant product: x·y = k with x = x0 + real ETH. */
export function curveAt(ethRaised: number): CurvePoint {
  const eth = Math.min(Math.max(ethRaised, 0), GRADUATION_ETH);
  const x = X0 + eth;
  const y = K / x;
  const priceEth = x / y;
  return {
    ethRaised: eth,
    tokensSold: Y0 - y,
    priceEth,
    marketCapEth: priceEth * TOTAL_SUPPLY,
    progress: eth / GRADUATION_ETH,
  };
}

/** Price relative to the launch price, so the chart's y axis runs 1× → graduation multiple. */
export function priceMultiple(point: CurvePoint) {
  return point.priceEth / curveAt(0).priceEth;
}

/** SVG path of price (y) against tokens sold (x), normalised into a width × height box. */
export function curvePath(width: number, height: number, samples = 64) {
  const top = priceMultiple(curveAt(GRADUATION_ETH));
  const commands: string[] = [];
  for (let index = 0; index <= samples; index += 1) {
    const point = curveAt((GRADUATION_ETH * index) / samples);
    const [x, y] = chartCoordinates(point, width, height, top);
    commands.push(`${index === 0 ? "M" : "L"}${x.toFixed(2)} ${y.toFixed(2)}`);
  }
  return commands.join(" ");
}

export function chartCoordinates(
  point: CurvePoint,
  width: number,
  height: number,
  top = priceMultiple(curveAt(GRADUATION_ETH)),
): [number, number] {
  const x = (point.tokensSold / CURVE_TOKENS) * width;
  const y = height - ((priceMultiple(point) - 1) / (top - 1)) * height;
  return [x, y];
}

export function formatCompact(value: number) {
  return new Intl.NumberFormat("en-US", { notation: "compact", maximumFractionDigits: 1 }).format(
    value,
  );
}

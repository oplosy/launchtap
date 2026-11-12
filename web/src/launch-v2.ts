// Engine-2 launch facts (Plan v2-C D8): read from `LaunchFactoryV2.futureDefaults()`, which is
// exactly what the next launch snapshots. Integer arithmetic only; never a float.

/** `2000` → `"20%"`, `150` → `"1.5%"`, `1` → `"0.01%"`. */
export function formatBps(bps: number): string {
  const whole = Math.trunc(bps / 100);
  const fraction = (bps % 100).toString().padStart(2, "0").replace(/0+$/, "");
  return fraction ? `${whole}.${fraction}%` : `${whole}%`;
}

/** Whole units, largest first, at most two of them: `5400` → `"1 h 30 min"`. */
export function formatDuration(seconds: number): string {
  if (seconds <= 0) return "none";
  const parts: [number, string][] = [
    [Math.floor(seconds / 3600), "h"],
    [Math.floor((seconds % 3600) / 60), "min"],
    [seconds % 60, "s"],
  ];
  return parts
    .filter(([value]) => value > 0)
    .slice(0, 2)
    .map(([value, unit]) => `${value} ${unit}`)
    .join(" ");
}

export function launchRulesV2(defaults: {
  antiSnipe: { startBps: number; durationSeconds: number };
  lpCreatorShareBps: number;
}) {
  return {
    taxStart: formatBps(defaults.antiSnipe.startBps),
    taxWindow: formatDuration(defaults.antiSnipe.durationSeconds),
    lpCreatorShare: formatBps(defaults.lpCreatorShareBps),
    lpProtocolShare: formatBps(10_000 - defaults.lpCreatorShareBps),
  };
}

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

/**
 * A stable fingerprint of the raw `futureDefaults` read (curve parameters, anti-snipe profile and
 * LP creator share), so a sign-time re-read can prove the launch snapshots the reviewed profile.
 */
export function launchDefaultsKey(defaults: unknown): string {
  return JSON.stringify(defaults, (_, value: unknown) =>
    typeof value === "bigint" ? value.toString() : value,
  );
}

type FactoryRead = (functionName: string, args?: readonly unknown[]) => Promise<unknown>;

/** The raw engine-2 defaults as the review shows them, plus the fingerprint that binds them. */
export function launchProfileV2(defaults: unknown) {
  return {
    rules: launchRulesV2(defaults as Parameters<typeof launchRulesV2>[0]),
    key: launchDefaultsKey(defaults),
  };
}

/**
 * The sign-time re-read of a launch: a pause or a disabled engine stops it, and on engine 2 the
 * defaults the factory will snapshot are compared with the reviewed ones (`changed`).
 */
export async function requoteLaunch(
  read: FactoryRead,
  engineVersion: number,
  reviewedKey?: string,
) {
  const [fee, paused, enabled, defaults] = await Promise.all([
    read("launchFee") as Promise<bigint>,
    read("launchesPaused"),
    engineVersion === 2 ? Promise.resolve(true) : read("engineEnabled", [1]),
    engineVersion === 2 ? read("futureDefaults") : Promise.resolve(null),
  ]);
  if (paused) throw new Error("LaunchesPaused");
  if (!enabled) throw new Error("EngineDisabled");
  const profile = engineVersion === 2 ? launchProfileV2(defaults) : null;
  return { fee, profile, changed: profile !== null && profile.key !== reviewedKey };
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

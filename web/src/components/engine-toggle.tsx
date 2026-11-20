import { engineDeployment, type PublicConfiguration } from "@/config/public";

export type LaunchEngine = 1 | 2;

const ENGINES: readonly { version: LaunchEngine; label: string; summary: string }[] = [
  {
    version: 1,
    label: "Engine v1",
    summary: "Graduates to a Uniswap v2 pair. The pool's LP tokens are burned.",
  },
  {
    version: 2,
    label: "Engine v2",
    summary:
      "Graduates to a Uniswap v4 pool. The LP stays locked forever and you share its trading fees.",
  },
];

/** The engines this network can launch on: each needs an enabled, reviewed deployment. */
export function availableEngines(
  configuration: Pick<PublicConfiguration, "engines">,
): LaunchEngine[] {
  return ENGINES.filter(({ version }) => engineDeployment(configuration, version)).map(
    ({ version }) => version,
  );
}

/** Engine 1 is preselected whenever it is available (dual-engine pre-flight, Q1). */
export function defaultEngine(available: readonly LaunchEngine[]): LaunchEngine {
  return available.includes(1) ? 1 : (available[0] ?? 1);
}

/**
 * The launch engine choice on the create page. Engine 1 and engine 2 run side by side, and a
 * token keeps its engine for its whole life; an engine without a deployment on this network is
 * shown but cannot be picked.
 */
export function EngineChoice({
  value,
  available,
  onChange,
}: {
  value: LaunchEngine;
  available: readonly LaunchEngine[];
  onChange: (engine: LaunchEngine) => void;
}) {
  const selected = ENGINES.find((engine) => engine.version === value) ?? ENGINES[0];
  return (
    <div className="engine-choice">
      <div className="ui-segment" role="group" aria-label="Launch engine">
        {ENGINES.map((engine) => {
          const enabled = available.includes(engine.version);
          return (
            <button
              key={engine.version}
              type="button"
              aria-pressed={engine.version === value}
              disabled={!enabled}
              title={enabled ? undefined : "Not deployed on this network"}
              onClick={() => onChange(engine.version)}
            >
              {engine.label}
            </button>
          );
        })}
      </div>
      <p className="engine-choice-summary">{selected?.summary}</p>
    </div>
  );
}

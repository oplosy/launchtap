"use client";

import { publicConfiguration } from "@/config/public";

/**
 * Read-only launch engine badge on the create page (Plan v2-C D7). One deployment serves one
 * engine, so the configured deployment decides it; a choice control would mislead.
 */
export function EngineBadge() {
  const engineVersion = publicConfiguration().deployment?.engineVersion;
  if (engineVersion !== 1 && engineVersion !== 2) return null;
  return (
    <span className="engine-badge" aria-label={`Launch engine: v${engineVersion}`}>
      Engine v{engineVersion}
    </span>
  );
}

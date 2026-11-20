"use client";

import Link from "next/link";
import { useState } from "react";
import { publicConfiguration } from "@/config/public";
import { LaunchPanel } from "@/transactions-panel";
import { availableEngines, defaultEngine, EngineChoice, type LaunchEngine } from "./engine-toggle";

/**
 * The create page body: the engine choice above the launch form. The form remounts when the
 * engine changes, so no factory read of one engine can leak into a launch on the other.
 */
export function CreateWorkspace() {
  const available = availableEngines(publicConfiguration());
  const [engine, setEngine] = useState<LaunchEngine>(() => defaultEngine(available));
  return (
    <>
      <div className="create-page-toolbar">
        <Link className="ui-button ui-button-secondary ui-button-sm create-back-button" href="/">
          <span aria-hidden="true">‹</span> Back
        </Link>
        {available.length ? (
          <EngineChoice value={engine} available={available} onChange={setEngine} />
        ) : null}
      </div>
      <LaunchPanel key={engine} engineVersion={engine} />
    </>
  );
}

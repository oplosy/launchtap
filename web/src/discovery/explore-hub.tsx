import { Suspense } from "react";
import { Hero } from "@/landing/hero";
import { LaunchStory } from "@/landing/launch-story";
import { TokenDiscovery } from "./token-discovery";

export function ExploreHub() {
  return (
    <div className="page-stack explore-hub">
      <Hero />
      <LaunchStory />
      <section className="explore-intro" id="launches" aria-labelledby="explore-title">
        <h2 id="explore-title">Explore launches.</h2>
        <p>Search graduated tokens, then follow active launches moving toward liquidity.</p>
      </section>
      <Suspense fallback={<ExploreFallback label="Loading graduated tokens…" />}>
        <TokenDiscovery
          defaultPhase="graduated"
          fixedPhase="graduated"
          showHero={false}
          showSearch
          showSortControls={false}
          title="Graduated"
          summary="Tokens that cleared the graduation threshold."
        />
      </Suspense>
      <Suspense fallback={<ExploreFallback label="Loading all tokens…" />}>
        <TokenDiscovery
          defaultPhase="curve"
          fixedPhase="curve"
          showHero={false}
          showSearch={false}
          showSortControls
          title="Explore"
          summary="New launches and tokens still moving toward graduation."
        />
      </Suspense>
    </div>
  );
}

function ExploreFallback({ label }: { label: string }) {
  return (
    <section className="workspace-panel explore-fallback" aria-busy="true">
      <p className="mono">{label}</p>
    </section>
  );
}

import Link from "next/link";
import { Hero } from "@/landing/hero";
import { LaunchStory } from "@/landing/launch-story";
import { LiveStrip } from "@/live/live-strip";

/** The interim home page until the new home lands (plan Task 4); the token list is on /explore. */
export function ExploreHub() {
  return (
    <div className="page-stack explore-hub">
      <Hero />
      <LaunchStory />
      <LiveStrip />
      <p className="explore-hub-link">
        <Link className="ui-button ui-button-secondary ui-button-md" href={"/explore" as never}>
          Explore launches
        </Link>
      </p>
    </div>
  );
}

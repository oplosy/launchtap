import { LiveStrip } from "@/live/live-strip";
import { WatchlistToaster } from "@/watchlist/star-button";
import { HomeFeed } from "./featured";

/** The home page: the live strip under the nav, then three card sections (sr-only h1). */
export function HomeView() {
  return (
    <div className="page-stack home">
      <LiveStrip />
      <h1 className="sr-only">Launchpad</h1>
      <HomeFeed />
      <WatchlistToaster />
    </div>
  );
}

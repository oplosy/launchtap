import Link from "next/link";
import { LiveStrip } from "@/live/live-strip";
import { WatchlistToaster } from "@/watchlist/star-button";
import { HomeFeed } from "./featured";

/** The home page: the live strip under the nav, a centred hero and three card sections. */
export function HomeView() {
  return (
    <div className="page-stack home">
      <LiveStrip />
      <section className="home-hero" aria-labelledby="home-title">
        <h1 id="home-title">Launch a token. Trade the curve.</h1>
        <p className="home-hero-summary">
          Fixed-supply launches on Robinhood Chain. Every token trades on a bonding curve until it
          graduates to Uniswap.
        </p>
        <div className="home-hero-actions">
          <Link className="ui-button ui-button-primary ui-button-lg" href="/create">
            Create a token
          </Link>
          <Link className="ui-button ui-button-secondary ui-button-lg" href={"/explore" as never}>
            Explore
          </Link>
        </div>
      </section>
      <HomeFeed />
      <WatchlistToaster />
    </div>
  );
}

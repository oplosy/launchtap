import Link from "next/link";

export default function NotFound() {
  return (
    <div className="page-stack">
      <section className="ui-state ui-error-state" role="status" aria-labelledby="not-found-title">
        <div>
          <p className="panel-kicker">404 · Route not found</p>
          <h1 id="not-found-title">That launch route does not exist.</h1>
          <p>Check the address or open Explore for the latest launches.</p>
          <Link className="ui-button ui-button-secondary ui-button-md" href="/explore">
            Return to Explore
          </Link>
        </div>
      </section>
    </div>
  );
}

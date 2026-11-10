import Link from "next/link";
import type { ReactNode } from "react";

/** The centred heading block every page opens with: one h1, an optional summary and actions. */
export function PageHeader({
  title,
  summary,
  actions,
}: {
  title: string;
  summary?: string;
  actions?: ReactNode;
}) {
  return (
    <header className="page-header">
      <h1>{title}</h1>
      {summary ? <p className="page-header-summary">{summary}</p> : null}
      {actions ? <div className="page-header-actions">{actions}</div> : null}
    </header>
  );
}

/** A page section with a centred heading and an optional "View all" link beneath it. */
export function Section({
  id,
  title,
  viewAllHref,
  viewAllLabel = "View all",
  children,
}: {
  id: string;
  title: string;
  viewAllHref?: string;
  viewAllLabel?: string;
  children?: ReactNode;
}) {
  const headingId = `${id}-title`;
  return (
    <section className="page-section" id={id} aria-labelledby={headingId}>
      <div className="page-section-head">
        <h2 id={headingId}>{title}</h2>
        {viewAllHref ? (
          <Link className="view-all-link" href={viewAllHref as never}>
            {viewAllLabel} <span aria-hidden="true">â†’</span>
          </Link>
        ) : null}
      </div>
      {children}
    </section>
  );
}

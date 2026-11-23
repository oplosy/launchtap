import Link from "next/link";
import type { ReactNode } from "react";
import { SectionMotif, type MotifName } from "./section-motif";

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

/**
 * A page section with a centred heading and an optional line-drawn motif above it. With `href` the
 * heading itself links to the section's full view.
 */
export function Section({
  id,
  title,
  motif,
  href,
  children,
}: {
  id: string;
  title: string;
  motif?: MotifName;
  href?: string;
  children?: ReactNode;
}) {
  const headingId = `${id}-title`;
  return (
    <section className="page-section" id={id} aria-labelledby={headingId}>
      <div className="page-section-head">
        {motif ? <SectionMotif name={motif} /> : null}
        <h2 id={headingId}>
          {href ? (
            <Link className="section-title-link" href={href as never}>
              {title}
            </Link>
          ) : (
            title
          )}
        </h2>
      </div>
      {children}
    </section>
  );
}

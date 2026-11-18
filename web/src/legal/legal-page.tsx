import { legalDocument, type LegalDocumentId } from "./documents";

/** Shared frame for the legal routes: title, effective date, and the draft notice. */
export function LegalPage({ id, children }: { id: LegalDocumentId; children: React.ReactNode }) {
  const document = legalDocument(id);
  return (
    <div className="page-stack docs-page">
      <article className="docs-article legal-article">
        <h1>{document.title}</h1>
        {document.status === "draft" ? (
          <p className="legal-draft" role="note">
            Draft, pending legal review. This text is not yet in force and will be replaced by
            counsel-approved terms before launch.
          </p>
        ) : (
          <p className="legal-effective">Effective {document.effectiveDate}</p>
        )}
        {children}
      </article>
    </div>
  );
}

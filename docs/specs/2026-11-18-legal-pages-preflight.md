# Pre-flight: legal pages, risk notice and the counsel release gate

Date: 2026-11-18. Source: gap G5 in `docs/audits/2026-11-18-project-gap-assessment.md` (no
Terms, Privacy or risk acknowledgement; no legal position). The owner delegated the decision on
2026-11-18.

## Problem

The web app has no Terms of Service or Privacy Policy route and nothing that makes a first-time
visitor see the risks. The legal text itself, the operating entity and the excluded
jurisdictions need a lawyer. Code cannot supply them, and placeholder text must never reach
production by accident.

## Locked decisions

1. **Build the mechanism now; counsel writes the text.**
   - `/terms` and `/privacy` render documents whose status lives in `web/src/legal/documents.json`
     (`draft` or `approved`, with an effective date).
   - A draft shows a visible "Draft, pending legal review" notice.
   - The privacy draft states only what the code really processes. The terms draft states the
     non-custodial facts and leaves the legal clauses to counsel.

   *Why:* the routes, links and gate can be reviewed and tested today. Swapping the text later
   is a content change only.

2. **The production release gate refuses drafts.** `validate-release.mjs --production`, which
   the Compose `preflight` container also runs, fails while any legal document is not
   `approved`.
   *Trade-off:* production cannot start until counsel signs off. That is intended, because the
   gap assessment names legal pages as a launch blocker.

3. **First-visit risk notice, non-blocking.**
   - A notice above the page content links to the risk section of the docs and to the terms.
     "I understand" hides it for a year.
   - It is a first-party `launchpad-risk-ack` cookie (SameSite=Lax), not localStorage, so the
     server renders the right state on the first paint (no layout shift). This matches the theme
     cookie.
   - It does not block reading or signing.

   *Alternative rejected:* a modal gate before any wallet action. Whether acknowledgement must
   block trading is a legal question for counsel, and a modal adds friction and e2e churn before
   that answer exists.

4. **No geo-blocking yet.** Excluded jurisdictions come from counsel (G5 owner step). An edge
   block belongs in the proxy or CDN (G9), not the app, and is added once the list exists.

5. **Risk content is not duplicated.** The docs page already has a "Risks and custody" section
   (`/docs#risk`); the notice and footer link to it.

## Out of scope

- The legal text, the operating entity and the jurisdiction list: owner and counsel.
- A cookie banner: the app sets only functional first-party cookies (theme, risk notice) and runs
  no third-party tracking (`docs/specs/2026-11-18-client-telemetry-preflight.md`).

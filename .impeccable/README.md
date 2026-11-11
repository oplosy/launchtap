# Impeccable evidence

This task uses a code-led Operate-mode workflow. The PNGs in `mocks/` are visual direction
references only; they are not pixel-fidelity acceptance targets. The final shell deliberately
translates the airline-ticket route idea into a dark, dense, trust-first operating surface.

The `review/` PNGs are the committed visual QA captures for the required 360px phone, tablet,
small-laptop, wide desktop, and reduced-motion states. The previous generated comp-diff report
and build-phase state were removed because they recorded a contradicted score and forced fidelity
gate, which must not be presented as acceptance evidence.

Task 4 discovery evidence is prefixed `task4-` and covers both `/` (Explore) and `/graduated`
at 360, 768, 1280, and 1440 pixels, plus reduced-motion captures at 1440 pixels. These images
were captured from the production build after commit `fe8ad51`; they intentionally show the
honest unavailable state because no reviewed public API configuration is present.

Frontend rebuild evidence is prefixed `rebuild-`. Route captures are named
`rebuild-<route>-<width>-<theme>.png` and cover every route (home, explore, graduated, forum,
leaderboard, analytics, docs, create, profile, notifications, token, not-found) at 360, 768,
1280, and 1440 pixels in the dark and light themes, plus `rebuild-<route>-reduced-motion.png`
at 1440 pixels with reduced motion. They were captured from a production build with no reviewed
public configuration, so data routes show their honest unavailable states; no capture overflowed
horizontally. Populated layouts come from the mocked live e2e specs (`rebuild-token-wide`,
`rebuild-token-scrolled`, `rebuild-token-sheet-mobile`, `rebuild-create-wide`,
`rebuild-forum-<project>`, `rebuild-leaderboard`, `rebuild-docs-wide`).

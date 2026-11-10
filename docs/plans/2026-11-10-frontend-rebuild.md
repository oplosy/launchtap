# Frontend Rebuild Implementation Plan

> **For agentic workers:** execute inline with superpowers:executing-plans (the owner forbids
> subagent-driven development in this repo). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rebuild the web presentation layer as one centred, calm, monochrome design system with a
new home, a new `/explore`, and recomposed pages, without changing data, wallet or security code.

**Architecture:** Tokens and primitives first (CSS custom properties in `web/app/styles/`, React
primitives in `web/src/components/`), then the shell, then routes one by one on
`task/frontend-rebuild`. Old token names stay aliased to new ones until the last task removes them,
so every intermediate commit renders and passes the gates.

**Tech Stack:** Next.js 16 (App Router, Turbopack), React 19, plain CSS with custom properties,
Geist / Geist Mono (`next/font/local`), Vitest, Playwright + axe.

**Spec:** `docs/specs/2026-11-10-frontend-rebuild-design.md`

## Global Constraints

- No new runtime or dev dependency; motion is CSS (plus `IntersectionObserver` for reveals).
- Do not change: `src/api/`, `src/wallet/` logic, `src/watchlist/use-watchlist.ts` logic,
  transaction flows, `src/security/headers.ts`, `next.config.ts` headers.
- Dark canvas `#000`; light canvas `#fff`; UI chrome monochrome; colour only via `--up`, `--down`,
  `--progress`. The lime accent is removed.
- Type: body 17/28, label 14/20 w500, section 22/28 w500, page 48/56 w500 −0.03em, hero
  clamp(56px, 9vw, 110px) / 1.0 w600–700 −0.045em; numbers Geist Mono tabular.
- Radii: pill `9999px` (buttons, chips, segments), media `4px`, input `12px`, container `20px`.
- Layout: centred 1200 px column, 720 px text measure, 640 px form measure; headings, summaries,
  tabs, section intros, empty states centred; row and cell contents start-aligned, numbers
  end-aligned.
- Motion: `--dur-fast 150ms`, `--dur-base 300ms`, `--dur-slow 640ms`, `--ease-out
  cubic-bezier(0.22, 1, 0.36, 1)`, `--ease-in-out cubic-bezier(0.6, 0, 0.4, 1)`; reduced motion
  removes translation and scaling.
- Keep the accessible names of controls the e2e suite drives wherever the control still exists.
- Every commit: `npm run format:check` (on changed files; Windows CRLF noise excepted), `lint`,
  `typecheck`, `test`. Each task's last step also runs `test:e2e` and `test:e2e:live`.
- Work on `task/frontend-rebuild`; commit per task with a conventional prefix.

## Review Focus

1. **Old links:** `/graduated`, `/?view=new`, `/?view=marketcap`, `/?view=watchlist` and old
   inbox/watchlist links must land on the equivalent `/explore` state, never on the new home with
   the parameter ignored. Pinned in Task 3.
2. **Keyboard on the mega menu and sheets:** Tab reaches every link, Escape closes and returns
   focus to the trigger, focus leaving closes it. Pinned in Task 2.
3. **Light theme contrast:** grey text tones and hairlines must stay AA on `#fff`, not only on
   `#000`. Pinned in Task 1 (token contrast unit test) and Task 9 (axe in light).
4. **Reduced motion:** reveals must not leave content at opacity 0 when `IntersectionObserver`
   never fires (reduced motion, print, no JS after hydration failure). Pinned in Task 1.
5. **Long, hostile token names and numbers** in centred headers and rows must truncate, not
   overflow at 360 px, and render as text. Pinned in Task 5 and Task 4.

---

### Task 1: Tokens, typography and primitives

**Files:**
- Modify: `web/app/styles/tokens.css`, `web/app/styles/base.css`, `web/app/styles/primitives.css`,
  `web/app/layout.tsx` (font variables only), `web/src/components/primitives.tsx`
- Create: `web/src/components/layout.tsx`, `web/src/components/reveal.tsx`,
  `web/src/components/contrast.ts`
- Test: `web/src/components/contrast.test.ts`, `web/src/components/reveal.test.tsx`,
  `web/src/components/primitives.test.ts`

**Interfaces:**
- Produces CSS tokens: `--canvas`, `--text`, `--text-2`, `--text-3`, `--hairline`,
  `--hairline-strong`, `--fill` (secondary button / input surface), `--fill-hover`, `--up`,
  `--down`, `--progress`, `--radius-pill`, `--radius-media`, `--radius-input`,
  `--radius-container`, `--measure-page`, `--measure-text`, `--measure-form`, `--gutter`, the
  motion tokens above, and type tokens `--type-body`, `--type-label`, `--type-section`,
  `--type-page`, `--type-hero`. Old names (`--surface`, `--line`, `--text-soft`, `--accent`, …)
  remain as aliases mapping to the new tokens; `--accent*` aliases map to `--text`/`--progress`.
- Produces `PageHeader({ title, summary?, actions? }: { title: string; summary?: string; actions?: ReactNode })` — centred `<header>` with an `h1`.
- Produces `Section({ title, viewAllHref?, viewAllLabel?, id?, children })` — centred `h2` and an
  optional "View all →" link (`viewAllLabel` defaults to `"View all"`); `aria-labelledby` on the
  section.
- Produces `Reveal({ children, delay? }: { children: ReactNode; delay?: number })`.
- Produces `contrastRatio(foreground: string, background: string): number` (hex in, WCAG ratio out).
- `Button` keeps its props; `variant` `primary` = white pill, `secondary` = `--fill` pill,
  `quiet` = text, `danger` = text in `--down`. `Tabs` gains a sliding underline.

- [ ] **Step 1: Write failing tests**
  - `contrast.test.ts`: for both themes' token values (import them from a
    `THEME_TOKENS` constant exported by `contrast.ts`), `contrastRatio(text, canvas) >= 7`,
    `contrastRatio(text2, canvas) >= 4.5`, `contrastRatio(text3, canvas) >= 4.5`,
    `contrastRatio(up, canvas) >= 3`, `contrastRatio(down, canvas) >= 3`.
  - `reveal.test.tsx`: renders children visible (no `data-hidden`) when
    `window.IntersectionObserver` is undefined; renders visible immediately when
    `matchMedia('(prefers-reduced-motion: reduce)')` matches.
  - `primitives.test.ts`: `PageHeader` renders one `h1` with the title; `Section` with
    `viewAllHref="/explore"` renders a link named `"View all"` to `/explore`.
- [ ] **Step 2:** Run `npx vitest run src/components` — expect FAIL (missing modules).
- [ ] **Step 3: Implement.** `THEME_TOKENS` in `contrast.ts` is the single source of the six
  colour values per theme that the test checks; `tokens.css` uses the same hex values (comment
  points at `contrast.ts`). `Reveal` sets `data-reveal="pending"` only after mount when an
  observer exists and motion is allowed, and clears it on first intersection; CSS hides only
  `[data-reveal="pending"]`. Restyle primitives in `primitives.css` per Global Constraints.
- [ ] **Step 4:** Run `npx vitest run` — expect PASS; `npm run lint`, `npm run typecheck` clean.
- [ ] **Step 5:** Run `npm run test:e2e` and `npm run test:e2e:live` — expect all pass (pages
  still use old class names through aliases).
- [ ] **Step 6: Commit** `feat(web): add rebuild design tokens and primitives`.

### Task 2: Shell — nav, mega menu, mobile sheet, footer, theme

**Files:**
- Modify: `web/src/components/app-shell.tsx`, `web/app/styles/shell.css`
- Create: `web/src/components/mega-menu.tsx`, `web/src/components/site-footer.tsx`
- Test: `web/src/components/mega-menu.test.tsx`, `web/e2e/shell.spec.ts`

**Interfaces:**
- Consumes: Task 1 tokens, `Button`, `Sheet`.
- Produces: `NAV_LINKS: ReadonlyArray<{ href: string; label: string }>` =
  Explore `/explore`, Forum `/forum`, Leaderboard `/leaderboard`, Analytics `/analytics`,
  Docs `/docs`. `MegaMenu({ open, onClose, triggerRef })` with groups
  New `/explore?view=new`, Trending `/explore?view=trending`, Graduated `/explore?view=graduated`
  and resources Watchlist `/explore?view=watchlist`, Leaderboard, Docs.
- Keeps accessible names: `"Open home"` (wordmark link), `"Connect wallet"`, the notification bell
  `/^Notifications/`, command palette shortcut.

- [ ] **Step 1: Write failing tests**
  - `mega-menu.test.tsx`: Escape calls `onClose` and focuses `triggerRef.current`; focus moving
    outside calls `onClose`.
  - `e2e/shell.spec.ts` (small-laptop and mobile): desktop shows the five nav links and
    "Create"; hovering or focusing Explore + Enter opens a region named `"Explore menu"` containing
    a link `"Graduated"` to `/explore?view=graduated`; Escape closes it and focus returns to
    Explore; on 360 px a button `"Open menu"` opens a dialog with the same links; the footer has
    links Explore, Create, Forum, Leaderboard, Analytics, Docs and a theme selector.
- [ ] **Step 2:** Run the new tests — expect FAIL.
- [ ] **Step 3: Implement** the three-zone centred nav (wordmark start, links centred, actions
  end), the full-width mega menu over a blurred dimmed backdrop (`backdrop-filter: blur(12px)`,
  opacity + 4 px shift over `--dur-base`), the mobile sheet and the footer. Remove the shell's
  icon nav for desktop; keep icons out of text links.
- [ ] **Step 4:** Run vitest, `test:e2e`, `test:e2e:live` — expect PASS. Update existing specs only
  where they clicked the old nav (`notifications.spec.ts` uses `"Connect wallet"` — must still pass
  unchanged).
- [ ] **Step 5: Commit** `feat(web): rebuild shell navigation, mega menu and footer`.

### Task 3: `/explore` and legacy redirects

**Files:**
- Create: `web/app/explore/page.tsx`, `web/src/discovery/legacy-routes.ts`
- Modify: `web/app/graduated/page.tsx`, `web/app/page.tsx` (redirect branch only),
  `web/src/discovery/query-state.ts`, `web/src/discovery/token-discovery.tsx`,
  `web/app/styles/discovery.css`, `web/src/notifications/inbox-view.tsx` (watchlist link),
  `web/scripts/check-budgets.mjs` + `web/performance-budgets.json` (add `/explore`)
- Test: `web/src/discovery/legacy-routes.test.ts`, `web/src/discovery/query-state.test.ts`,
  `web/e2e/explore.spec.ts`

**Interfaces:**
- Produces: `EXPLORE_VIEWS = ["trending", "new", "graduated", "watchlist"] as const`;
  `decodeExploreView` maps unknown → `"trending"`.
- Produces: `legacyExploreTarget(params: URLSearchParams): string | null` — returns null when
  `view` is absent; otherwise `/explore?view=<v>` with `marketcap` → `view=trending&sort=market_cap`
  and `volume` → `view=trending&sort=volume_24h`; other existing params (`q`, `age`) carried over.
- `/explore` renders `PageHeader` "Explore", centred view tabs, one centred control row
  (Search, Sort, Age, list/grid toggle), then `TokenDiscovery` driven by the view (`graduated` →
  phase `graduated`; others → phase `curve`; `watchlist` → the existing watchlist tab).

- [ ] **Step 1: Write failing tests**
  - `legacy-routes.test.ts`: `?view=new` → `/explore?view=new`; `?view=marketcap&q=dog` →
    `/explore?view=trending&sort=market_cap&q=dog`; `?view=volume` →
    `/explore?view=trending&sort=volume_24h`; `?view=bogus` → `/explore?view=trending`; no `view`
    → `null`.
  - `query-state.test.ts`: update to the four views; legacy view values decode to `"trending"`.
  - `e2e/explore.spec.ts` (mocked `/v1/**` like `token-comments.spec.ts`): `/graduated` lands on
    `/explore?view=graduated` with the Graduated tab selected; `/?view=watchlist` lands on
    `/explore?view=watchlist`; `/?view=marketcap` lands on `/explore?view=trending&sort=market_cap`;
    switching tabs updates the URL and Back restores the previous tab; list/grid toggle keeps the
    same tokens; a 120-character token name truncates without horizontal overflow at 360 px.
- [ ] **Step 2:** Run them — expect FAIL.
- [ ] **Step 3: Implement.** `/graduated` uses `permanentRedirect`. `app/page.tsx` reads
  `searchParams` and calls `redirect(legacyExploreTarget(...))` when non-null (home content comes
  in Task 4; until then it renders the current `ExploreHub`). Move the explore list rows to the
  spec's ~72 px hairline rows and the 3–4 column grid.
- [ ] **Step 4:** Update e2e specs that open the token list on `/` (`live-strip`, `diagnostic`,
  `release-hardening`, `task6-anvil`, `wallet-page`, `accessibility`) to `/explore` where they test
  the list, and add `/explore` to the accessibility route list. Run vitest, `test:e2e`,
  `test:e2e:live`, `node --test scripts/check-budgets.test.mjs` — expect PASS.
- [ ] **Step 5: Commit** `feat(web): add /explore with view tabs and legacy redirects`.

### Task 4: Home

**Files:**
- Create: `web/src/home/home-view.tsx`, `web/src/home/featured.tsx`, `web/app/styles/home.css`
- Modify: `web/app/page.tsx`, `web/app/globals.css` (import `home.css`, drop `landing.css`)
- Delete: `web/src/landing/*` and `web/app/styles/landing.css` once unused;
  `web/src/discovery/explore-hub.tsx`
- Test: `web/e2e/home.spec.ts`

**Interfaces:**
- Consumes: `PageHeader`-style centred hero (own markup, `h1`), `Section`, `Reveal`, existing data
  hooks/fetchers from `src/discovery/highlights.tsx` (king of the hill, trending), the forum feed
  client call used by `/forum`, `LiveStrip`.
- Hero copy: `h1` "Launch a token. Trade the curve." and buttons "Create a token" (`/create`,
  primary) and "Explore" (`/explore`, secondary). Sections: "Trending" (`viewAllHref`
  `/explore?view=trending`), "Just graduated" (`/explore?view=graduated`), "From the forum"
  (`/forum`).

- [ ] **Step 1: Write failing e2e** (`home.spec.ts`, mocked API): the hero heading and both
  buttons are visible and centred (button centre x within 2 px of viewport centre ± gap); the
  featured band shows the king-of-the-hill token name; each section has a "View all" link with the
  hrefs above; with every list endpoint returning empty items each section shows its empty state,
  not a blank area; token names render as text (`<b>` in a name shows literally).
- [ ] **Step 2:** Run — expect FAIL.
- [ ] **Step 3: Implement** the hero, featured band (8 + 4 columns; stacked below 1024 px), the
  three sections with `Reveal`, and remove the old landing/hub code.
- [ ] **Step 4:** Run vitest, `test:e2e`, `test:e2e:live` — expect PASS.
- [ ] **Step 5: Commit** `feat(web): rebuild the home page`.

### Task 5: Token page and trade card

**Files:**
- Modify: `web/src/token/token-detail.tsx` (layout only), `web/app/styles/token.css`,
  `web/app/styles/transactions.css`, `web/app/styles/live.css`, trade panel component(s) under
  `web/src/token/` (markup/classes only), `web/src/comments/comment-panel.tsx` (classes only)
- Test: `web/e2e/token-layout.spec.ts`; existing `token-detail`, `token-comments`, `signed-in`,
  `task6-transactions` specs

**Interfaces:**
- Consumes: Task 1 primitives (`Tabs` for Trades · Holders · Comments, `Sheet`, `Dialog`).
- Keeps every accessible name the transaction and comment specs use (Buy/Sell controls, "Post",
  "Comments" region, dialog titles).
- Mobile (< 1024 px): a fixed bottom bar with buttons "Buy" and "Sell" opening a `Sheet` titled
  "Trade" that contains the same trade card.

- [ ] **Step 1: Write failing e2e** (`token-layout.spec.ts`, mocked API like
  `token-comments.spec.ts`): the centred header shows name, symbol, price; at 1280 px the trade
  card stays visible after scrolling 1500 px (sticky); at 360 px the "Buy" bottom-bar button opens
  a dialog named "Trade"; a 120-character hostile name (`<img src=x onerror=…>` text) renders as
  text and causes no horizontal overflow; a price change hint flashes and settles (class removed
  after 600 ms) and does not flash under reduced motion.
- [ ] **Step 2:** Run — expect FAIL.
- [ ] **Step 3: Implement** the centred header, 8 + 4 body, borderless chart with text-tab ranges,
  stats row, thin progress bar (`--progress`), tabs, About, sticky trade card (20 px radius, pill
  segment, chips, full-width primary action), mobile bar + sheet, dialog restyle.
- [ ] **Step 4:** Run vitest, `test:e2e`, `test:e2e:live` (token-detail, token-comments,
  signed-in, task6-transactions must pass unchanged) — expect PASS.
- [ ] **Step 5: Commit** `feat(web): rebuild the token page and trade card`.

### Task 6: Create

**Files:**
- Modify: create form component(s) under `web/src/token/` or `web/src/create/` used by
  `web/app/create/page.tsx` (layout/classes only), `web/app/styles/create.css`
- Test: existing create flows in `task6-transactions.spec.ts`; `web/e2e/create-layout.spec.ts`

**Interfaces:**
- Section headings with numbers "01 Image", "02 Name and symbol", "03 Description and links",
  "04 Initial buy"; primary action keeps its current accessible name.

- [ ] **Step 1: Write failing e2e** (`create-layout.spec.ts`): the form column is ≤ 640 px wide and
  centred; the four numbered section headings appear in order; focusing an input changes its
  border colour (computed style differs from unfocused).
- [ ] **Step 2:** Run — expect FAIL.
- [ ] **Step 3: Implement** the single centred column, numbered sections, 12 px inputs, summary
  row, full-width action; reuse Task 5 dialog styles.
- [ ] **Step 4:** Run vitest, `test:e2e`, `test:e2e:live` — expect PASS.
- [ ] **Step 5: Commit** `feat(web): rebuild the create flow layout`.

### Task 7: Forum, Leaderboard, Analytics

**Files:**
- Modify: `web/app/forum/page.tsx`, `web/src/forum/*` (markup/classes), `web/app/leaderboard/page.tsx`,
  `web/app/analytics/page.tsx`, `web/app/styles/pages.css`
- Test: existing `forum.spec.ts`, `wallet-page.spec.ts` (leaderboard), accessibility spec

- [ ] **Step 1: Write failing e2e** additions in `forum.spec.ts`: the New · Hot · Top tab list is
  horizontally centred; each post row is one link target to the token. In `wallet-page.spec.ts`:
  leaderboard 7d · 30d · All tabs centred and rank numbers end-aligned in their column.
- [ ] **Step 2:** Run — expect FAIL.
- [ ] **Step 3: Implement** with `PageHeader`, centred tabs, Research-index rows for the forum,
  borderless leaderboard table, analytics KPIs + borderless charts (colour only in data).
- [ ] **Step 4:** Run vitest, `test:e2e`, `test:e2e:live` — expect PASS.
- [ ] **Step 5: Commit** `feat(web): rebuild forum, leaderboard and analytics pages`.

### Task 8: Wallet, profile, notifications, docs, error and empty states

**Files:**
- Modify: `web/app/wallet/[address]/page.tsx` + `web/src/portfolio/*` (markup/classes),
  `web/app/profile/page.tsx`, `web/src/profile/*.tsx` (markup/classes),
  `web/app/notifications/page.tsx`, `web/src/notifications/inbox-view.tsx` (markup/classes),
  `web/app/docs/page.tsx`, `web/app/error.tsx`, `web/app/not-found.tsx`,
  `web/app/leaderboard/error.tsx`, `web/app/styles/pages.css`
- Test: existing `wallet-page`, `public-profile`, `notifications`, `signed-in` specs;
  `web/e2e/docs.spec.ts`

- [ ] **Step 1: Write failing e2e** `docs.spec.ts`: at 1280 px the text column is ≤ 720 px and
  centred and the table of contents is visible beside it; at 360 px the contents collapse into a
  button "Contents" above the text; `/does-not-exist` shows a centred heading and one link home.
- [ ] **Step 2:** Run — expect FAIL.
- [ ] **Step 3: Implement** the account pages as hairline-separated sections, the inbox rows
  (unread: white text + dot), docs reading layout, and the centred empty/error/not-found states.
- [ ] **Step 4:** Run vitest, `test:e2e`, `test:e2e:live` (signed-in, public-profile,
  notifications unchanged) — expect PASS.
- [ ] **Step 5: Commit** `feat(web): rebuild account, docs and status pages`.

### Task 9: Consistency, alias removal, visual QA, PR

**Files:**
- Modify: `web/app/styles/tokens.css` (delete old aliases), any stylesheet still using them,
  `web/e2e/accessibility.spec.ts` (run in light and dark), `.impeccable/README.md`
- Create: `.impeccable/review/rebuild-*.png`

- [ ] **Step 1:** Extend `accessibility.spec.ts` to run every route twice, with the theme cookie set
  to `light` and to `dark`; expect no serious/critical axe violations.
- [ ] **Step 2:** Delete the alias block; `rg -- "--(surface|line|text-soft|text-faint|accent)" web/app web/src`
  returns nothing.
- [ ] **Step 3:** Run vitest, lint, typecheck, `test:e2e`, `test:e2e:live`,
  `node --test scripts/check-budgets.test.mjs` and a production build with
  `NEXT_PUBLIC_E2E_FIXTURE=0` — expect PASS and budgets within limits.
- [ ] **Step 4:** In the browser, check every route at 360, 768, 1280, 1440 px in dark and light
  and with reduced motion; save captures as `.impeccable/review/rebuild-<route>-<width>-<theme>.png`;
  fix any overflow, contrast or alignment defect found and re-run Step 3.
- [ ] **Step 5: Commit** `chore(web): remove legacy tokens and record rebuild visual QA`, push
  `task/frontend-rebuild`, open the PR into `dev`, and update `backlog.md` and `docs/ROADMAP.md`.

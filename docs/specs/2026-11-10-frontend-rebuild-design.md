# Frontend rebuild: design

> **2026-11-12:** `.impeccable/` was removed from the repository; review captures are now
> written to `web/test-results/screenshots/` (gitignored). References below are historical.

- **Date:** 2026-11-10
- **Status:** draft for owner review
- **Scope:** full rebuild of the web presentation layer (`web/`): visual language, information
  architecture, navigation and every page's composition.
- **Reference:** the visual language of openai.com (composition, colour discipline, hairlines,
  spacing, corner radii, typography, navigation and motion), studied 2026-11-10 on the home page,
  `/research/index/`, the products mega menu and the ChatGPT overview page.

## Goal

Make Launchpad look and feel like a calm, premium, editorial product instead of a dense dashboard,
by adopting the reference's design language. Success: every route uses one design system; the
site reads as minimal and confident in both themes; no data, wallet or security behaviour changes;
all existing gates stay green.

## Non-goals

- Copying the reference's features or content. Its prompt-box home is a product function, not a
  style, and is explicitly out. No OpenAI logo, imagery, copy or proprietary font.
- Changing the data layer, API client, wallet/Privy logic, transaction lifecycle, CSP or security
  headers.
- New runtime dependencies (no animation library, no new font package).

## Decisions

| Topic | Decision | Rationale |
|---|---|---|
| Depth | Full rebuild including IA | Owner choice |
| Theme | Pure-black dark default plus a matching light theme; toggle stays | Owner choice; dark matches the reference |
| Colour | UI chrome monochrome; colour only where it carries data meaning | Reference is monochrome; a trading screen still needs buy/sell and change colour |
| Font | Geist (already self-hosted), retuned | OpenAI Sans is proprietary; Geist is the closest open match and needs no dependency |
| Delivery | Design system first, then pages, on `task/frontend-rebuild`, merged to `dev` by one PR | `dev` never shows a half-old, half-new site |

## 1. Visual system

**Colour tokens.** Dark: canvas `#000`; surfaces are the canvas itself; sections are separated by
hairlines (`rgba(255,255,255,.08)` default, `.12` strong); three text tones: primary white,
secondary grey, faint grey. Light theme mirrors it: white canvas, near-black text, black-alpha
hairlines. Data colours only: `--up` (green) and `--down` (red) for buy/sell and price change, and
one `--progress` colour for curve progress. The lime accent is removed. All text pairs meet WCAG AA.

**Typography (Geist / Geist Mono).**

| Role | Size / line height | Weight | Tracking |
|---|---|---|---|
| Body | 17 / 28 px | 400 | −0.01em |
| Label, meta, button | 14 / 20 px | 500 | normal |
| Section heading | 22 / 28 px | 500 | −0.01em |
| Page heading | 48 / 56 px | 500 | −0.03em |
| Hero | clamp 56–110 px, line height 1.0 | 600–700 | −0.045em |
| Numbers | Geist Mono, tabular figures | 400–500 | normal |

Hierarchy comes from spacing, not from many sizes.

**Radii.** Buttons, chips, segmented controls: fully rounded (pill). Images and media cards: 0–4 px.
Inputs: 12 px. Large containers (trade card, dialogs, sheets): 20 px.

**Buttons.** Primary: white pill, black text. Secondary: dark grey pill. Tertiary: text only.
All 14 px / 500, height 36 px (40 px in forms and the trade card). No shadows, no gradients.

**Layout: centred composition.** Everything sits on a centred axis: a centred 1200 px content
column (12-column grid, `clamp(16px, 4vw, 40px)` gutter), with narrower centred measures for
reading and forms (720 px text, 640 px forms). Heroes, page headings, their one-line summaries,
section intros, empty states and the primary actions under a heading are centre-aligned. Inside a
list row or table cell, text stays start-aligned and numbers end-aligned, because ragged centred
rows are hard to scan; the rows themselves span the centred column. 96–160 px vertical rhythm
between sections. Cards have no border and no shadow; tables have row hairlines only.

## 2. Information architecture and navigation

**Routes.**

| Route | Change |
|---|---|
| `/` | New home (see §3) |
| `/explore` | New. Hosts today's token list; tabs Trending · New · Graduated · Watchlist in the existing `?view=` parameter; market cap and volume ordering move to the Sort control |
| `/graduated` | Permanent redirect to `/explore?view=graduated` |
| `/?view=…` | Redirect to `/explore` with the same view (legacy `marketcap`/`volume` become `view=trending` with the matching sort); in-app links updated |
| `/token/[address]`, `/create`, `/forum`, `/leaderboard`, `/analytics`, `/docs`, `/wallet/[address]`, `/profile`, `/notifications` | Kept; recomposed |

**Desktop nav.** Three zones on the centred column: the "Launchpad" wordmark at the start, text
links Explore, Forum, Leaderboard, Analytics, Docs centred (14–15 px; active white, others grey),
actions at the end. Hovering or focusing Explore opens a
full-width mega menu over a dimmed, blurred page; its content sits on the centred column: large
links (New, Trending, Graduated) in one group, a small resources list (Watchlist, Leaderboard,
Docs) beside it. Action zone: search icon (the
existing command palette, ⌘K), the notification bell when signed in, "Create" (dark pill),
"Connect" or the account (white pill). The mega menu is keyboard operable and closes on Escape and
on focus leaving it.

**Mobile nav.** Wordmark, "Connect", menu icon. The menu opens a full-screen sheet of large links.

**In-site direction.** Each home section has a centred heading with a centred "View all →" link
beneath it; tab bars are centred and live in the URL; whole list rows are links and hover changes
text tone, not background; centred 48 px page headings; no breadcrumbs.

**Footer.** Multi-column link list centred on the content column: Product (Explore, Create,
Forum), Data (Leaderboard, Analytics), Resources (Docs), Account; a bottom row with the theme
selector and a "Non-custodial" note.

## 3. Page compositions

Shared: every page opens with a centred 48 px heading and one centred grey line of summary;
generous spacing; content on the centred column; borderless tables with row hairlines and
end-aligned mono numbers.

- **Home `/`.** Centred hero: short headline and, centred beneath it, two buttons ("Create a token"
  primary, "Explore" secondary). Featured band on the centred column: large king-of-the-hill card
  (8 columns) beside two or three smaller items (4 columns). Then sections "Trending", "Just
  graduated", "From the forum": centred heading and "View all →", hairline-separated rows beneath.
  Footer.
- **Explore `/explore`.** Centred heading and centred text tabs; Filter and Sort and the
  list/grid toggle on one centred control row beneath. List rows
  (~72 px): image, name and symbol, market cap, 24 h change, progress bar, age. Grid: 3–4 columns,
  4 px image radius, name and one meta line beneath. "Load more" as a secondary pill.
- **Token `/token/[address]`.** Centred header: image (4 px radius) above the name (48 px), symbol
  and phase; beneath, centred, price and 24 h change, then watch star and share. Body 8 + 4 columns
  on the centred column. Left: borderless chart with
  text-tab ranges; stats row (market cap, liquidity, volume, holders: grey label over large number);
  thin curve progress bar (the one `--progress` use); text tabs for Trades · Holders · Comments;
  About (description, links, contract). Right: sticky trade card (20 px radius, pill Buy/Sell
  segment, large amount input, preset chips, grey quote rows, full-width white action). Dialogs:
  centred, 20 px radius, blurred backdrop. Mobile: chart → stats → tabs, with a bottom "Buy / Sell"
  bar that opens a full-screen sheet.
- **Create `/create`.** Single centred column, max 640 px; numbered sections (01 Image, 02 Name and symbol,
  03 Description and links, 04 Initial buy); 12 px inputs with hairline border that turns white on
  focus; summary row and full-width primary action; same confirmation and status dialogs as trading.
- **Forum `/forum`.** Research-index style rows: token and time on the left, comment in the middle,
  Trade and Discuss links on the right; centred New · Hot · Top text tabs; "Biggest launches" as a plain
  list in the side column.
- **Leaderboard `/leaderboard`.** Centred 7d · 30d · All text tabs; borderless table; large grey rank,
  white wallet or username.
- **Analytics `/analytics`.** Four large-number KPIs, then borderless charts; colour only in data.
- **Wallet `/wallet/[address]`.** Address or username heading; totals as four large numbers;
  positions table.
- **Profile `/profile`.** Sections (Account, Username, Linked wallets, Claims and refunds)
  separated by hairlines, no cards.
- **Notifications `/notifications`.** Row list; unread rows white with a dot, read rows grey.
- **Docs `/docs`.** Reading layout: centred 720 px column of 17/28 body text with a centred
  heading; the table of contents sits in the margin beside it on wide screens and collapses into a
  centred menu above the text below 1024 px.
- **Empty, error, loading.** Centred short title, one sentence, at most one button. Skeletons are
  flat grey blocks with a gentle opacity pulse, no shimmer.

## 4. Motion

CSS only. Calm and short; no bounce, spring or perpetual decorative motion.

| Token | Value | Use |
|---|---|---|
| `--dur-fast` | 150 ms | Hover tone changes |
| `--dur-base` | 300 ms | Menus, tabs, button states |
| `--dur-slow` | 600–700 ms | Reveals, large transitions |
| `--ease-out` | `cubic-bezier(0.22, 1, 0.36, 1)` | Entering motion |
| `--ease-in-out` | `cubic-bezier(0.6, 0, 0.4, 1)` | Two-way transitions |

- Section reveal on scroll: opacity 0 → 1 with an 8 px rise, once per element, light stagger.
- Mega menu: backdrop blur and opacity together, content shifts 4 px.
- Tabs: the active underline slides to the new tab.
- Dialogs: blurred backdrop, box scales 0.98 → 1.
- Live data: a new trade row fades in with a brief tone highlight; a changed price flashes
  `--up`/`--down` for 600 ms.
- Route transition: the existing `route-transition` stays an opacity fade.
- `prefers-reduced-motion: reduce`: no translation or scaling, opacity changes are instant.

## 5. Delivery and verification

**Branch.** `task/frontend-rebuild` from `dev`; one PR back into `dev`; required checks green before
merge.

**Order.** (1) tokens, typography and primitives (button, input, tabs, dialog, sheet, table row,
empty state); (2) shell: nav, mega menu, mobile sheet, footer, theme selector; (3) `/explore` and the
redirects; (4) home; (5) token page and trade card; (6) create; (7) forum, leaderboard, analytics;
(8) wallet, profile, notifications, docs; (9) consistency, mobile and visual QA pass.

**Unchanged contracts.** Data fetching, API client, wallet and Privy logic, transaction flows, CSP
and security headers. Accessible names of controls the e2e suite drives are kept where the control
still exists.

**Gates per step.** Web typecheck, lint, `format:check`, vitest, `test:e2e`, `test:e2e:live`.
Specs whose routes or IA change (Explore, `/graduated`, watchlist) are updated, and redirects get
new e2e coverage. The axe accessibility spec covers every route including `/explore`. Performance
budgets (`scripts/check-budgets.mjs`) hold; the budget's route list is updated for `/explore`.

**Visual QA.** Each page checked in a browser at 360, 768, 1280 and 1440 px, in dark and light,
and with reduced motion; captures stored under `.impeccable/review/`.

**Accessibility.** Visible focus ring on every interactive element, AA contrast in both themes,
everything keyboard operable (mega menu, sheets, dialogs, tabs).

## Risks

- **Density versus calm.** Traders need numbers at a glance; the editorial spacing must not hide
  them. Mitigation: the token page and Explore keep compact mono numbers inside the calm frame.
- **E2E churn.** Selector changes can mask regressions. Mitigation: keep accessible names, change
  specs only where the IA changed, and review each spec diff.
- **Scope size.** Nine steps across every route. Mitigation: the step order above; each step ends
  green and committed on the task branch so work can stop at any boundary.

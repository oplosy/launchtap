# Launchpad Design DNA

<!-- impeccable:design-schema 1 -->

## Direction contract

This is an Operate-mode application shell translated from an airline ticket wallet: the
interface records a token's journey as a sequence of explicit, addressable states. Dark navy
surfaces are the night-flight context. Bone panels and red validation marks are reserved for
high-signal actions and state changes. Carbon-violet is the audit ink used for supporting data.

The primary remembered move is the **route strip**: a restrained line between shell navigation
and page content that makes the launch-to-liquidity lifecycle legible without inventing live
values. Motion is limited to route transition and state feedback, and collapses under reduced
motion. This direction is self-authored under the user's explicit creative delegation. The
`pons.family` reference was inaccessible on 2026-09-10, so no external visual pixels or copy are
used.

The early generated comps were reference-only direction studies, not pixel-fidelity acceptance
targets, and were removed from the repository on 2026-11-12 (they remain in git history). The
shipped shell is intentionally code-led: its dark operating surface, semantic empty states, and
restrained route motion take precedence over literal light ticket artwork. No comp-diff score is
treated as a pass claim; the evidence is the automated gates. Playwright specs write their review
captures to `web/test-results/screenshots/` (gitignored, cleared on every run).

Dial values: `DESIGN_VARIANCE 5`, `MOTION_INTENSITY 4`, `VISUAL_DENSITY 7`. The lower variance keeps
an application shell dependable; density supports scan-heavy crypto operations; motion remains
functional and interruptible.

## Design system

### Color

| Token | Value | Role |
| --- | --- | --- |
| `--color-canvas` | `#08111D` | page canvas and navigation ground |
| `--color-surface` | `#0E1B2A` | elevated shell surfaces |
| `--color-surface-strong` | `#14263A` | focused panels and active navigation |
| `--color-line` | `#263B50` | dividers and quiet boundaries |
| `--color-bone` | `#F3F0E8` | primary text and ticket stock |
| `--color-muted` | `#AAB7C5` | secondary text and helper copy |
| `--color-red` | `#E45756` | primary action, failure, void state |
| `--color-red-soft` | `#F3A4A0` | action text on dark surfaces |
| `--color-violet` | `#9B8AFB` | audit metadata and supporting links |
| `--color-green` | `#82D9A2` | mined/indexed/safe confirmation states |
| `--color-amber` | `#E7B86A` | stale/provisional/unavailable attention |

The accent lock is red. Violet, green, and amber are semantic status roles only. No gradients,
glows, or invented market values are used.

### Typography

- Display and interface headings: `Space Grotesk`, weight 600-700, tight tracking `-0.025em`.
- Body and controls: `IBM Plex Sans`, weight 400-600, line-height 1.5.
- Amounts, addresses, timestamps, and state labels: `IBM Plex Mono`, weight 500-600.
- Type scale: 0.6875rem labels, 0.8125rem metadata, 0.9375rem body, 1.125rem section, 1.5rem
  page title, 2.5rem display maximum.
- Font loading uses `next/font`; no remote stylesheet links.

### Spacing, layout, shape, elevation

- Spacing base: 4px; operational rhythm uses 8, 12, 16, 24, 32, and 48px.
- Content max width: 1440px with responsive gutters `16px / 24px / 40px`.
- Desktop shell: fixed 248px navigation rail plus flexible content. Tablet collapses to a compact
  top bar. Mobile uses a sticky bottom navigation with four high-frequency destinations and a full
  menu sheet for the complete six-route navigation set.
- Shape rule: 10px panels, 8px controls, 999px status badges only. No nested card stacks.
- Elevation uses one quiet border plus a tinted shadow only when hierarchy requires it.
- Focus ring: 2px `#F3F0E8` outer ring with 3px offset on the navy canvas.

### Motion and interaction

- Route transitions use a 180ms opacity/translate reveal to show state change.
- Buttons use a 120ms transform/opacity press response; no `transition: all`.
- Loading uses structural skeleton shimmer only when motion is allowed.
- `prefers-reduced-motion: reduce` disables animation and keeps all content visible.
- No scroll hijack, continuous pointer loops, WebGL, generated media, or decorative motion.

## Design style

- Mood: precise, watchful, calm under risk.
- Visual language: jet-age travel ephemera translated into a digital operations console. Coupon
  tabs, punched corners, ruled rows, and route marks organize real information rather than act as
  decoration.
- Composition: strong left alignment, one clear content column, short ticket-like metadata rows,
  and sparse red state marks. Density comes from hierarchy and ruled grouping, not tiny text.
- Voice: plain, direct, non-custodial, explicit about what is estimated, submitted, mined, indexed,
  safe, finalized, stale, or unavailable.
- Image stance: no shell imagery is required. Token images later use the safe image primitive and
  are treated as attacker-controlled content with a deterministic fallback.

## Visual effects

```json
{
  "overview": {
    "enabled": true,
    "effect_intensity": "low",
    "performance_tier": "lightweight",
    "composite_notes": "Ruled route strip and ticket-edge geometry are CSS structure, not imagery."
  },
  "route_transition": {
    "enabled": true,
    "technology": "motion/react",
    "purpose": "communicate navigation state change",
    "duration_ms": 180,
    "reduced_motion": "instant"
  },
  "state_feedback": {
    "enabled": true,
    "technology": "CSS transform and opacity",
    "purpose": "acknowledge press, loading, and transaction-state changes",
    "duration_ms": 120,
    "reduced_motion": "none"
  },
  "decorative_effects": {
    "enabled": false,
    "reason": "Operate shell prioritizes scanability and contract truth."
  }
}
```

## Shared semantics

The shell uses canonical labels such as `Estimated`, `Submitted`, `Mined`, `Indexed`, `Safe`,
`Finalized`, `Stale`, `Unavailable`, and `Failed`. Every transaction surface includes the shared
non-custodial statement and risk warning. Missing configuration is shown as unavailable instead of
being replaced by sample market data.

## Responsive contract

- 360px: no horizontal overflow; bottom navigation has 44px minimum targets; content gutters 16px;
  primary action remains visible before secondary details.
- Tablet: top navigation and two-column content where real data exists; no fixed rail.
- Small laptop: 248px rail, content max 1120px, no clipped action labels.
- Wide desktop: 248px rail, content max 1440px, restrained whitespace and stable reading measure.

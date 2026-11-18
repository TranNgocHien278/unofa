# UNOFA design

Direction: rubber-hose cel animation. Every game object is a cel: flat fill, thick ink outline, moves with squash and stretch. Not a green-felt casino, not glossy casual-game buttons. Brief and contract: `.impeccable/surfaces/web-src.md`.

## Tokens (`web/src/styles/tokens.css`)

| Role | Token | Value |
| --- | --- | --- |
| Card colours (spec) | `--card-red/yellow/green/blue` | `#d7263d` `#f2b500` `#2a9d4a` `#1b6ca8` |
| Ink / paper | `--ink` `--paper` | `#101010` `#fafafa` |
| Shell | `--sun` | `oklch(0.85 0.155 86)` |
| Floor tint per active colour | `--floor-*` | light oklch washes of each card colour |
| Danger / good | `--alert` `--good` | `oklch(0.52 0.19 22)` `oklch(0.47 0.12 150)` |
| Outline | `--stroke` | `3px` |
| Radius | `--r-card` `--r-ctl` `--r-panel` | `10px` `14px` `24px` |
| Type | `--font-display` `--font-body` | Coiny (headings, numbers on cards) / Be Vietnam Pro |
| Motion | `--ease-out` | `cubic-bezier(0.22, 1, 0.36, 1)` |

Rules:

- Colour is never the only signal. Each card colour has its own shape: red circle, yellow triangle, green square, blue diamond. The shape appears on the card face, the corner, the colour picker and the chosen-colour badge.
- Lifted things leave an oval contact shadow (`.ground-shadow`) on the ground. No drop shadows on flat panels.
- States are stamped, not hidden: READY, OUT, UNO, REPAID, SEIZED, Live, Cancelled (`.stamp`).
- Destructive actions (leave room, forfeit, sign out, delete account) sit in a `.danger-zone` after a dashed rule, away from the primary action, and always confirm.

## Layout

- Phone first, 390x844 is the design viewport. Desktop centres content at 1040px max.
- Table rows top to bottom: strip (menu, pot, direction, timer) | opponent seats | stage (turn text, draw pile, discard) | action row | hand. Actions and hand sit in the bottom third for thumb reach.
- Hand overlap is a container query: cards overlap just enough to fit the width (between 30% and 70% of a card); past 70% the hand scrolls.
- Mobile nav is a bottom tab bar with 4 items; desktop moves it to the top bar.

## Motion

- Signature: when the active colour changes, the table floor floods from the discard pile (`clip-path` circle) with Ben-Day dots densest around the pile.
- Played card flies from the hand to the pile via a shared `layoutId`; opponent cards fly in from their seat. Landing squashes (scaleX 1.14 / scaleY 0.84) then settles.
- Springs for position (stiffness 260, damping 22). Durations stay under 450ms except the 2.4s idle nudge on a live draw pile.
- `prefers-reduced-motion`: `MotionConfig reducedMotion="user"`, no fly or squash, a 120ms fade instead; CSS loops are switched off. Covered by `web/src/components/reducedMotion.test.tsx`.

## Charts (`web/src/components/ChartFrame.tsx`)

- Categorical order, fixed and never cycled: `#2a78d6 #eb6834 #1baf7a #eda100 #e87ba4 #008300 #6250d6 #e34948`; series past 7 fold into Other `#898781`. Validated on `#fafafa`: worst CVD adjacent ΔE 9.1, normal vision 19.6, contrast WARN for 3 slots, so every chart ships a table view.
- HTTP status classes are reserved colours: 2xx `#0ca30c`, 3xx `#898781`, 4xx `#fab219`, 5xx `#d03b3b`.
- One y-axis, 2px lines, 10% area wash, bars at most 24px with a 2px surface gap, legend for two or more series, one tooltip style.
- Quiet buckets: the server zero-fills every bucket in the range; the status share chart skips buckets with no traffic instead of plotting 0%.

## Accessibility

- Hand is a listbox with roving tabindex (arrows, Home, End, Enter/Space). Letter keys D/P/U/C work only while focus is inside the table; focus lands on the table when it loads and returns to the hand after a play.
- Live regions: top card (polite), announcements and action results (assertive), 5-second warning on the player's own turn.
- The Wild +4 challenge dialog is an alertdialog: Esc does not close it, focus starts on "Accept, draw 4", Tab stays inside.
- Inputs with hints use `aria-labelledby` so the hint is not part of the accessible name.

## Finish review (2026-11-18)

Reviewed live against the real backend on a 390x844 phone viewport and a 1440x900 desktop: login, register, lobby, room (host and guest), table (my turn, opponent turn, draw then pass, timeout, reload mid-round), account, loan, rules, about, and admin (login, dashboard, metrics, account detail).

Fixed during review:

- Hand overflowed at 390px with 7 or more cards; now fits by overlap, then scrolls.
- Keyboard focus was lost after a played card unmounted; focus now returns to the hand.
- Wild +4 face: "+4" sat on top of the four shapes; it now has its own row.
- Balance chart with one point drew nothing; the line now runs to the current balance.
- Admin stat tiles were misaligned by a stacked margin rule.
- Backend: `GET /api/loans/active` panicked with no active loan; admin metrics returned 500 (timestamp scanned into a string); metrics skipped quiet buckets; request logs stored raw paths instead of route patterns.

Verdict: ship for the class demo. Open items:

- Not automated: a keyboard-only full round in Playwright (F5) and an axe run (F6).
- Request-log rows written before the route-pattern fix still show raw paths in the endpoint chart until they age out (60 days).
- Admin bundle is one 254KB gzip chunk.

## Raster provenance

The app ships no raster images. `web/public/favicon.svg` and every card face are hand-authored SVG in this repo (`web/src/components/Card.tsx`). Fonts come from `@fontsource/coiny` and `@fontsource/be-vietnam-pro` (SIL Open Font License). Screenshots in `.impeccable/review/` are review evidence only and are not shipped.

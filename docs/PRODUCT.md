# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

Groups of friends and students sitting together in one room, each holding their own phone upright, playing quick rounds during breaks. Phone-first; desktop works but is secondary. An admin (the project owner) uses a separate site on desktop to watch traffic and manage accounts.

## Product Purpose

UNOFA (Uno Friend Arena) is a non-commercial student project: a real-time multiplayer card game with the publicly known shedding-card rules, a play currency `U`, in-game loans, and a private admin site. It exists to practise system design, web development, databases, CI/CD and security. Success: a group can create a room, share a 6-character code, and finish a round on their phones without confusion or desync.

## Positioning

Rooms are joined only by a 6-character code (no public room list). Every round has a stake in `U` that is deducted when the round starts and paid out to the winner. `U` cannot be bought or cashed out; loans, interest and seizure are a game mechanic.

## Operating Context

- Players: `/lobby` → create room or type a code → `/room/:code` (ready up, host picks stake 10/25/50/100/500U and presses Play) → asset handshake → `/table/:matchId` → winner panel → back to the room.
- 20-second turn clock, UNO call/catch race, `+4` challenge dialog, 90-second reconnect grace.
- Account: balance, balance chart, transaction history, transfers, username/password/language changes. Loan: packages 500/1000/2000U, terms 24h/3d/30d, overdue interest, seizure at 2× principal.
- Admin (`admin.` subdomain): request and HTTP-code charts, accounts list/detail with balance adjust and delete, warning flags, audit log.

## Capabilities and Constraints

- Stack (pinned by docs/spec/06-frontend.md): React 18 + TypeScript + Vite, `react-router-dom`, `zustand`, `framer-motion`, `recharts`, `react-i18next`, native `WebSocket`. No second chart library.
- Card artwork is original: own geometry, own names, never UNO/Mattel assets. Each colour also carries its own shape symbol, so red–green colour-blind players can play.
- Fonts self-hosted, no Google Fonts at runtime. CSP: `script-src 'self'`, `style-src 'self' 'unsafe-inline'`, `img-src 'self' data:`.
- Table route bundle target `< 250 KB` gzip, lazy-loaded.
- Sound: off by default, never before user interaction.
- `en` is the source language, `vi` the translation; server sends error codes, the client translates them.

## Brand Commitments

- Name: UNOFA (Uno Friend Arena). Currency: `U` (UNOd), formatted `1,000U` (en) / `1.000U` (vi).
- Footer on every page: "UNOFA is a student project. U has no real-world value." + link to `/about`.
- Disclaimer text on `/about` is fixed verbatim (docs/spec/01-product.md).
- The user asked for a cartoon style with animation, smooth on phones and PCs, and no AI-slop look.

## Evidence on Hand

No logo, illustrations, photos, or testimonials exist. Card faces and any illustration must be authored for this project.

## Product Principles

1. The table is the product: the current turn, the playable cards, and the clock are readable at a glance on a phone.
2. Server is the truth; the client never guesses hidden cards and rolls back on rejection.
3. Money is never ambiguous: stakes are stated before Play, every balance change is visible.
4. Keyboard and screen-reader players can finish a full round.

## Accessibility & Inclusion

WCAG 2.1 Level A plus AA contrast (4.5:1) and always-visible focus. Colour is never the only signal on cards. Single-key shortcuts only inside the table region. `prefers-reduced-motion` → short fades only.

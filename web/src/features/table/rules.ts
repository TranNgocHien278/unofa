import type { Card, Color } from '../../api/types'

const NUMBER = /^[0-9]$/

// playable mirrors internal/game/rules.go for highlighting only; the server decides.
export function playable(c: Card, top: Card, active: Color): boolean {
  if (c.rank === 'WILD' || c.rank === 'WILD4') return true
  if (c.color === active) return true
  if (c.rank !== top.rank) return false
  return NUMBER.test(c.rank) || top.rank === 'SKIP' || top.rank === 'REVERSE' || top.rank === 'DRAW2'
}

const COLOR_ORDER: Record<Color, number> = { RED: 0, YELLOW: 1, GREEN: 2, BLUE: 3, NONE: 4 }
const RANK_ORDER = ['0', '1', '2', '3', '4', '5', '6', '7', '8', '9', 'SKIP', 'REVERSE', 'DRAW2', 'WILD', 'WILD4']

export interface KeyedCard {
  key: string
  card: Card
}

// sortHand groups by colour then rank and gives each card a key that stays stable
// while other cards come and go (duplicates are numbered by occurrence).
export function sortHand(hand: Card[]): KeyedCard[] {
  const sorted = [...hand].sort(
    (a, b) => COLOR_ORDER[a.color] - COLOR_ORDER[b.color] || RANK_ORDER.indexOf(a.rank) - RANK_ORDER.indexOf(b.rank),
  )
  const seen = new Map<string, number>()
  return sorted.map((card) => {
    const id = `${card.color}-${card.rank}`
    const n = seen.get(id) ?? 0
    seen.set(id, n + 1)
    return { key: `${id}-${n}`, card }
  })
}

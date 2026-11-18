import { describe, expect, it } from 'vitest'
import { playable, sortHand } from './rules'

describe('playable', () => {
  const top = { color: 'RED', rank: '7' } as const
  it('matches colour, number, and wilds', () => {
    expect(playable({ color: 'RED', rank: '2' }, top, 'RED')).toBe(true)
    expect(playable({ color: 'BLUE', rank: '7' }, top, 'RED')).toBe(true)
    expect(playable({ color: 'NONE', rank: 'WILD4' }, top, 'RED')).toBe(true)
    expect(playable({ color: 'BLUE', rank: '2' }, top, 'RED')).toBe(false)
  })
  it('matches action on action of the same rank', () => {
    expect(playable({ color: 'BLUE', rank: 'SKIP' }, { color: 'RED', rank: 'SKIP' }, 'RED')).toBe(true)
  })
  it('uses the chosen colour after a wild', () => {
    expect(playable({ color: 'GREEN', rank: '3' }, { color: 'NONE', rank: 'WILD' }, 'GREEN')).toBe(true)
    expect(playable({ color: 'RED', rank: '3' }, { color: 'NONE', rank: 'WILD' }, 'GREEN')).toBe(false)
  })
})

describe('sortHand', () => {
  it('groups by colour and numbers duplicates', () => {
    const keys = sortHand([
      { color: 'BLUE', rank: '1' },
      { color: 'RED', rank: '5' },
      { color: 'RED', rank: '5' },
      { color: 'NONE', rank: 'WILD' },
    ]).map((k) => k.key)
    expect(keys).toEqual(['RED-5-0', 'RED-5-1', 'BLUE-1-0', 'NONE-WILD-0'])
  })
})

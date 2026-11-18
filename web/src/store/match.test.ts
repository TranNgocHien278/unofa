import { beforeEach, describe, expect, it, vi } from 'vitest'
import { view } from '../test/fixtures'

const matchState = vi.fn()
vi.mock('../api/matches', () => ({ matchState: (...a: unknown[]) => matchState(...a) }))

const { useMatch } = await import('./match')

const action = (seq: number, data: Record<string, unknown>) => ({ name: 'action', seq, server_now: new Date().toISOString(), data })

beforeEach(() => {
  matchState.mockReset().mockResolvedValue(view({ seq: 9 }))
  useMatch.getState().reset()
  useMatch.getState().setMatch('m1', 1)
  useMatch.getState().applySnapshot(view())
})

describe('F1 applyEvent', () => {
  it('drops a duplicate seq', () => {
    const play = action(4, { action: 'play', player_id: 2, card: { color: 'RED', rank: '2' } })
    useMatch.getState().applyEvent(play)
    useMatch.getState().applyEvent({ ...play, data: { action: 'play', player_id: 2, card: { color: 'RED', rank: '9' } } })
    expect(useMatch.getState().seq).toBe(4)
    expect(useMatch.getState().public?.top_card).toEqual({ color: 'RED', rank: '2' })
  })

  it('refetches the snapshot on a gap instead of applying', () => {
    useMatch.getState().applyEvent(action(6, { action: 'pass', player_id: 2 }))
    expect(useMatch.getState().seq).toBe(3)
    expect(matchState).toHaveBeenCalledWith('m1')
  })

  it('appends the private draw frame without moving seq', () => {
    useMatch.getState().applyEvent(action(4, { cards: [{ color: 'GREEN', rank: '1' }] }))
    expect(useMatch.getState().hand).toHaveLength(3)
    expect(useMatch.getState().seq).toBe(3)
    useMatch.getState().applyEvent(action(4, { action: 'draw', player_id: 1 }))
    expect(useMatch.getState().seq).toBe(4)
    expect(useMatch.getState().you.drewThisTurn).toBe(true)
  })
})

describe('F2 optimistic rollback', () => {
  it('keeps the card while pending and clears pending on error', () => {
    const card = { color: 'RED', rank: '5' } as const
    useMatch.getState().setPending('ref-1', card)
    expect(useMatch.getState().pendingAction?.card).toEqual(card)
    expect(useMatch.getState().hand).toContainEqual(card)
    expect(useMatch.getState().rollback('other')).toBe(false)
    expect(useMatch.getState().rollback('ref-1')).toBe(true)
    expect(useMatch.getState().pendingAction).toBeNull()
    expect(useMatch.getState().hand).toContainEqual(card)
  })

  it('removes the card once the server confirms the play', () => {
    const card = { color: 'RED', rank: '5' } as const
    useMatch.getState().setPending('ref-1', card)
    useMatch.getState().applyEvent(action(4, { action: 'play', player_id: 1, card }))
    expect(useMatch.getState().pendingAction).toBeNull()
    expect(useMatch.getState().hand).not.toContainEqual(card)
  })
})

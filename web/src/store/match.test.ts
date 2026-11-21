import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
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

  it('holds the private draw frame without moving seq, and adds the card when it lands', () => {
    vi.useFakeTimers()
    useMatch.getState().applyEvent(action(4, { cards: [{ color: 'GREEN', rank: '1' }] }))
    expect(useMatch.getState().hand).toHaveLength(2)
    expect(useMatch.getState().seq).toBe(3)
    useMatch.getState().applyEvent(action(4, { action: 'draw', player_id: 1 }))
    expect(useMatch.getState().seq).toBe(4)
    expect(useMatch.getState().you.drewThisTurn).toBe(true)
    expect(useMatch.getState().flights).toHaveLength(1)
    vi.advanceTimersByTime(399)
    expect(useMatch.getState().hand).toHaveLength(2)
    expect(count(1)).toBe(2)
    vi.advanceTimersByTime(1)
    expect(useMatch.getState().flights).toHaveLength(0)
    expect(useMatch.getState().hand).toContainEqual({ color: 'GREEN', rank: '1' })
    expect(count(1)).toBe(3)
    vi.useRealTimers()
  })
})

const count = (id: number) => useMatch.getState().public!.players.find((p) => p.user_id === id)!.hand_count
const seat = (user_id: number, hand_count: number) => ({ user_id, seat: user_id - 1, hand_count, status: 'connected' as const, uno_called: false })
const state = (v: ReturnType<typeof view>) => ({ name: 'state', server_now: new Date().toISOString(), data: v })

describe('draw flights', () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => vi.useRealTimers())

  it('+4: four cards land 150ms apart, and the next turn waits for the fourth', () => {
    useMatch.getState().applySnapshot(
      view({ you: { is_your_turn: false }, pub: { players: [seat(1, 2), seat(2, 5), seat(3, 5)], current_player: 1, phase: 'awaiting_challenge', pending_challenger: 2 } }),
    )
    useMatch.getState().applyEvent(action(4, { action: 'accept', player_id: 2, target_player_id: 1, result: 'success' }))
    const flights = useMatch.getState().flights
    expect(flights.map((f) => [f.to, f.delay, f.dur])).toEqual([
      [2, 0, 400],
      [2, 150, 400],
      [2, 300, 400],
      [2, 450, 400],
    ])
    // The server already moved the turn on; that frame and a fast next play wait.
    useMatch.getState().applyEvent(state(view({ seq: 4, you: { is_your_turn: false }, pub: { players: [seat(1, 2), seat(2, 9), seat(3, 5)], current_player: 3 } })))
    useMatch.getState().applyEvent(action(5, { action: 'play', player_id: 3, card: { color: 'RED', rank: '3' } }))

    vi.advanceTimersByTime(399)
    expect(count(2)).toBe(5)
    vi.advanceTimersByTime(1)
    expect(count(2)).toBe(6)
    vi.advanceTimersByTime(300)
    expect(count(2)).toBe(8)
    vi.advanceTimersByTime(149)
    expect(count(2)).toBe(8)
    expect(useMatch.getState().public?.phase).toBe('awaiting_challenge')
    expect(useMatch.getState().public?.top_card).toEqual({ color: 'RED', rank: '7' })

    vi.advanceTimersByTime(1)
    const s = useMatch.getState()
    expect(s.flights).toHaveLength(0)
    expect(count(2)).toBe(9)
    expect(s.public?.phase).toBe('playing')
    expect(s.public?.current_player).toBe(3)
    expect(s.public?.top_card).toEqual({ color: 'RED', rank: '3' })
    expect(s.seq).toBe(5)
  })

  it('+2 on me: my pile and hand grow one card per landing', () => {
    useMatch.getState().applyEvent(action(4, { cards: [{ color: 'GREEN', rank: '1' }, { color: 'BLUE', rank: '2' }] }))
    useMatch.getState().applyEvent(action(4, { action: 'draw2_penalty', player_id: 1, target_player_id: 1, result: 'success' }))
    expect(useMatch.getState().flights).toHaveLength(2)
    vi.advanceTimersByTime(400)
    expect(useMatch.getState().hand).toHaveLength(3)
    expect(count(1)).toBe(3)
    vi.advanceTimersByTime(150)
    expect(useMatch.getState().hand).toHaveLength(4)
    expect(count(1)).toBe(4)
    expect(useMatch.getState().flights).toHaveLength(0)
  })

  it("flies the round's first card from the draw pile to the table", () => {
    useMatch.getState().reset()
    useMatch.getState().setMatch('m1', 1)
    useMatch.getState().applyEvent({ name: 'match_start', seq: 1, server_now: new Date().toISOString(), data: view({ seq: 1 }) })
    expect(useMatch.getState().flights).toMatchObject([{ to: 'discard', card: { color: 'RED', rank: '7' } }])
    vi.advanceTimersByTime(400)
    expect(useMatch.getState().flights).toHaveLength(0)
  })

  it('reduced motion keeps the hold but only fades the card in', () => {
    vi.stubGlobal('matchMedia', (q: string) => ({ matches: q.includes('reduce') }))
    useMatch.getState().applyEvent(action(4, { action: 'draw', player_id: 2 }))
    expect(useMatch.getState().flights.map((f) => f.dur)).toEqual([120])
    vi.advanceTimersByTime(119)
    expect(count(2)).toBe(5)
    vi.advanceTimersByTime(1)
    expect(count(2)).toBe(6)
    vi.unstubAllGlobals()
  })

  it('a hidden tab lands the cards at once instead of holding the table', () => {
    const hidden = vi.spyOn(document, 'hidden', 'get').mockReturnValue(true)
    useMatch.getState().applyEvent(action(4, { action: 'draw2_penalty', player_id: 2, target_player_id: 2, result: 'success' }))
    expect(useMatch.getState().flights).toHaveLength(0)
    expect(count(2)).toBe(7)
    hidden.mockRestore()
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

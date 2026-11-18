import type { Card, MatchView } from '../api/types'

// view builds a two-player snapshot where user 1 (me) holds `hand` and it is my turn.
export function view(over: { seq?: number; hand?: Card[]; you?: Partial<MatchView['you']>; pub?: Partial<MatchView['public']> } = {}): MatchView {
  const hand = over.hand ?? [
    { color: 'RED', rank: '5' },
    { color: 'BLUE', rank: '9' },
  ]
  return {
    match_id: 'm1',
    seq: over.seq ?? 3,
    server_now: new Date().toISOString(),
    you: { user_id: 1, hand, can_call_uno: false, can_catch_uno: null, is_your_turn: true, drew_this_turn: false, ...over.you },
    public: {
      players: [
        { user_id: 1, seat: 0, hand_count: hand.length, status: 'connected', uno_called: false },
        { user_id: 2, seat: 1, hand_count: 5, status: 'connected', uno_called: false },
      ],
      top_card: { color: 'RED', rank: '7' },
      active_color: 'RED',
      draw_pile_count: 80,
      current_player: 1,
      direction: 1,
      turn_deadline: new Date(Date.now() + 20_000).toISOString(),
      pot: 100,
      phase: 'playing',
      ...over.pub,
    },
  }
}

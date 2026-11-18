import { create } from 'zustand'
import { matchState } from '../api/matches'
import type { ActionData, ActionName, Card, Color, MatchEnd, MatchView, Phase, PlayerStatus, PublicState } from '../api/types'

export interface Announce {
  id: number
  kind: ActionName | 'status'
  playerId?: number
  targetId?: number
  result?: 'success' | 'fail'
  card?: Card
  color?: Color
  status?: PlayerStatus
  drew?: boolean
}

export interface EventFrame {
  name: string
  seq?: number
  server_now?: string
  data: unknown
}

interface You {
  canCallUno: boolean
  canCatchUno: number | null
  isYourTurn: boolean
  drewThisTurn: boolean
}

interface MatchState {
  matchId: string | null
  meId: number
  seq: number
  phase: Phase | null
  hand: Card[]
  public: PublicState | null
  you: You
  timerOffsetMs: number
  // Optimistic play (06 §Optimistic UI): the card stays in hand, dimmed, until the
  // server answers. `seq` is the last applied event when it was sent.
  pendingAction: { ref: string; card: Card; seq: number } | null
  // Cards from the latest private draw payload, so the hand can raise the one
  // card still playable after a draw.
  lastDrawn: Card[]
  announce: Announce | null
  end: MatchEnd | null
  setMatch(id: string | null, meId?: number): void
  applySnapshot(v: MatchView): void
  applyEvent(e: EventFrame): void
  setPending(ref: string, card: Card): void
  rollback(ref: string): boolean
  reset(): void
}

const noYou: You = { canCallUno: false, canCatchUno: null, isYourTurn: false, drewThisTurn: false }

let announceId = 1
let lastRefetch = 0

export const sameCard = (a: Card, b: Card) => a.color === b.color && a.rank === b.rank

function removeOne(hand: Card[], card: Card): Card[] {
  const i = hand.findIndex((c) => sameCard(c, card))
  return i < 0 ? hand : [...hand.slice(0, i), ...hand.slice(i + 1)]
}

const offsetOf = (serverNow?: string) => (serverNow ? Date.parse(serverNow) - Date.now() : null)

const initial = {
  matchId: null,
  meId: 0,
  seq: 0,
  phase: null,
  hand: [] as Card[],
  public: null,
  you: noYou,
  timerOffsetMs: 0,
  pendingAction: null,
  lastDrawn: [] as Card[],
  announce: null,
  end: null,
}

export const useMatch = create<MatchState>((set, get) => {
  // Snapshot GET is rate limited to 20/min (04 §Rate limit): one refetch per 3s is plenty.
  function refetch() {
    const id = get().matchId
    if (!id || Date.now() - lastRefetch < 3000) return
    lastRefetch = Date.now()
    matchState(id)
      .then((v) => get().applySnapshot(v))
      .catch(() => {})
  }

  function applyAction(d: ActionData) {
    const s = get()
    const pub = s.public
    const announce: Announce = {
      id: announceId++,
      kind: d.action ?? 'play',
      playerId: d.player_id,
      targetId: d.target_player_id,
      result: d.result,
      card: d.card,
      drew: d.drew,
    }
    if (d.action === 'play' && d.card && pub) {
      const color = d.chosen_color || d.card.color
      announce.color = color
      const mine = d.player_id === s.meId
      set({
        hand: mine ? removeOne(s.hand, d.card) : s.hand,
        pendingAction: mine ? null : s.pendingAction,
        public: {
          ...pub,
          top_card: d.card,
          active_color: color,
          players: pub.players.map((p) =>
            p.user_id === d.player_id ? { ...p, hand_count: Math.max(0, p.hand_count - 1) } : p,
          ),
        },
      })
    } else if (d.action === 'call_uno' && pub) {
      set({ public: { ...pub, players: pub.players.map((p) => (p.user_id === d.player_id ? { ...p, uno_called: true } : p)) } })
    } else if (d.action === 'draw' && d.player_id === s.meId) {
      set({ you: { ...s.you, drewThisTurn: true } })
    }
    set({ announce })
  }

  return {
    ...initial,

    setMatch(id, meId) {
      if (id === get().matchId) {
        if (meId) set({ meId })
        return
      }
      set({ ...initial, matchId: id, meId: meId ?? get().meId })
    },

    applySnapshot(v) {
      const s = get()
      if (s.matchId && v.match_id !== s.matchId) return
      if (v.seq < s.seq) return
      const pending = s.pendingAction && v.seq > s.pendingAction.seq ? null : s.pendingAction
      set({
        matchId: v.match_id,
        meId: v.you.user_id,
        seq: v.seq,
        phase: v.public.phase,
        hand: v.you.hand,
        public: v.public,
        you: {
          canCallUno: v.you.can_call_uno,
          canCatchUno: v.you.can_catch_uno,
          isYourTurn: v.you.is_your_turn,
          drewThisTurn: Boolean(v.you.drew_this_turn),
        },
        timerOffsetMs: offsetOf(v.server_now) ?? s.timerOffsetMs,
        pendingAction: pending,
        lastDrawn: v.you.is_your_turn ? s.lastDrawn : [],
      })
    },

    // applyEvent follows 06 §Store: drop duplicates, refetch on a gap, apply in order.
    applyEvent(e) {
      const s = get()
      const offset = offsetOf(e.server_now)
      if (offset !== null) set({ timerOffsetMs: offset })
      if (e.name === 'state' || e.name === 'match_start') {
        get().applySnapshot(e.data as MatchView)
        return
      }
      if (e.seq === undefined) return
      const d = e.data as ActionData

      // The drawer's private frame shares its seq with the public frame that
      // follows it, and carries only the drawn cards.
      if (e.name === 'action' && d.cards && !d.action) {
        if (e.seq === s.seq + 1) set({ hand: [...s.hand, ...d.cards], lastDrawn: d.cards })
        return
      }
      if (e.seq <= s.seq) return
      if (e.seq > s.seq + 1) {
        refetch()
        return
      }
      set({ seq: e.seq })

      if (e.name === 'action') {
        applyAction(d)
      } else if (e.name === 'player_status') {
        const ps = e.data as { user_id: number; status: PlayerStatus }
        const pub = get().public
        if (pub) set({ public: { ...pub, players: pub.players.map((p) => (p.user_id === ps.user_id ? { ...p, status: ps.status } : p)) } })
        set({ announce: { id: announceId++, kind: 'status', playerId: ps.user_id, status: ps.status } })
      } else if (e.name === 'match_end') {
        const end = e.data as MatchEnd
        const pub = get().public
        set({
          end,
          phase: 'ended',
          pendingAction: null,
          you: noYou,
          public: pub ? { ...pub, phase: 'ended', winner_id: end.winner_id ?? undefined } : pub,
        })
      }
    },

    setPending(ref, card) {
      set({ pendingAction: { ref, card, seq: get().seq } })
    },

    rollback(ref) {
      if (get().pendingAction?.ref !== ref) return false
      set({ pendingAction: null })
      return true
    },

    reset: () => set({ ...initial }),
  }
})

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

// A card in the air from the draw pile to a player's pile, or to the discard for
// the round's first card. The store owns the clock: a pile count goes up only when
// its card lands, and while any card is up the table is locked and later frames
// wait in `held`, so the next turn never starts before the last card lands.
export interface Flight {
  id: number
  to: number | 'discard'
  card?: Card
  at: number // performance.now() when the batch left the pile
  delay: number
  dur: number
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
  // Cards in the air; non-empty means the table is locked.
  flights: Flight[]
  // My drawn cards still in the air: each joins the hand as it lands.
  incoming: Card[]
  setMatch(id: string | null, meId?: number): void
  applySnapshot(v: MatchView, receivedAt?: number): void
  applyEvent(e: EventFrame, receivedAt?: number): void
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

// receivedAt keeps the offset honest for a frame that waited behind a flight.
const offsetOf = (serverNow?: string, receivedAt = Date.now()) => (serverNow ? Date.parse(serverNow) - receivedAt : null)

// Each card flies ~400ms and the next leaves 150ms later. Reduced motion keeps the
// rhythm but only fades the card in at its pile, in the spec's short 120ms fade.
const FLY = { dur: 400, gap: 150 }
const FADE = { dur: 120, gap: 150 }
const reducedMotion = () => typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches

// Who takes cards from the draw pile in an action event, and how many. My own
// count comes from my private frame instead (`incoming`).
function drawsFor(d: ActionData): [number | undefined, number] {
  switch (d.action) {
    case 'draw':
      return [d.player_id, 1]
    case 'timeout':
      return [d.player_id, d.drew ? 1 : 0]
    case 'draw2_penalty':
      return [d.player_id, 2]
    case 'accept':
      return [d.player_id, 4]
    case 'challenge':
      return d.result === 'success' ? [d.target_player_id, 4] : [d.player_id, 6]
    case 'catch_uno':
      return [d.target_player_id, d.result === 'success' ? 2 : 0]
    default:
      return [undefined, 0]
  }
}

let nextFlight = 1
let held: { e: EventFrame; at: number }[] = []
let timers: ReturnType<typeof setTimeout>[] = []

function clearFlights() {
  timers.forEach(clearTimeout)
  timers = []
  held = []
}

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
  flights: [] as Flight[],
  incoming: [] as Card[],
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

  // land puts one card on its pile: only now does that count go up.
  function land(to: number | 'discard', id?: number) {
    const s = get()
    const patch: Partial<MatchState> = {}
    if (id !== undefined) patch.flights = s.flights.filter((f) => f.id !== id)
    if (to !== 'discard' && s.public) {
      const pub = s.public
      patch.public = {
        ...pub,
        draw_pile_count: Math.max(0, pub.draw_pile_count - 1),
        players: pub.players.map((p) => (p.user_id === to ? { ...p, hand_count: p.hand_count + 1 } : p)),
      }
      if (to === s.meId && s.incoming.length) {
        patch.hand = [...s.hand, s.incoming[0]]
        patch.incoming = s.incoming.slice(1)
      }
    }
    set(patch)
    // The last card is down: play out what arrived meanwhile, in order.
    while (held.length && !get().flights.length) {
      const h = held.shift()!
      get().applyEvent(h.e, h.at)
    }
  }

  function fly(to: number | 'discard', n: number, card?: Card) {
    if (n <= 0) return
    // A hidden tab throttles timers, so land at once rather than stall the table.
    if (typeof document !== 'undefined' && document.hidden) {
      for (let i = 0; i < n; i++) land(to)
      return
    }
    const { dur, gap } = reducedMotion() ? FADE : FLY
    const at = performance.now()
    const batch = Array.from({ length: n }, (_, i) => ({ id: nextFlight++, to, card, at, delay: i * gap, dur }))
    set({ flights: [...get().flights, ...batch] })
    for (const f of batch) timers.push(setTimeout(() => land(f.to, f.id), f.delay + f.dur))
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
    const [to, n] = drawsFor(d)
    if (to !== undefined) fly(to, to === s.meId ? s.incoming.length : n)
  }

  return {
    ...initial,

    setMatch(id, meId) {
      if (id === get().matchId) {
        if (meId) set({ meId })
        return
      }
      clearFlights()
      set({ ...initial, matchId: id, meId: meId ?? get().meId })
    },

    applySnapshot(v, receivedAt = Date.now()) {
      const s = get()
      if (s.matchId && v.match_id !== s.matchId) return
      if (s.flights.length) {
        held.push({ e: { name: 'state', data: v }, at: receivedAt })
        return
      }
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
        timerOffsetMs: offsetOf(v.server_now, receivedAt) ?? s.timerOffsetMs,
        pendingAction: pending,
        lastDrawn: v.you.is_your_turn ? s.lastDrawn : [],
        incoming: [],
      })
    },

    // applyEvent follows 06 §Store: drop duplicates, refetch on a gap, apply in order.
    // Frames that arrive while cards are in the air wait for the last one to land.
    applyEvent(e, receivedAt = Date.now()) {
      if (get().flights.length) {
        held.push({ e, at: receivedAt })
        return
      }
      const s = get()
      const offset = offsetOf(e.server_now, receivedAt)
      if (offset !== null) set({ timerOffsetMs: offset })
      if (e.name === 'state' || e.name === 'match_start') {
        get().applySnapshot(e.data as MatchView, receivedAt)
        // The round's first card goes from the draw pile to the table.
        const top = get().public?.top_card
        if (e.name === 'match_start' && top) fly('discard', 1, top)
        return
      }
      if (e.seq === undefined) return
      const d = e.data as ActionData

      // The drawer's private frame shares its seq with the public frame that
      // follows it, and carries only the drawn cards. They join the hand as they
      // land, once the public frame has sent them flying.
      if (e.name === 'action' && d.cards && !d.action) {
        if (e.seq === s.seq + 1) set({ incoming: d.cards, lastDrawn: d.cards })
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

    reset: () => {
      clearFlights()
      set({ ...initial })
    },
  }
})

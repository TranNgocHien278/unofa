// Wire shapes from docs/spec/04-http-api.md and 05-realtime.md, checked against internal/web and internal/game.

export type Locale = 'en' | 'vi'
export type Color = 'RED' | 'YELLOW' | 'GREEN' | 'BLUE' | 'NONE'
export type Rank = '0' | '1' | '2' | '3' | '4' | '5' | '6' | '7' | '8' | '9' | 'SKIP' | 'REVERSE' | 'DRAW2' | 'WILD' | 'WILD4'
export type Range = 'Today' | '24h' | '3d' | '7d' | '30d' | '60d'

export interface Card {
  color: Color
  rank: Rank
}

export interface ActiveLoan {
  loan_id: number
  principal: number
  due_at: string
  owed_now: number
  overdue: boolean
  periods_overdue: number
  seize_at: string
}

export interface Me {
  user_id: number
  username: string
  balance: number
  locale: Locale
  terms_version: number
  active_loan: ActiveLoan | null
}

export interface Page<T> {
  items: T[]
  next_cursor: string | number | null
}

export interface Transaction {
  transaction_id: number
  reason: string
  amount: number
  direction: 'in' | 'out'
  counterparty?: string
  at: string
}

export interface MatchRow {
  match_id: string
  stake: number
  pot: number
  status: 'running' | 'ended' | 'aborted'
  winner_id?: number
  started_at: string
  ended_at?: string
}

export interface LoanProducts {
  packages: number[]
  terms: { term: string; rate_bps: number }[]
  overdue_bps: number
  cap_multiplier: number
}

export interface RoomPlayer {
  user_id: number
  username: string
  seat: number
  lobby_ready: boolean
  balance: number
}

export interface Room {
  room_id: string
  code: string
  stake: number
  status: 'open' | 'locked' | 'in_match'
  host_user_id: number
  players: RoomPlayer[]
}

export type PlayerStatus = 'connected' | 'disconnected' | 'forfeit'
export type Phase = 'playing' | 'awaiting_challenge' | 'ended'

export interface PublicPlayer {
  user_id: number
  seat: number
  hand_count: number
  status: PlayerStatus
  uno_called: boolean
}

export interface PublicState {
  players: PublicPlayer[]
  top_card: Card
  active_color: Color
  draw_pile_count: number
  current_player: number
  direction: 1 | -1
  turn_deadline: string
  pot: number
  phase: Phase
  pending_challenger?: number
  winner_id?: number
}

export interface MatchView {
  match_id: string
  seq: number
  server_now: string
  you: {
    user_id: number
    hand: Card[]
    can_call_uno: boolean
    can_catch_uno: number | null
    is_your_turn: boolean
    drew_this_turn?: boolean
  }
  public: PublicState
}

export type ActionName =
  | 'play'
  | 'draw'
  | 'pass'
  | 'call_uno'
  | 'catch_uno'
  | 'challenge'
  | 'accept'
  | 'timeout'
  | 'draw2_penalty'

export interface ActionData {
  match_id?: string
  player_id?: number
  action?: ActionName
  card?: Card
  chosen_color?: Color | ''
  target_player_id?: number
  result?: 'success' | 'fail'
  drew?: boolean
  cards?: Card[]
}

export interface MatchEnd {
  winner_id: number | null
  pot: number
  reason: 'out_of_cards' | 'last_standing' | 'burn'
}

export interface Hello {
  user_id: number
  room: { room_id: string; seat: number; code?: string } | null
  match_id: string | null
}

export type ServerFrame =
  | { type: 'event'; name: 'hello'; server_now: string; data: Hello }
  | { type: 'event'; name: 'ready_check'; server_now: string; data: { match_id: string } }
  | { type: 'event'; name: 'match_start' | 'state'; seq?: number; server_now: string; data: MatchView }
  | { type: 'event'; name: 'action'; seq: number; server_now: string; data: ActionData }
  | { type: 'event'; name: 'player_status'; seq: number; server_now: string; data: { user_id: number; status: PlayerStatus } }
  | { type: 'event'; name: 'match_end'; seq: number; server_now: string; data: MatchEnd }
  | { type: 'error'; code: string; request_id?: string }
  | { type: 'ack'; ref: string }

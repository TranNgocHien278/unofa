import { useEffect } from 'react'
import { Socket, wsURL } from './socket'
import { matchState } from '../api/matches'
import { me as fetchMe } from '../api/auth'
import type { Card, Color, ServerFrame } from '../api/types'
import { useMatch } from '../store/match'
import { useRoom } from '../store/room'
import { useSession } from '../store/session'
import { useUi } from '../store/ui'
import { errorText, i18n } from '../i18n'
import { uuid } from '../lib/uuid'
import { sound } from '../lib/sound'

let socket: Socket | null = null
let navigate: (to: string) => void = (to) => location.assign(to)

export const setNavigator = (fn: (to: string) => void) => {
  navigate = fn
}

const loadTable = () => import('../features/table/TablePage')

function refreshSnapshot(id: string) {
  matchState(id)
    .then((v) => useMatch.getState().applySnapshot(v))
    .catch(() => {})
}

async function onReadyCheck(matchId: string) {
  const { setPreparing } = useRoom.getState()
  setPreparing(true)
  useMatch.getState().setMatch(matchId, useSession.getState().user?.userId)
  // 05: the server waits 5s for every ready_ack; ack before that even if assets lag.
  const deadline = new Promise((r) => setTimeout(r, 3500))
  await Promise.race([Promise.all([loadTable(), document.fonts?.ready]), deadline])
  socket?.send({ type: 'ready_ack', ref: uuid(), match_id: matchId })
  setPreparing(false)
  navigate(`/table/${matchId}`)
}

function onFrame(f: ServerFrame) {
  const match = useMatch.getState()
  if (f.type === 'error') {
    // The server echoes the action's ref as request_id.
    match.rollback(f.request_id ?? '')
    if (f.code === 'ALREADY_DREW' && match.public) useMatch.setState({ you: { ...match.you, drewThisTurn: true } })
    useUi.getState().toast(errorText(f.code), 'alert')
    return
  }
  if (f.type !== 'event') return

  switch (f.name) {
    case 'hello': {
      if (f.data.room?.code) useRoom.getState().setCode(f.data.room.code)
      if (!f.data.room) useRoom.getState().setCode(null)
      if (f.data.match_id) {
        match.setMatch(f.data.match_id, f.data.user_id)
        refreshSnapshot(f.data.match_id)
      }
      return
    }
    case 'ready_check':
      void onReadyCheck(f.data.match_id)
      return
    case 'match_start':
      match.setMatch(f.data.match_id, f.data.you.user_id)
      match.applyEvent(f)
      return
    case 'match_end':
      match.applyEvent(f)
      if (f.data.winner_id === match.meId) sound('win')
      // PAYOUT commits before match_end is sent (05), so the balance is already final.
      fetchMe()
        .then((m) => useSession.getState().setMe(m))
        .catch(() => {})
      return
    case 'action': {
      match.applyEvent(f)
      const d = f.data
      if (d.action === 'play') sound('play')
      // My private draw frame: the cards join the hand as they land, not now.
      else if (d.cards?.length) sound('draw')
      return
    }
    default:
      match.applyEvent(f)
  }
}

function connect() {
  if (socket) return
  socket = new Socket({
    url: wsURL(),
    onFrame,
    onClose({ reason, willRetry }) {
      if (reason === 'SESSION_REPLACED') useUi.getState().toast(i18n.t('table.replaced'), 'alert')
      if (!willRetry) socket = null
    },
  })
}

export function disconnect() {
  socket?.close()
  socket = null
}

// reconnect opens a fresh socket, e.g. after signing in again with a new session.
export function reconnect() {
  disconnect()
  connect()
}

// sendAction sends one table action. Only `play` is optimistic (06 §Optimistic UI).
export function sendAction(action: string, extra: { card?: Card; chosen_color?: Color } = {}): boolean {
  const m = useMatch.getState()
  if (!m.matchId) return false
  const ref = uuid()
  if (action === 'play' && extra.card) m.setPending(ref, extra.card)
  const ok = socket?.send({ type: 'action', ref, match_id: m.matchId, action, ...extra }) ?? false
  if (!ok) {
    m.rollback(ref)
    useUi.getState().toast(i18n.t('table.offline'), 'alert')
  }
  return ok
}

// useMatchStream keeps the app's single socket open while signed in, and nudges
// it to reconnect as soon as the tab is visible again.
export function useMatchStream() {
  const status = useSession((s) => s.status)
  useEffect(() => {
    if (status !== 'authenticated') return
    connect()
    const wake = () => {
      if (document.visibilityState !== 'visible') return
      if (!socket) connect()
      else socket.retryNow()
    }
    document.addEventListener('visibilitychange', wake)
    window.addEventListener('online', wake)
    return () => {
      document.removeEventListener('visibilitychange', wake)
      window.removeEventListener('online', wake)
      disconnect()
    }
  }, [status])
}

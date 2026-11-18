import { useCallback, useEffect, useRef, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { AnimatePresence, motion, useReducedMotion } from 'framer-motion'
import { Crown, LogOut, Share2, UserX } from 'lucide-react'
import * as rooms from '../../api/rooms'
import { isApiError } from '../../api/client'
import type { Room, RoomPlayer } from '../../api/types'
import { errorText, formatU } from '../../i18n'
import { Money } from '../../components/Money'
import { Dialog } from '../../components/Dialog'
import { STAKES } from '../lobby/LobbyPage'
import { useRoom } from '../../store/room'
import { useSession } from '../../store/session'
import { useMatch } from '../../store/match'
import { useUi } from '../../store/ui'

// No room_state event exists on the wire, so the waiting room polls (24/min, under the 60/min read limit).
const POLL_MS = 2500

function Seat({ p, host, me, canKick, onKick }: { p: RoomPlayer; host: boolean; me: boolean; canKick: boolean; onKick(): void }) {
  const { t } = useTranslation()
  const reduce = useReducedMotion()
  return (
    <motion.li
      layout={!reduce}
      className={`seat-cell ${me ? 'seat-cell--me' : ''}`}
      initial={reduce ? { opacity: 0 } : { opacity: 0, scale: 0.9 }}
      animate={{ opacity: 1, scale: 1 }}
      exit={{ opacity: 0, scale: 0.9 }}
      transition={reduce ? { duration: 0.12 } : { type: 'spring', stiffness: 500, damping: 26 }}
    >
      <span className="avatar" aria-hidden>
        {p.username.slice(0, 1).toUpperCase()}
      </span>
      <span className="seat-cell__body">
        <span className="seat-cell__name">
          {p.username}
          {me && <span className="muted"> · {t('room.you')}</span>}
        </span>
        <span className="muted">
          <Money value={p.balance} />
        </span>
      </span>
      {host && <Crown className="seat-cell__crown" size={20} strokeWidth={2.5} aria-label={t('room.host')} />}
      <AnimatePresence initial={false}>
        {p.lobby_ready && (
          <motion.span
            className="stamp stamp--good seat-cell__stamp"
            initial={reduce ? { opacity: 0 } : { opacity: 0, scale: 1.8, rotate: -20 }}
            animate={{ opacity: 1, scale: 1, rotate: -6 }}
            exit={{ opacity: 0 }}
            transition={reduce ? { duration: 0.12 } : { type: 'spring', stiffness: 700, damping: 18 }}
          >
            {t('room.ready')}
          </motion.span>
        )}
      </AnimatePresence>
      {canKick && (
        <button type="button" className="btn btn--small btn--icon btn--danger seat-cell__kick" onClick={onKick} aria-label={t('room.kick', { name: p.username })}>
          <UserX size={18} strokeWidth={2.5} aria-hidden />
        </button>
      )}
    </motion.li>
  )
}

export function RoomPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const code = (useParams().code ?? '').toUpperCase()
  const meId = useSession((s) => s.user?.userId ?? 0)
  const balance = useSession((s) => s.user?.balance ?? 0)
  const liveMatch = useMatch((s) => (s.phase && s.phase !== 'ended' ? s.matchId : null))
  const [room, setRoom] = useState<Room | null>(null)
  const [fatal, setFatal] = useState<string | null>(null)
  const [busy, setBusy] = useState<string | null>(null)
  const [kickTarget, setKickTarget] = useState<RoomPlayer | null>(null)
  const [confirmLeave, setConfirmLeave] = useState(false)
  const joined = useRef(false)

  const load = useCallback(async () => {
    try {
      let r = await rooms.getRoom(code)
      if (!r.players.some((p) => p.user_id === meId)) {
        if (joined.current) {
          // We were in it and are not any more: kicked.
          useRoom.getState().setCode(null)
          setFatal(t('room.kicked'))
          return
        }
        await rooms.joinRoom(code)
        r = await rooms.getRoom(code)
      }
      joined.current = true
      useRoom.getState().setCode(r.code)
      setRoom(r)
    } catch (err) {
      if (isApiError(err, 'ROOM_NOT_FOUND') || isApiError(err, 'ROOM_FULL') || isApiError(err, 'ALREADY_IN_ROOM') || isApiError(err, 'ROOM_LOCKED')) {
        setFatal(errorText(err.code))
      }
    }
  }, [code, meId, t])

  useEffect(() => {
    void load()
    const id = setInterval(() => {
      if (document.visibilityState === 'visible') void load()
    }, POLL_MS)
    return () => clearInterval(id)
  }, [load])

  if (fatal)
    return (
      <div className="page">
        <div className="cel stack" role="alert">
          <h2>{fatal}</h2>
          <Link className="btn btn--primary" to="/lobby">
            {t('room.toLobby')}
          </Link>
        </div>
      </div>
    )
  if (!room)
    return (
      <div className="empty" role="status">
        <span className="spinner" style={{ display: 'inline-block' }} />
      </div>
    )

  const isHost = room.host_user_id === meId
  const mine = room.players.find((p) => p.user_id === meId)
  const readyCount = room.players.filter((p) => p.lobby_ready).length
  const hostReady = room.players.some((p) => p.user_id === room.host_user_id && p.lobby_ready)
  const open = room.status === 'open'
  const canPlay = isHost && open && readyCount >= 2 && hostReady
  const playHint = !hostReady ? t('room.hintHostReady') : readyCount < 2 ? t('room.hintTwoReady') : null

  async function act(name: string, fn: () => Promise<unknown>, optimistic?: (r: Room) => Room) {
    const before = room
    if (optimistic && before) setRoom(optimistic(before))
    setBusy(name)
    try {
      await fn()
      void load()
    } catch (err) {
      if (before) setRoom(before)
      if (isApiError(err)) useUi.getState().toast(errorText(err.code), 'alert', err.requestId)
    } finally {
      setBusy(null)
    }
  }

  async function share() {
    const url = `${location.origin}/room/${room!.code}`
    const text = t('room.shareText', { code: room!.code })
    if (navigator.share) {
      try {
        await navigator.share({ title: 'UNOFA', text, url })
        return
      } catch {
        // cancelled: fall through to copy
      }
    }
    try {
      await navigator.clipboard.writeText(url)
      useUi.getState().toast(t('room.copied'))
    } catch {
      useUi.getState().toast(room!.code)
    }
  }

  async function leave() {
    setConfirmLeave(false)
    await act('leave', () => rooms.leaveRoom(code))
    useRoom.getState().setCode(null)
    navigate('/lobby')
  }

  return (
    <div className="page room-page">
      <div className="room-head on-sun">
        <div>
          <p className="muted--sun">{t('room.codeLabel')}</p>
          <p className="room-code" aria-label={room.code.split('').join(' ')}>
            {room.code}
          </p>
        </div>
        <button type="button" className="btn btn--sun" onClick={share}>
          <Share2 size={18} strokeWidth={2.5} aria-hidden />
          {t('room.invite')}
        </button>
      </div>

      {!open && liveMatch && (
        <div className="banner" style={{ marginBottom: 16 }}>
          <span style={{ flex: 1 }}>{t('room.inMatch')}</span>
          <Link className="btn btn--small btn--primary" to={`/table/${liveMatch}`}>
            {t('room.toTable')}
          </Link>
        </div>
      )}

      <section className="cel stack" aria-labelledby="stake-title">
        <div className="row row--between">
          <h2 id="stake-title">{t('room.stake')}</h2>
          <span className="stake-big">
            <Money value={room.stake} />
          </span>
        </div>
        {isHost && open && (
          <div className="chips" role="radiogroup" aria-label={t('room.changeStake')}>
            {STAKES.map((s) => (
              <button
                key={s}
                type="button"
                role="radio"
                className="chip"
                aria-checked={room.stake === s}
                disabled={busy !== null}
                onClick={() => room.stake !== s && act('stake', () => rooms.setStake(code, s), (r) => ({ ...r, stake: s }))}
              >
                <Money value={s} />
              </button>
            ))}
          </div>
        )}
        {room.stake > balance && <p className="field-error">{t('room.cantAfford')}</p>}
      </section>

      <section className="cel" aria-labelledby="seats-title">
        <div className="row row--between" style={{ marginBottom: 12 }}>
          <h2 id="seats-title">{t('room.players')}</h2>
          <span className="muted num">{t('room.readyCount', { ready: readyCount, total: room.players.length })}</span>
        </div>
        <ul className="seat-grid">
          <AnimatePresence initial={false}>
            {room.players
              .slice()
              .sort((a, b) => a.seat - b.seat)
              .map((p) => (
                <Seat
                  key={p.user_id}
                  p={p}
                  host={p.user_id === room.host_user_id}
                  me={p.user_id === meId}
                  canKick={isHost && open && p.user_id !== meId}
                  onKick={() => setKickTarget(p)}
                />
              ))}
          </AnimatePresence>
        </ul>
      </section>

      <section className="cel stack room-actions" aria-label={t('room.actions')}>
        <button
          type="button"
          className={`btn btn--block ${mine?.lobby_ready ? '' : 'btn--primary'}`}
          aria-pressed={Boolean(mine?.lobby_ready)}
          disabled={!open || busy !== null}
          onClick={() => {
            const next = !mine?.lobby_ready
            void act('ready', () => rooms.setReady(code, next), (r) => ({
              ...r,
              players: r.players.map((p) => (p.user_id === meId ? { ...p, lobby_ready: next } : p)),
            }))
          }}
        >
          {mine?.lobby_ready ? t('room.unready') : t('room.readyCta')}
        </button>
        {isHost && (
          <>
            <button
              type="button"
              className="btn btn--block btn--sun"
              disabled={!canPlay || busy !== null}
              aria-busy={busy === 'play'}
              aria-describedby="bet-note play-hint"
              onClick={() => act('play', () => rooms.play(code))}
            >
              {busy === 'play' ? <span className="spinner" aria-hidden /> : t('room.playCta', { amount: formatU(room.stake) })}
            </button>
            {playHint && (
              <p id="play-hint" className="muted">
                {playHint}
              </p>
            )}
          </>
        )}
        <p id="bet-note" className="bet-note">
          {t('room.betNote')}
        </p>
      </section>

      <div className="danger-zone">
        <button type="button" className="btn btn--danger" onClick={() => setConfirmLeave(true)} disabled={busy !== null}>
          <LogOut size={18} strokeWidth={2.5} aria-hidden />
          {t('room.leave')}
        </button>
      </div>

      <AnimatePresence>
        {kickTarget && (
          <Dialog key="kick" title={t('room.kickTitle', { name: kickTarget.username })} onClose={() => setKickTarget(null)}>
            <div className="dialog-actions">
              <button type="button" className="btn" onClick={() => setKickTarget(null)} autoFocus>
                {t('common.cancel')}
              </button>
              <button
                type="button"
                className="btn btn--danger"
                onClick={() => {
                  const target = kickTarget
                  setKickTarget(null)
                  void act('kick', () => rooms.kick(code, target.user_id), (r) => ({ ...r, players: r.players.filter((p) => p.user_id !== target.user_id) }))
                }}
              >
                {t('room.kickCta')}
              </button>
            </div>
          </Dialog>
        )}
        {confirmLeave && (
          <Dialog key="leave" title={t('room.leaveTitle')} description={liveMatch ? t('room.leaveForfeit') : undefined} onClose={() => setConfirmLeave(false)}>
            <div className="dialog-actions">
              <button type="button" className="btn" onClick={() => setConfirmLeave(false)} autoFocus>
                {t('common.cancel')}
              </button>
              <button type="button" className="btn btn--danger" onClick={leave}>
                {t('room.leave')}
              </button>
            </div>
          </Dialog>
        )}
      </AnimatePresence>
    </div>
  )
}

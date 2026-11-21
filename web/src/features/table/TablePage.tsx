import './table.css'
import { forwardRef, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { AnimatePresence, motion, useReducedMotion } from 'framer-motion'
import { Menu, RotateCcw, RotateCw } from 'lucide-react'
import { matchState } from '../../api/matches'
import { getRoom } from '../../api/rooms'
import { isApiError } from '../../api/client'
import type { Card, Color, PublicPlayer } from '../../api/types'
import { CardBack, colorVar } from '../../components/Card'
import { Discard } from '../../components/Discard'
import { Hand, type HandHandle } from '../../components/Hand'
import { Money } from '../../components/Money'
import { Timer } from '../../components/Timer'
import { useMatch, sameCard } from '../../store/match'
import { useRoom } from '../../store/room'
import { useSession } from '../../store/session'
import { sendAction } from '../../realtime/useMatchStream'
import { sound } from '../../lib/sound'
import { playable, sortHand, type KeyedCard } from './rules'
import { ChallengeDialog, ColorPicker, TableMenu, WinnerPanel } from './TableDialogs'
import { useAnnouncement } from './announce'
import { DrawFlights } from './DrawFlights'

const FLOOR: Record<Color, string> = {
  RED: 'var(--floor-red)',
  YELLOW: 'var(--floor-yellow)',
  GREEN: 'var(--floor-green)',
  BLUE: 'var(--floor-blue)',
  NONE: 'var(--floor-none)',
}

// Floor floods with the active colour, spreading from the discard pile (the
// direction contract's signature move). Reduced motion swaps it instantly.
function Floor({ color, origin }: { color: Color; origin: { x: number; y: number } }) {
  const reduce = useReducedMotion()
  const [layers, setLayers] = useState([{ id: 0, color }])
  const [prev, setPrev] = useState(color)
  if (prev !== color) {
    setPrev(color)
    setLayers((l) => [...l.slice(-1), { id: (l[l.length - 1]?.id ?? 0) + 1, color }])
  }
  const at = `${origin.x}px ${origin.y}px`
  return (
    <div className="floor" aria-hidden>
      {layers.map((l, i) => (
        <motion.div
          key={l.id}
          className="floor__layer"
          style={{ background: FLOOR[l.color], ['--dot' as string]: colorVar(l.color), ['--dot-at' as string]: at }}
          initial={i === 0 || reduce ? false : { clipPath: `circle(0px at ${at})` }}
          animate={{ clipPath: `circle(150% at ${at})` }}
          transition={{ duration: 0.75, ease: [0.22, 1, 0.36, 1] }}
        />
      ))}
    </div>
  )
}

// One pile of card backs per player round the table: name on top, count in the
// middle, and a glow while it is that player's move. `angle` is in degrees with
// 90 at the bottom, so seats run clockwise from me.
function Seat({
  p,
  name,
  turn,
  angle,
  pileRef,
}: {
  p: PublicPlayer
  name: string
  turn: boolean
  angle: number
  pileRef(el: HTMLSpanElement | null): void
}) {
  const { t } = useTranslation()
  const out = p.status === 'forfeit'
  const rad = (angle * Math.PI) / 180
  return (
    <li
      className={`seat ${turn ? 'seat--turn' : ''} ${out ? 'seat--out' : ''}`}
      style={{ ['--x' as string]: Math.cos(rad).toFixed(4), ['--y' as string]: Math.sin(rad).toFixed(4) }}
      aria-current={turn ? 'true' : undefined}
    >
      <span className="seat__name">{name}</span>
      <span className="seat__pile" ref={pileRef}>
        <span className="seat__card seat__card--under" aria-hidden>
          <CardBack />
        </span>
        <span className="seat__card" aria-hidden>
          <CardBack />
        </span>
        <span className="seat__count num" aria-hidden>
          {p.hand_count}
        </span>
      </span>
      <span className="sr-only">{t('table.cards', { count: p.hand_count })}</span>
      {p.status === 'disconnected' && <span className="stamp stamp--alert seat__stamp">{t('table.offlineStamp')}</span>}
      {out && <span className="stamp stamp--ink seat__stamp">{t('table.outStamp')}</span>}
      {!out && p.uno_called && p.hand_count === 1 && <span className="stamp stamp--alert seat__stamp">UNO!</span>}
    </li>
  )
}

export default function TablePage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const matchId = useParams().matchId ?? ''
  const m = useMatch()
  const meId = useSession((s) => s.user?.userId ?? 0)
  const code = useRoom((s) => s.code)
  const [names, setNames] = useState<Record<number, string>>({})
  const [gone, setGone] = useState(false)
  const [wildFor, setWildFor] = useState<{ k: KeyedCard; origin: { x: number; y: number } } | null>(null)
  const [menuOpen, setMenuOpen] = useState(false)
  const [playKey, setPlayKey] = useState<string | null>(null)
  const handRef = useRef<HandHandle>(null)
  const tableRef = useRef<HTMLDivElement>(null)
  const drawRef = useRef<HTMLButtonElement>(null)
  const discardRef = useRef<HTMLDivElement>(null)
  const seatEls = useRef(new Map<number, HTMLElement>())
  const [floorAt, setFloorAt] = useState({ x: 200, y: 300 })

  // Snapshot on entry (05 §Snapshot: first visit and F5). During the 5s
  // handshake the server answers NOT_IN_MATCH, so retry a few times first.
  useEffect(() => {
    let cancelled = false
    let tries = 0
    useMatch.getState().setMatch(matchId, meId)
    const load = () =>
      matchState(matchId)
        .then((v) => !cancelled && useMatch.getState().applySnapshot(v))
        .catch((e) => {
          if (cancelled || !isApiError(e, 'NOT_IN_MATCH')) return
          if (useMatch.getState().public) return
          if (++tries < 4) setTimeout(load, 1500)
          else setGone(true)
        })
    if (!useMatch.getState().public) void load()
    return () => {
      cancelled = true
    }
  }, [matchId, meId])

  useEffect(() => {
    if (!code) return
    getRoom(code)
      .then((r) => setNames(Object.fromEntries(r.players.map((p) => [p.user_id, p.username]))))
      .catch(() => {})
  }, [code])

  // Land focus in the table so D/P/U/C work straight away (they only listen here).
  const hasTable = m.public !== null
  useEffect(() => {
    const idle = !document.activeElement || document.activeElement === document.body
    if (hasTable && idle) tableRef.current?.focus({ preventScroll: true })
  }, [hasTable])

  useLayoutEffect(() => {
    const measure = () => {
      const a = tableRef.current?.getBoundingClientRect()
      const b = discardRef.current?.getBoundingClientRect()
      if (a && b) setFloorAt({ x: b.left - a.left + b.width / 2, y: b.top - a.top + b.height / 2 })
    }
    measure()
    window.addEventListener('resize', measure)
    return () => window.removeEventListener('resize', measure)
  }, [m.public !== null])

  const nameOf = (id?: number | null) =>
    id == null ? '' : id === meId ? t('table.you') : (names[id] ?? t('table.seatName', { n: (m.public?.players.find((p) => p.user_id === id)?.seat ?? 0) + 1 }))

  const announcement = useAnnouncement(m.announce, nameOf, meId)

  const cards = useMemo(() => sortHand(m.hand), [m.hand])
  const pub = m.public

  // The play by me that should fly from the hand: cleared once it lands or fails.
  useEffect(() => {
    if (!m.pendingAction && m.announce?.kind !== 'play') setPlayKey(null)
  }, [m.pendingAction, m.announce])

  // The opponent's seat a played card flies from, relative to the pile.
  const from = useMemo(() => {
    const a = m.announce
    if (!a || a.kind !== 'play' || a.playerId === meId || !a.playerId) return null
    const seat = seatEls.current.get(a.playerId)?.getBoundingClientRect()
    const pile = discardRef.current?.getBoundingClientRect()
    if (!seat || !pile) return { x: 0, y: -200 }
    return { x: seat.left + seat.width / 2 - (pile.left + pile.width / 2), y: seat.top + seat.height / 2 - (pile.top + pile.height / 2) }
  }, [m.announce, meId])

  useEffect(() => {
    if (m.you.isYourTurn) sound('warn')
  }, [m.you.isYourTurn])

  if (gone)
    return (
      <div className="page" style={{ paddingTop: 48 }}>
        <div className="cel stack" role="alert">
          <h2>{t('table.goneTitle')}</h2>
          <p className="muted">{t('table.goneBody')}</p>
          <Link className="btn btn--primary" to={code ? `/room/${code}` : '/lobby'}>
            {code ? t('table.toRoom') : t('room.toLobby')}
          </Link>
        </div>
      </div>
    )

  if (!pub)
    return (
      <div className="table table--loading" role="status">
        <span className="spinner" />
        <p>{t('room.preparing')}</p>
      </div>
    )

  const yourTurn = m.you.isYourTurn && pub.phase === 'playing'
  // Cards in the air lock the table until the last one lands; the next turn's
  // frames wait in the store meanwhile.
  const locked = m.flights.length > 0
  const myTurn = yourTurn && !locked
  const drawnOnly = m.you.drewThisTurn
  const isPlayable = (c: Card) => {
    if (!myTurn || m.pendingAction) return false
    if (drawnOnly) return m.lastDrawn.some((d) => sameCard(d, c)) && playable(c, pub.top_card, pub.active_color)
    return playable(c, pub.top_card, pub.active_color)
  }
  const pendingKey = m.pendingAction ? (cards.find((k) => sameCard(k.card, m.pendingAction!.card))?.key ?? null) : null
  const current = pub.players.find((p) => p.user_id === pub.current_player)
  // Everyone in turn order starting with me, so I sit at the bottom of the table.
  const seats = (() => {
    const sorted = [...pub.players].sort((a, b) => a.seat - b.seat)
    const i = sorted.findIndex((p) => p.user_id === meId)
    return i < 0 ? sorted : [...sorted.slice(i), ...sorted.slice(0, i)]
  })()
  // Exactly one pile glows: whoever the server is waiting on.
  const turnOwner = pub.phase === 'ended' ? null : pub.phase === 'awaiting_challenge' ? (pub.pending_challenger ?? null) : pub.current_player
  const challenger = pub.phase === 'awaiting_challenge' && pub.pending_challenger === meId && !locked
  const canDraw = myTurn && !m.you.drewThisTurn
  const canPass = myTurn && m.you.drewThisTurn
  // UNO is only for the one-card moment; the server refuses two cards as well.
  const canUno = m.you.canCallUno && m.hand.length === 1 && !locked
  const canCatch = m.you.canCatchUno !== null && !locked
  const catchName = m.you.canCatchUno !== null ? nameOf(m.you.canCatchUno) : ''

  function play(k: KeyedCard, el: HTMLElement) {
    if (k.card.color === 'NONE') {
      const r = el.getBoundingClientRect()
      setWildFor({ k, origin: { x: r.left + r.width / 2, y: r.top } })
      return
    }
    setPlayKey(k.key)
    sendAction('play', { card: k.card })
  }

  function chooseColor(c: Exclude<Color, 'NONE'>) {
    if (!wildFor) return
    setPlayKey(wildFor.k.key)
    sendAction('play', { card: wildFor.k.card, chosen_color: c })
    setWildFor(null)
  }

  // Single-key shortcuts only fire inside the table region (WCAG 2.1.4, 06 §Bàn phím).
  function onKeyDown(e: React.KeyboardEvent) {
    if (e.ctrlKey || e.metaKey || e.altKey || (e.target as HTMLElement).tagName === 'INPUT') return
    const k = e.key.toLowerCase()
    if (k === 'd' && canDraw) sendAction('draw')
    else if (k === 'p' && canPass) sendAction('pass')
    else if (k === 'u' && canUno) sendAction('call_uno')
    else if (k === 'c' && canCatch) sendAction('catch_uno')
    else return
    e.preventDefault()
  }

  const discardLayoutId = playKey && m.announce?.kind === 'play' && m.announce.playerId === meId ? `card-${playKey}` : undefined

  return (
    <section className="table" ref={tableRef} tabIndex={-1} onKeyDown={onKeyDown} aria-label={t('table.region')}>
      <Floor color={pub.active_color} origin={floorAt} />

      <header className="tstrip">
        <button type="button" className="btn btn--small btn--icon" onClick={() => setMenuOpen(true)} aria-label={t('table.menu')}>
          <Menu size={20} strokeWidth={2.5} aria-hidden />
        </button>
        <span className="tstrip__pot">
          <span className="muted">{t('table.pot')}</span> <Money value={pub.pot} />
        </span>
        <span className="tstrip__dir" title={t('table.direction')}>
          {pub.direction === 1 ? <RotateCw size={18} strokeWidth={2.5} aria-label={t('table.clockwise')} /> : <RotateCcw size={18} strokeWidth={2.5} aria-label={t('table.counter')} />}
        </span>
        {pub.phase !== 'ended' && <Timer deadline={pub.turn_deadline} offsetMs={m.timerOffsetMs} mine={yourTurn || challenger} />}
      </header>

      <div className="ring-wrap">
        <div className="ring" data-count={seats.length}>
          <span className="ring__table" aria-hidden />
          <ul className="seats" aria-label={t('table.players')}>
            {seats.map((p, i) => (
              <Seat
                key={p.user_id}
                p={p}
                name={nameOf(p.user_id)}
                turn={p.user_id === turnOwner}
                angle={90 + (360 * i) / seats.length}
                pileRef={(el) => {
                  if (el) seatEls.current.set(p.user_id, el)
                  else seatEls.current.delete(p.user_id)
                }}
              />
            ))}
          </ul>
          <button
            ref={drawRef}
            type="button"
            className={`drawpile ${canDraw ? 'drawpile--live' : ''}`}
            onClick={() => canDraw && sendAction('draw')}
            aria-disabled={!canDraw}
            aria-label={t('table.drawPile', { count: pub.draw_pile_count })}
            tabIndex={canDraw ? 0 : -1}
          >
            <span className="drawpile__card drawpile__card--3" aria-hidden>
              <CardBack />
            </span>
            <span className="drawpile__card drawpile__card--2" aria-hidden>
              <CardBack />
            </span>
            <span className="drawpile__card" aria-hidden>
              <CardBack />
            </span>
            <span className="drawpile__count num" aria-hidden>
              {pub.draw_pile_count}
            </span>
          </button>
          <Discard
            ref={discardRef}
            top={pub.top_card}
            activeColor={pub.active_color}
            seq={m.seq}
            playId={m.announce?.kind === 'play' ? m.announce.id : 0}
            layoutId={discardLayoutId}
            from={from}
            hidden={m.flights.some((f) => f.to === 'discard')}
          />
        </div>
        {/* On screen the glowing pile and the cards say whose turn it is and what
            just happened; these lines say it to screen readers. */}
        <p className="sr-only" aria-live="polite">
          {pub.phase === 'awaiting_challenge'
            ? t('table.awaitChallenge', { name: nameOf(pub.pending_challenger) })
            : yourTurn
              ? m.you.drewThisTurn
                ? t('table.yourTurnDrew')
                : t('table.yourTurn')
              : current
                ? t('table.theirTurn', { name: nameOf(current.user_id) })
                : ''}
        </p>
        <p className="sr-only" aria-live="polite">
          {announcement?.text ?? ''}
        </p>
      </div>

      <div className="tactions">
        <p className="sr-only" aria-live="assertive">
          {canUno ? t('table.unoNow') : canCatch ? t('table.catchNow', { name: catchName }) : ''}
        </p>
        <AnimatePresence mode="popLayout">
          {canUno && (
            <ActionButton key="uno" className="btn--uno" onClick={() => sendAction('call_uno')} hotkey="U">
              UNO!
            </ActionButton>
          )}
          {canCatch && (
            <ActionButton key="catch" className="btn--catch" onClick={() => sendAction('catch_uno')} hotkey="C">
              {t('table.catch', { name: catchName })}
            </ActionButton>
          )}
          {canDraw && (
            <ActionButton key="draw" className="btn--primary" onClick={() => sendAction('draw')} hotkey="D">
              {t('table.draw')}
            </ActionButton>
          )}
          {canPass && (
            <ActionButton key="pass" className="btn--primary" onClick={() => sendAction('pass')} hotkey="P">
              {t('table.pass')}
            </ActionButton>
          )}
        </AnimatePresence>
      </div>

      <Hand ref={handRef} cards={cards} isPlayable={isPlayable} pendingKey={pendingKey} onPlay={play} />

      <DrawFlights
        flights={m.flights}
        from={() => drawRef.current}
        to={(f) => (f.to === 'discard' ? discardRef.current : (seatEls.current.get(f.to) ?? null))}
      />

      <AnimatePresence>
        {wildFor && (
          <ColorPicker
            key="wild"
            origin={wildFor.origin}
            onPick={chooseColor}
            onClose={() => {
              const key = wildFor.k.key
              setWildFor(null)
              requestAnimationFrame(() => handRef.current?.focusKey(key))
            }}
          />
        )}
        {challenger && <ChallengeDialog key="challenge" onAccept={() => sendAction('accept')} onChallenge={() => sendAction('challenge')} />}
        {m.end && (
          <WinnerPanel
            key="end"
            end={m.end}
            winnerName={nameOf(m.end.winner_id)}
            mine={m.end.winner_id === meId}
            onBack={() => {
              useMatch.getState().reset()
              navigate(code ? `/room/${code}` : '/lobby')
            }}
          />
        )}
        {menuOpen && <TableMenu key="menu" code={code} onClose={() => setMenuOpen(false)} />}
      </AnimatePresence>
    </section>
  )
}

// forwardRef: AnimatePresence popLayout measures the leaving button through its ref.
const ActionButton = forwardRef<HTMLButtonElement, { children: React.ReactNode; className: string; onClick(): void; hotkey: string }>(function ActionButton(
  { children, className, onClick, hotkey },
  ref,
) {
  const reduce = useReducedMotion()
  return (
    <motion.button
      ref={ref}
      type="button"
      layout={!reduce}
      className={`btn ${className}`}
      onClick={onClick}
      aria-keyshortcuts={hotkey}
      initial={reduce ? { opacity: 0 } : { opacity: 0, scaleX: 1.3, scaleY: 0.5 }}
      animate={{ opacity: 1, scaleX: 1, scaleY: 1 }}
      exit={{ opacity: 0, scale: 0.8 }}
      transition={reduce ? { duration: 0.12 } : { type: 'spring', stiffness: 700, damping: 16 }}
    >
      {children}
      <kbd className="kbd" aria-hidden>
        {hotkey}
      </kbd>
    </motion.button>
  )
})

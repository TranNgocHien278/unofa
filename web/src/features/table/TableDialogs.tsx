import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { motion, useReducedMotion } from 'framer-motion'
import { BookOpen, LogOut, Volume2, VolumeX } from 'lucide-react'
import type { Color, MatchEnd } from '../../api/types'
import { leaveRoom } from '../../api/rooms'
import { isApiError } from '../../api/client'
import { COLORS, colorVar, Shape } from '../../components/Card'
import { Dialog } from '../../components/Dialog'
import { Money } from '../../components/Money'
import { errorText } from '../../i18n'
import { useMatch } from '../../store/match'
import { useRoom } from '../../store/room'
import { useUi } from '../../store/ui'

type Pick = Exclude<Color, 'NONE'>

// Colour picker for wilds: four big swatches, each with its shape so colour is
// never the only cue; keys 1–4 pick, Esc returns the card to the hand.
export function ColorPicker({ origin, onPick, onClose }: { origin: { x: number; y: number }; onPick(c: Pick): void; onClose(): void }) {
  const { t } = useTranslation()
  return (
    <Dialog title={t('table.pickColor')} onClose={onClose} origin={origin}>
      <div
        className="swatches"
        onKeyDown={(e) => {
          const i = Number(e.key) - 1
          if (i >= 0 && i < 4) {
            e.preventDefault()
            onPick(COLORS[i])
          }
        }}
      >
        {COLORS.map((c, i) => (
          <button key={c} type="button" className="swatch" style={{ background: colorVar(c) }} onClick={() => onPick(c)} aria-keyshortcuts={String(i + 1)}>
            <svg viewBox="-40 -40 80 80" width="44" height="44" aria-hidden>
              <Shape color={c} fill="var(--card-paper)" size={1} sw={6} />
            </svg>
            <span className="swatch__name">{t(`card.color.${c}`)}</span>
            <kbd className="kbd" aria-hidden>
              {i + 1}
            </kbd>
          </button>
        ))}
      </div>
    </Dialog>
  )
}

// The +4 decision is an alertdialog: focus starts on Accept (the safe choice),
// Esc does nothing, and the 20s turn timer keeps running behind it.
export function ChallengeDialog({ onAccept, onChallenge }: { onAccept(): void; onChallenge(): void }) {
  const { t } = useTranslation()
  return (
    <Dialog role="alertdialog" dismissable={false} title={t('table.challengeTitle')} description={t('table.challengeBody')}>
      <div className="dialog-actions">
        <button type="button" className="btn btn--primary" onClick={onAccept}>
          {t('table.accept')}
        </button>
        <button type="button" className="btn btn--sun" onClick={onChallenge}>
          {t('table.challenge')}
        </button>
      </div>
    </Dialog>
  )
}

const BACK_S = 10

export function WinnerPanel({ end, winnerName, mine, onBack }: { end: MatchEnd; winnerName: string; mine: boolean; onBack(): void }) {
  const { t } = useTranslation()
  const reduce = useReducedMotion()
  const [left, setLeft] = useState(BACK_S)
  useEffect(() => {
    const id = setInterval(() => setLeft((n) => n - 1), 1000)
    return () => clearInterval(id)
  }, [])
  useEffect(() => {
    if (left <= 0) onBack()
  }, [left, onBack])

  const burn = end.reason === 'burn' || end.winner_id === null
  return (
    <Dialog title={burn ? t('table.burnTitle') : mine ? t('table.winTitle') : t('table.loseTitle', { name: winnerName })} onClose={onBack}>
      <div className="winner">
        <motion.span
          className={`stamp ${burn ? 'stamp--ink' : mine ? 'stamp--good' : 'stamp--alert'} winner__stamp`}
          initial={reduce ? { opacity: 0 } : { opacity: 0, scale: 2.4, rotate: -24 }}
          animate={{ opacity: 1, scale: 1, rotate: -8 }}
          transition={reduce ? { duration: 0.12 } : { type: 'spring', stiffness: 600, damping: 14, delay: 0.15 }}
        >
          {burn ? t('table.burnStamp') : mine ? t('table.winStamp') : t('table.loseStamp')}
        </motion.span>
        <p className="winner__pot">
          {burn ? t('table.burnBody') : mine ? t('table.wonPot') : t('table.theyWonPot', { name: winnerName })} <Money value={end.pot} />
        </p>
      </div>
      <div className="dialog-actions">
        <button type="button" className="btn btn--primary" onClick={onBack} autoFocus>
          {t('table.backToRoom', { n: Math.max(0, left) })}
        </button>
      </div>
    </Dialog>
  )
}

// Table menu: sound, rules, and leaving (which forfeits the stake, 02 §Rời ván).
export function TableMenu({ code, onClose }: { code: string | null; onClose(): void }) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const soundOn = useUi((s) => s.sound)
  const [confirm, setConfirm] = useState(false)
  const [busy, setBusy] = useState(false)

  async function forfeit() {
    if (!code) return
    setBusy(true)
    try {
      await leaveRoom(code)
      useMatch.getState().reset()
      useRoom.getState().setCode(null)
      navigate('/lobby')
    } catch (err) {
      if (isApiError(err)) useUi.getState().toast(errorText(err.code), 'alert', err.requestId)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog title={confirm ? t('table.leaveTitle') : t('table.menu')} description={confirm ? t('table.leaveBody') : undefined} onClose={onClose}>
      {confirm ? (
        <div className="dialog-actions">
          <button type="button" className="btn" onClick={() => setConfirm(false)} autoFocus>
            {t('table.stay')}
          </button>
          <button type="button" className="btn btn--danger" onClick={forfeit} disabled={busy || !code} aria-busy={busy}>
            {t('table.forfeit')}
          </button>
        </div>
      ) : (
        <div className="stack" style={{ marginTop: 16 }}>
          <button type="button" className="btn btn--block" aria-pressed={soundOn} onClick={() => useUi.getState().setSound(!soundOn)}>
            {soundOn ? <Volume2 size={20} strokeWidth={2.5} aria-hidden /> : <VolumeX size={20} strokeWidth={2.5} aria-hidden />}
            {soundOn ? t('settings.soundOn') : t('settings.soundOff')}
          </button>
          <a className="btn btn--block" href="/play" target="_blank" rel="noopener">
            <BookOpen size={20} strokeWidth={2.5} aria-hidden />
            {t('nav.rules')}
          </a>
          <div className="danger-zone">
            <button type="button" className="btn btn--danger btn--block" onClick={() => setConfirm(true)}>
              <LogOut size={20} strokeWidth={2.5} aria-hidden />
              {t('table.leave')}
            </button>
          </div>
        </div>
      )}
    </Dialog>
  )
}

import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { motion, useAnimationControls } from 'framer-motion'
import { DoorOpen } from 'lucide-react'
import { createRoom, joinRoom } from '../../api/rooms'
import { isApiError } from '../../api/client'
import { errorText } from '../../i18n'
import { Money } from '../../components/Money'
import { useRoom } from '../../store/room'
import { useSession } from '../../store/session'

// Presets fixed by the spec (01 §Phạm vi, rooms.stake CHECK).
export const STAKES = [10, 25, 50, 100, 500]

// extractCode accepts a bare code, a code with spaces or lower case, or a pasted invite link.
export function extractCode(raw: string): string {
  const up = raw.toUpperCase()
  const fromLink = up.match(/\/ROOM\/([A-Z0-9]+)/)
  return (fromLink ? fromLink[1] : up).replace(/[^A-Z0-9]/g, '').slice(0, 6)
}

export function LobbyPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const current = useRoom((s) => s.code)
  const balance = useSession((s) => s.user?.balance ?? 0)
  const [code, setCode] = useState('')
  const [stake, setStake] = useState(STAKES[1])
  const [joinErr, setJoinErr] = useState<string | null>(null)
  const [busy, setBusy] = useState<'join' | 'create' | null>(null)
  const shake = useAnimationControls()

  async function join(e: React.FormEvent) {
    e.preventDefault()
    if (code.length !== 6) return
    setBusy('join')
    setJoinErr(null)
    try {
      const room = await joinRoom(code)
      useRoom.getState().setCode(room.code)
      navigate(`/room/${room.code}`)
    } catch (err) {
      if (isApiError(err, 'ALREADY_IN_ROOM') && current) setJoinErr(t('lobby.alreadyIn', { code: current }))
      else if (isApiError(err)) setJoinErr(errorText(err.code))
      void shake.start({ x: [0, -8, 6, -3, 0], transition: { duration: 0.32 } })
    } finally {
      setBusy(null)
    }
  }

  async function create() {
    setBusy('create')
    try {
      const room = await createRoom(stake)
      useRoom.getState().setCode(room.code)
      navigate(`/room/${room.code}`)
    } catch (err) {
      if (isApiError(err)) setJoinErr(errorText(err.code))
    } finally {
      setBusy(null)
    }
  }

  return (
    <div className="page">
      <div className="page-head on-sun">
        <h1>{t('lobby.title')}</h1>
      </div>

      {current && (
        <div className="banner" style={{ marginBottom: 16 }}>
          <DoorOpen size={22} strokeWidth={2.5} aria-hidden />
          <span style={{ flex: 1 }}>{t('lobby.inRoom', { code: current })}</span>
          <Link className="btn btn--small btn--primary" to={`/room/${current}`}>
            {t('lobby.back')}
          </Link>
        </div>
      )}

      <motion.form className="cel stack" onSubmit={join} animate={shake} noValidate>
        <h2>{t('lobby.joinTitle')}</h2>
        <label className="field">
          <span>{t('lobby.codeLabel')}</span>
          <input
            className="input code-input"
            inputMode="text"
            autoCapitalize="characters"
            autoComplete="off"
            spellCheck={false}
            placeholder="K7M2QX"
            value={code}
            aria-invalid={Boolean(joinErr)}
            onChange={(e) => {
              setCode(extractCode(e.target.value))
              setJoinErr(null)
            }}
          />
        </label>
        {joinErr && (
          <p className="field-error" role="alert">
            {joinErr}
          </p>
        )}
        <button className="btn btn--primary btn--block" disabled={code.length !== 6 || busy !== null} aria-busy={busy === 'join'}>
          {t('lobby.joinCta')}
        </button>
      </motion.form>

      <section className="cel stack" aria-labelledby="create-title">
        <h2 id="create-title">{t('lobby.createTitle')}</h2>
        <div className="chips" role="radiogroup" aria-label={t('room.stake')}>
          {STAKES.map((s) => (
            <button key={s} type="button" role="radio" className="chip num" aria-checked={stake === s} onClick={() => setStake(s)}>
              <Money value={s} />
            </button>
          ))}
        </div>
        <p className={stake > balance ? 'field-error' : 'muted'}>
          {stake > balance ? t('lobby.stakeTooHigh') : t('lobby.stakeNote')}
        </p>
        <button type="button" className="btn btn--block" onClick={create} disabled={busy !== null} aria-busy={busy === 'create'}>
          {t('lobby.createCta')}
        </button>
      </section>
    </div>
  )
}

import { useState } from 'react'
import { Link, useLocation, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { motion, useAnimationControls, useMotionValue, useReducedMotion, useTransform, animate } from 'framer-motion'
import { login, me } from '../../api/auth'
import { isApiError } from '../../api/client'
import type { Card } from '../../api/types'
import { CardFace } from '../../components/Card'
import { errorText } from '../../i18n'
import { useSession } from '../../store/session'
import { TERMS_VERSION, lastUsername, rememberUsername } from './terms'

const FAN: { card: Card; rot: number; x: number }[] = [
  { card: { color: 'RED', rank: '7' }, rot: -16, x: -70 },
  { card: { color: 'NONE', rank: 'WILD4' }, rot: 0, x: 0 },
  { card: { color: 'BLUE', rank: 'REVERSE' }, rot: 16, x: 70 },
]

// One card of the fan: tapping makes it jump, squash on landing, and its oval
// ground shadow shrinks while it is in the air.
function FanCard({ card, rot, x, i }: { card: Card; rot: number; x: number; i: number }) {
  const reduce = useReducedMotion()
  const y = useMotionValue(0)
  const shadowScale = useTransform(y, [-60, 0], [0.55, 1])
  const shadowOpacity = useTransform(y, [-60, 0], [0.35, 1])
  const sx = useMotionValue(1)
  const sy = useMotionValue(1)
  const jump = () => {
    if (reduce) return
    void animate(y, [0, -58, 0], { duration: 0.55, times: [0, 0.45, 1], ease: ['easeOut', 'easeIn'] })
    void animate(sy, [0.88, 1.08, 1.08, 0.82, 1], { duration: 0.7, times: [0, 0.15, 0.6, 0.8, 1] })
    void animate(sx, [1.08, 0.94, 0.94, 1.14, 1], { duration: 0.7, times: [0, 0.15, 0.6, 0.8, 1] })
  }
  return (
    <motion.div
      className="fan-card"
      style={{ x, rotate: rot, zIndex: i === 1 ? 2 : 1 }}
      initial={reduce ? { opacity: 0 } : { opacity: 0, y: 40, rotate: 0 }}
      animate={{ opacity: 1, y: 0, rotate: rot }}
      transition={reduce ? { duration: 0.12 } : { type: 'spring', stiffness: 260, damping: 14, delay: 0.08 + i * 0.07 }}
    >
      <motion.span className="ground-shadow" style={{ scaleX: shadowScale, opacity: shadowOpacity }} aria-hidden />
      <motion.button type="button" className="fan-hit" style={{ y, scaleX: sx, scaleY: sy }} onClick={jump} tabIndex={-1} aria-hidden>
        <CardFace card={card} />
      </motion.button>
    </motion.div>
  )
}

export function LoginPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const loc = useLocation()
  const [username, setUsername] = useState(lastUsername)
  const [password, setPassword] = useState('')
  const [needTerms, setNeedTerms] = useState(false)
  const [terms, setTerms] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const shake = useAnimationControls()

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError(null)
    try {
      await login(username.trim().toLowerCase(), password, needTerms && terms ? TERMS_VERSION : undefined)
      rememberUsername(username.trim().toLowerCase())
      useSession.getState().setMe(await me())
      navigate((loc.state as { from?: string } | null)?.from ?? '/lobby', { replace: true })
    } catch (err) {
      if (isApiError(err, 'TERMS_OUTDATED')) setNeedTerms(true)
      if (isApiError(err)) {
        setError(err.code === 'LOGIN_LOCKED' && err.retryAfter ? t('auth.lockedFor', { min: Math.ceil(err.retryAfter / 60) }) : errorText(err.code))
      }
      void shake.start({ x: [0, -8, 6, -3, 0], transition: { duration: 0.32 } })
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="page auth-page">
      <section className="auth-hero" aria-labelledby="hero-title">
        <div className="fan" aria-hidden>
          {FAN.map((f, i) => (
            <FanCard key={i} i={i} {...f} />
          ))}
        </div>
        <h1 id="hero-title">{t('auth.heroTitle')}</h1>
        <p className="muted--sun">{t('auth.heroBody')}</p>
      </section>

      <motion.form className="cel stack" animate={shake} onSubmit={submit} noValidate>
        <h2>{t('auth.loginTitle')}</h2>
        <label className="field">
          <span>{t('auth.username')}</span>
          <input
            className="input"
            name="username"
            autoComplete="username"
            autoCapitalize="none"
            spellCheck={false}
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            required
          />
        </label>
        <label className="field">
          <span>{t('auth.password')}</span>
          <input
            className="input"
            type="password"
            name="password"
            autoComplete="current-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            required
          />
        </label>
        {needTerms && (
          <label className="check">
            <input type="checkbox" checked={terms} onChange={(e) => setTerms(e.target.checked)} />
            <span>
              {t('auth.termsAgain')} <Link to="/about">{t('auth.termsLink')}</Link>
            </span>
          </label>
        )}
        {error && (
          <p className="field-error" role="alert">
            {error}
          </p>
        )}
        <button className="btn btn--primary btn--block" disabled={busy || !username || !password || (needTerms && !terms)} aria-busy={busy}>
          {busy ? <span className="spinner" aria-hidden /> : t('auth.loginCta')}
        </button>
        <p className="muted">
          {t('auth.noAccount')} <Link to="/register">{t('auth.registerLink')}</Link>
        </p>
      </motion.form>
    </div>
  )
}

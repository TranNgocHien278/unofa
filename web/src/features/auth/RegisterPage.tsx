import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { motion, useAnimationControls } from 'framer-motion'
import { me, register } from '../../api/auth'
import { isApiError } from '../../api/client'
import { errorText } from '../../i18n'
import { useSession } from '../../store/session'
import { rememberUsername } from './terms'

const USERNAME = /^[a-z0-9_]{3,20}$/

// usernameProblem mirrors the server rule (01 §Đăng ký) so most mistakes show before submit.
export function usernameProblem(name: string): string | null {
  if (!name) return null
  if (!USERNAME.test(name)) return 'USERNAME_INVALID'
  if (name === 'system' || name.startsWith('deleted_')) return 'USERNAME_INVALID'
  return null
}

export function passwordProblem(pw: string): string | null {
  if (!pw) return null
  if (pw.length < 8) return 'PASSWORD_TOO_SHORT'
  if (new TextEncoder().encode(pw).length > 72) return 'PASSWORD_TOO_LONG'
  return null
}

export function RegisterPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [terms, setTerms] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const shake = useAnimationControls()

  // Forgiving input: capitals and stray spaces are fixed rather than rejected.
  const name = username.trim().toLowerCase()
  const nameErr = usernameProblem(name)
  const pwErr = passwordProblem(password)
  const confirmErr = confirm && confirm !== password ? 'auth.mismatch' : null
  const ready = name && password && confirm && !nameErr && !pwErr && !confirmErr && terms

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    if (!ready) return
    setBusy(true)
    setError(null)
    try {
      await register(name, password)
      rememberUsername(name)
      useSession.getState().setMe(await me())
      navigate('/lobby', { replace: true })
    } catch (err) {
      if (isApiError(err)) setError(errorText(err.code))
      void shake.start({ x: [0, -8, 6, -3, 0], transition: { duration: 0.32 } })
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="page">
      <div className="page-head on-sun">
        <h1>{t('auth.registerTitle')}</h1>
        <p className="muted--sun">{t('auth.registerBody')}</p>
      </div>
      <motion.form className="cel stack" animate={shake} onSubmit={submit} noValidate>
        <label className="field">
          <span id="u-label">{t('auth.username')}</span>
          <input
            className="input"
            aria-labelledby="u-label"
            autoComplete="username"
            autoCapitalize="none"
            spellCheck={false}
            value={username}
            aria-invalid={Boolean(nameErr)}
            aria-describedby="u-hint"
            onChange={(e) => setUsername(e.target.value)}
          />
          <small id="u-hint" className={nameErr ? 'field-error' : 'muted'}>
            {t('auth.usernameHint')}
          </small>
        </label>
        <label className="field">
          <span id="p-label">{t('auth.password')}</span>
          <input
            className="input"
            type="password"
            aria-labelledby="p-label"
            autoComplete="new-password"
            value={password}
            aria-invalid={Boolean(pwErr)}
            aria-describedby="p-hint"
            onChange={(e) => setPassword(e.target.value)}
          />
          <small id="p-hint" className={pwErr ? 'field-error' : 'muted'}>
            {pwErr ? errorText(pwErr) : t('auth.passwordHint')}
          </small>
        </label>
        <label className="field">
          <span id="c-label">{t('auth.confirm')}</span>
          <input
            className="input"
            type="password"
            aria-labelledby="c-label"
            aria-describedby={confirmErr ? 'c-err' : undefined}
            autoComplete="new-password"
            value={confirm}
            aria-invalid={Boolean(confirmErr)}
            onChange={(e) => setConfirm(e.target.value)}
          />
          {confirmErr && (
            <small id="c-err" className="field-error">
              {t(confirmErr)}
            </small>
          )}
        </label>
        <label className="check">
          <input type="checkbox" checked={terms} onChange={(e) => setTerms(e.target.checked)} />
          <span>
            {t('auth.terms')} <Link to="/about">{t('auth.termsLink')}</Link>
          </span>
        </label>
        {error && (
          <p className="field-error" role="alert">
            {error}
          </p>
        )}
        <button className="btn btn--primary btn--block" disabled={!ready || busy} aria-busy={busy}>
          {busy ? <span className="spinner" aria-hidden /> : t('auth.registerCta')}
        </button>
        <p className="muted">
          {t('auth.haveAccount')} <Link to="/login">{t('auth.loginLink')}</Link>
        </p>
      </motion.form>
    </div>
  )
}

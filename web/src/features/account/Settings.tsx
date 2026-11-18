import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { AnimatePresence } from 'framer-motion'
import { KeyRound, LogOut, UserPen, Volume2, VolumeX } from 'lucide-react'
import { changePassword, changeUsername, logout, setLocale } from '../../api/auth'
import { isApiError } from '../../api/client'
import type { Locale } from '../../api/types'
import { Dialog } from '../../components/Dialog'
import { errorText } from '../../i18n'
import { disconnect, reconnect } from '../../realtime/useMatchStream'
import { useSession } from '../../store/session'
import { useUi } from '../../store/ui'
import { passwordProblem, usernameProblem } from '../auth/RegisterPage'
import { rememberUsername } from '../auth/terms'

function UsernameDialog({ onClose }: { onClose(): void }) {
  const { t } = useTranslation()
  const [name, setName] = useState('')
  const [pw, setPw] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const clean = name.trim().toLowerCase()
  const problem = usernameProblem(clean)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError(null)
    try {
      const r = await changeUsername(clean, pw)
      useSession.getState().patch({ username: r.username })
      rememberUsername(r.username)
      useUi.getState().toast(t('settings.usernameChanged'))
      onClose()
    } catch (err) {
      if (isApiError(err)) setError(errorText(err.code))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog title={t('settings.changeUsername')} onClose={onClose}>
      <form className="stack" onSubmit={submit} style={{ marginTop: 16 }}>
        <label className="field">
          <span id="nu-label">{t('settings.newUsername')}</span>
          <input
            className="input"
            autoCapitalize="none"
            spellCheck={false}
            value={name}
            aria-labelledby="nu-label"
            aria-describedby="nu-hint"
            aria-invalid={Boolean(problem)}
            onChange={(e) => setName(e.target.value)}
          />
          <small id="nu-hint" className={problem ? 'field-error' : 'muted'}>
            {t('auth.usernameHint')}
          </small>
        </label>
        <label className="field">
          <span>{t('settings.currentPassword')}</span>
          <input className="input" type="password" autoComplete="current-password" value={pw} onChange={(e) => setPw(e.target.value)} />
        </label>
        {error && (
          <p className="field-error" role="alert">
            {error}
          </p>
        )}
        <div className="dialog-actions">
          <button type="button" className="btn" onClick={onClose}>
            {t('common.cancel')}
          </button>
          <button className="btn btn--primary" disabled={!clean || Boolean(problem) || !pw || busy} aria-busy={busy}>
            {t('common.save')}
          </button>
        </div>
      </form>
    </Dialog>
  )
}

function PasswordDialog({ onClose }: { onClose(): void }) {
  const { t } = useTranslation()
  const [cur, setCur] = useState('')
  const [next, setNext] = useState('')
  const [again, setAgain] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const problem = passwordProblem(next)
  const mismatch = again !== '' && again !== next

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError(null)
    try {
      await changePassword(next, cur)
      // The server replaced the session and closed the old socket (04 §PATCH /api/me).
      reconnect()
      useUi.getState().toast(t('settings.passwordChanged'))
      onClose()
    } catch (err) {
      if (isApiError(err)) setError(errorText(err.code))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog title={t('settings.changePassword')} onClose={onClose}>
      <form className="stack" onSubmit={submit} style={{ marginTop: 16 }}>
        <label className="field">
          <span>{t('settings.currentPassword')}</span>
          <input className="input" type="password" autoComplete="current-password" value={cur} onChange={(e) => setCur(e.target.value)} />
        </label>
        <label className="field">
          <span id="np-label">{t('settings.newPassword')}</span>
          <input
            className="input"
            type="password"
            autoComplete="new-password"
            value={next}
            aria-labelledby="np-label"
            aria-describedby="np-hint"
            aria-invalid={Boolean(problem)}
            onChange={(e) => setNext(e.target.value)}
          />
          <small id="np-hint" className={problem ? 'field-error' : 'muted'}>
            {problem ? errorText(problem) : t('auth.passwordHint')}
          </small>
        </label>
        <label className="field">
          <span id="na-label">{t('auth.confirm')}</span>
          <input
            className="input"
            type="password"
            autoComplete="new-password"
            value={again}
            aria-labelledby="na-label"
            aria-describedby={mismatch ? 'na-err' : undefined}
            aria-invalid={mismatch}
            onChange={(e) => setAgain(e.target.value)}
          />
          {mismatch && (
            <small id="na-err" className="field-error">
              {t('auth.mismatch')}
            </small>
          )}
        </label>
        {error && (
          <p className="field-error" role="alert">
            {error}
          </p>
        )}
        <div className="dialog-actions">
          <button type="button" className="btn" onClick={onClose}>
            {t('common.cancel')}
          </button>
          <button className="btn btn--primary" disabled={!cur || !next || Boolean(problem) || again !== next || busy} aria-busy={busy}>
            {t('common.save')}
          </button>
        </div>
      </form>
    </Dialog>
  )
}

export function Settings() {
  const { t, i18n } = useTranslation()
  const navigate = useNavigate()
  const soundOn = useUi((s) => s.sound)
  const [open, setOpen] = useState<'name' | 'pw' | null>(null)

  async function lang(l: Locale) {
    if (l === i18n.language) return
    await i18n.changeLanguage(l)
    try {
      await setLocale(l)
      useSession.getState().patch({ locale: l })
    } catch {
      // the switch still applies locally; the client toasted the error
    }
  }

  async function signOut() {
    try {
      await logout()
    } catch {
      // leaving anyway: the cookie expires on its own
    }
    disconnect()
    useSession.getState().setAnonymous()
    navigate('/login', { replace: true })
  }

  return (
    <section className="cel stack" aria-labelledby="set-title">
      <h2 id="set-title">{t('settings.title')}</h2>
      <div className="row row--between row--wrap">
        <span>{t('settings.language')}</span>
        <div className="chips" role="radiogroup" aria-label={t('settings.language')}>
          {(['vi', 'en'] as Locale[]).map((l) => (
            <button key={l} type="button" role="radio" className="chip chip--small" aria-checked={i18n.language === l} onClick={() => lang(l)} lang={l}>
              {l === 'vi' ? 'Tiếng Việt' : 'English'}
            </button>
          ))}
        </div>
      </div>
      <button type="button" className="btn" aria-pressed={soundOn} onClick={() => useUi.getState().setSound(!soundOn)}>
        {soundOn ? <Volume2 size={20} strokeWidth={2.5} aria-hidden /> : <VolumeX size={20} strokeWidth={2.5} aria-hidden />}
        {soundOn ? t('settings.soundOn') : t('settings.soundOff')}
      </button>
      <button type="button" className="btn" onClick={() => setOpen('name')}>
        <UserPen size={20} strokeWidth={2.5} aria-hidden />
        {t('settings.changeUsername')}
      </button>
      <button type="button" className="btn" onClick={() => setOpen('pw')}>
        <KeyRound size={20} strokeWidth={2.5} aria-hidden />
        {t('settings.changePassword')}
      </button>
      <div className="danger-zone">
        <button type="button" className="btn btn--danger" onClick={signOut}>
          <LogOut size={20} strokeWidth={2.5} aria-hidden />
          {t('settings.logout')}
        </button>
      </div>
      <AnimatePresence>
        {open === 'name' && <UsernameDialog key="n" onClose={() => setOpen(null)} />}
        {open === 'pw' && <PasswordDialog key="p" onClose={() => setOpen(null)} />}
      </AnimatePresence>
    </section>
  )
}

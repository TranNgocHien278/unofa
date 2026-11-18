import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { login, me } from '../../api/auth'
import { isApiError } from '../../api/client'
import { matchState } from '../../api/matches'
import { Dialog } from '../../components/Dialog'
import { errorText } from '../../i18n'
import { useMatch } from '../../store/match'
import { useSession } from '../../store/session'
import { useUi } from '../../store/ui'
import { disconnect, reconnect } from '../../realtime/useMatchStream'

// Session TTL and the 90s reconnect grace are separate clocks (05 §Hai đồng hồ):
// signing in again here keeps the seat if the grace has not run out.
export function ReloginDialog() {
  const { t } = useTranslation()
  const username = useSession((s) => s.user?.username ?? '')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError(null)
    try {
      await login(username, password)
      useSession.getState().setMe(await me())
      useUi.getState().closeRelogin()
      // The old socket belonged to the dead session.
      reconnect()
      const id = useMatch.getState().matchId
      if (id) useMatch.getState().applySnapshot(await matchState(id))
    } catch (err) {
      if (isApiError(err)) setError(errorText(err.code))
    } finally {
      setBusy(false)
    }
  }

  function signOut() {
    useUi.getState().closeRelogin()
    disconnect()
    useSession.getState().setAnonymous()
  }

  return (
    <Dialog title={t('auth.reloginTitle')} description={t('auth.reloginBody', { name: username })} dismissable={false}>
      <form className="stack" onSubmit={submit} style={{ marginTop: 16 }}>
        <input type="text" autoComplete="username" value={username} readOnly hidden />
        <label className="field">
          <span>{t('auth.password')}</span>
          <input className="input" type="password" autoComplete="current-password" autoFocus value={password} onChange={(e) => setPassword(e.target.value)} />
        </label>
        {error && (
          <p className="field-error" role="alert">
            {error}
          </p>
        )}
        <div className="dialog-actions">
          <button type="button" className="btn btn--small" onClick={signOut}>
            {t('auth.signOut')}
          </button>
          <button className="btn btn--primary" disabled={!password || busy} aria-busy={busy}>
            {t('auth.loginCta')}
          </button>
        </div>
      </form>
    </Dialog>
  )
}

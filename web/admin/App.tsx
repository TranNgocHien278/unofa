import { useEffect, useState } from 'react'
import { createBrowserRouter, NavLink, Outlet, RouterProvider } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { AlertTriangle } from 'lucide-react'
import { adminHeartbeat, adminLogin, adminLogout } from '../src/api/admin'
import { isApiError } from '../src/api/client'
import { errorText, i18n } from '../src/i18n'
import { useAdmin } from './state'
import { Dashboard } from './pages/Dashboard'
import { Metrics } from './pages/Metrics'
import { Accounts } from './pages/Accounts'
import { AccountDetail } from './pages/AccountDetail'
import { Audit } from './pages/Audit'

function Toasts() {
  const toasts = useAdmin((s) => s.toasts)
  const { t } = useTranslation()
  return (
    <div className="toasts" role="status" aria-live="polite">
      {toasts.map((x) => (
        <div key={x.id} className={`toast ${x.tone === 'alert' ? 'toast--alert' : ''}`}>
          {x.tone === 'alert' && <AlertTriangle size={20} strokeWidth={2.5} aria-hidden />}
          <span>
            {x.text}
            {x.requestId && <small> {t('common.requestId', { id: x.requestId })}</small>}
          </span>
        </div>
      ))}
    </div>
  )
}

function Login() {
  const { t } = useTranslation()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError(null)
    try {
      await adminLogin(username.trim(), password)
      useAdmin.getState().setStatus('in')
    } catch (err) {
      if (isApiError(err)) setError(errorText(err.code))
    } finally {
      setBusy(false)
    }
  }
  return (
    <main className="page admin-login">
      <form className="cel stack" onSubmit={submit}>
        <h1>{t('admin.login.title')}</h1>
        <label className="field">
          <span>{t('auth.username')}</span>
          <input className="input" autoComplete="username" autoCapitalize="none" value={username} onChange={(e) => setUsername(e.target.value)} />
        </label>
        <label className="field">
          <span>{t('auth.password')}</span>
          <input className="input" type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} />
        </label>
        {error && (
          <p className="field-error" role="alert">
            {error}
          </p>
        )}
        <button className="btn btn--primary btn--block" disabled={!username || !password || busy} aria-busy={busy}>
          {t('admin.login.cta')}
        </button>
      </form>
    </main>
  )
}

function Layout() {
  const { t } = useTranslation()
  const links = [
    ['/', 'admin.nav.dashboard'],
    ['/metrics', 'admin.nav.metrics'],
    ['/accounts', 'admin.nav.accounts'],
    ['/audit', 'admin.nav.audit'],
  ] as const
  async function out() {
    try {
      await adminLogout()
    } finally {
      useAdmin.getState().setStatus('out')
    }
  }
  return (
    <div className="admin-shell">
      <header className="admin-bar">
        <span className="wordmark">UNOFA</span>
        <nav aria-label={t('admin.nav.main')}>
          {links.map(([to, key]) => (
            <NavLink key={to} to={to} end={to === '/'}>
              {t(key)}
            </NavLink>
          ))}
        </nav>
        <div className="chips" role="group" aria-label={t('settings.language')}>
          {(['en', 'vi'] as const).map((l) => (
            <button key={l} type="button" className="chip chip--small" aria-pressed={i18n.language === l} onClick={() => void i18n.changeLanguage(l)}>
              {l.toUpperCase()}
            </button>
          ))}
        </div>
        <button type="button" className="btn btn--small" onClick={out}>
          {t('admin.nav.logout')}
        </button>
      </header>
      <main className="page page--wide">
        <Outlet />
      </main>
    </div>
  )
}

const router = createBrowserRouter([
  {
    element: <Layout />,
    children: [
      { path: '/', element: <Dashboard /> },
      { path: '/metrics', element: <Metrics /> },
      { path: '/accounts', element: <Accounts /> },
      { path: '/accounts/:id', element: <AccountDetail /> },
      { path: '/audit', element: <Audit /> },
      { path: '*', element: <Dashboard /> },
    ],
  },
])

export function App() {
  const status = useAdmin((s) => s.status)
  useTranslation()

  // No "who am I" endpoint: a heartbeat answers 204 for a live admin session.
  useEffect(() => {
    adminHeartbeat()
      .then(() => useAdmin.getState().setStatus('in'))
      .catch(() => useAdmin.getState().setStatus('out'))
  }, [])

  useEffect(() => {
    if (status !== 'in') return
    const id = setInterval(() => {
      if (document.visibilityState === 'visible') void adminHeartbeat().catch(() => {})
    }, 60_000)
    return () => clearInterval(id)
  }, [status])

  return (
    <>
      {status === 'loading' ? <div className="empty" role="status"><span className="spinner" style={{ display: 'inline-block' }} /></div> : status === 'in' ? <RouterProvider router={router} /> : <Login />}
      <Toasts />
    </>
  )
}

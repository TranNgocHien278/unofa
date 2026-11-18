import type { ReactNode } from 'react'
import { Link, NavLink } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { motion, useReducedMotion } from 'framer-motion'
import { ArrowDownRight, ArrowUpRight, BookOpen, HandCoins, Spade, Wallet } from 'lucide-react'
import { useSession } from '../store/session'
import { Money } from '../components/Money'
import { formatU, i18n } from '../i18n'

const NAV = [
  { to: '/lobby', key: 'nav.play', Icon: Spade },
  { to: '/account', key: 'nav.account', Icon: Wallet },
  { to: '/loan', key: 'nav.loan', Icon: HandCoins },
  { to: '/play', key: 'nav.rules', Icon: BookOpen },
] as const

function BalanceChip() {
  const user = useSession((s) => s.user)
  const delta = useSession((s) => s.lastDelta)
  const reduce = useReducedMotion()
  const { t } = useTranslation()
  if (!user) return null
  return (
    <Link to="/account" className="balance-chip" aria-label={t('nav.balance', { amount: formatU(user.balance) })}>
      {delta > 0 && <ArrowUpRight size={18} strokeWidth={2.75} className="amount-in" aria-hidden />}
      {delta < 0 && <ArrowDownRight size={18} strokeWidth={2.75} className="amount-out" aria-hidden />}
      <motion.span
        key={user.balance}
        initial={reduce ? false : { scale: 1.25, y: -3 }}
        animate={{ scale: 1, y: 0 }}
        transition={{ type: 'spring', stiffness: 500, damping: 14 }}
        style={{ display: 'inline-block' }}
      >
        <Money value={user.balance} />
      </motion.span>
    </Link>
  )
}

function LangSwitch() {
  const { t } = useTranslation()
  return (
    <div className="chips" role="group" aria-label={t('settings.language')} style={{ marginLeft: 'auto' }}>
      {(['en', 'vi'] as const).map((l) => (
        <button key={l} type="button" className="chip" aria-pressed={i18n.language === l} onClick={() => void i18n.changeLanguage(l)}>
          {l.toUpperCase()}
        </button>
      ))}
    </div>
  )
}

export function Shell({ children }: { children: ReactNode }) {
  const { t } = useTranslation()
  const authed = useSession((s) => s.status === 'authenticated')
  return (
    <div className="shell">
      <header className="topbar on-sun">
        <Link to={authed ? '/lobby' : '/'} className="wordmark">
          UNOFA
        </Link>
        {authed && (
          <nav className="topnav" aria-label={t('nav.main')}>
            {NAV.map(({ to, key }) => (
              <NavLink key={to} to={to}>
                {t(key)}
              </NavLink>
            ))}
          </nav>
        )}
        {authed ? <BalanceChip /> : <LangSwitch />}
      </header>
      <main id="main">{children}</main>
      <footer className="footer">
        {t('footer.disclaimer')} <Link to="/about">{t('footer.about')}</Link>
      </footer>
      {authed && (
        <nav className="tabbar" aria-label={t('nav.main')}>
          {NAV.map(({ to, key, Icon }) => (
            <NavLink key={to} to={to}>
              <Icon size={22} strokeWidth={2.5} aria-hidden />
              {t(key)}
            </NavLink>
          ))}
        </nav>
      )}
    </div>
  )
}

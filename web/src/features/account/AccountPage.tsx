import { useTranslation } from 'react-i18next'
import { motion, useReducedMotion } from 'framer-motion'
import { ArrowDownRight, ArrowUpRight } from 'lucide-react'
import { Money } from '../../components/Money'
import { useSession } from '../../store/session'
import { BalanceChart } from './BalanceChart'
import { TransferForm } from './TransferForm'
import { Matches, Transactions } from './History'
import { Settings } from './Settings'

export default function AccountPage() {
  const { t } = useTranslation()
  const reduce = useReducedMotion()
  const user = useSession((s) => s.user)
  const delta = useSession((s) => s.lastDelta)
  if (!user) return null
  return (
    <div className="page page--wide account">
      <div className="page-head on-sun account__hero">
        <p className="muted--sun">{t('account.hello', { name: user.username })}</p>
        <p className="account__balance">
          <motion.span
            key={user.balance}
            style={{ display: 'inline-block' }}
            initial={reduce ? false : { scaleY: 0.7, scaleX: 1.15, y: 6 }}
            animate={{ scaleY: 1, scaleX: 1, y: 0 }}
            transition={{ type: 'spring', stiffness: 500, damping: 12 }}
          >
            <Money value={user.balance} />
          </motion.span>
          {delta !== 0 && (
            <span className={`account__delta ${delta > 0 ? 'amount-in' : 'amount-out'}`}>
              {delta > 0 ? <ArrowUpRight size={28} strokeWidth={3} aria-hidden /> : <ArrowDownRight size={28} strokeWidth={3} aria-hidden />}
              <Money value={delta} signed />
            </span>
          )}
        </p>
      </div>
      <div className="account__grid">
        <div className="account__main">
          <BalanceChart />
          <Transactions />
          <Matches />
        </div>
        <div className="account__side">
          <TransferForm />
          <Settings />
        </div>
      </div>
    </div>
  )
}

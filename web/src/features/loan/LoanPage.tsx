import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { AnimatePresence, motion, useReducedMotion } from 'framer-motion'
import { AlarmClock, AlertTriangle } from 'lucide-react'
import * as loan from '../../api/loan'
import { isApiError } from '../../api/client'
import type { ActiveLoan, LoanProducts } from '../../api/types'
import { Dialog } from '../../components/Dialog'
import { Money } from '../../components/Money'
import { errorText, formatU } from '../../i18n'
import { useSession } from '../../store/session'
import { useUi } from '../../store/ui'
import { forgivenIfSeized, owedInTerm, periodsToSeize } from './loanMath'

const TERM_MS: Record<string, number> = { '24h': 864e5, '3d': 3 * 864e5, '30d': 30 * 864e5 }

function useNow(ms: number) {
  const [now, setNow] = useState(Date.now)
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), ms)
    return () => clearInterval(id)
  }, [ms])
  return now
}

function useLeft() {
  const { t } = useTranslation()
  return (ms: number) => {
    if (ms <= 0) return t('loan.now')
    const d = Math.floor(ms / 864e5)
    const h = Math.floor((ms % 864e5) / 36e5)
    const m = Math.floor((ms % 36e5) / 6e4)
    return d > 0 ? t('loan.leftDays', { d, h }) : t('loan.leftHours', { h, m })
  }
}

function Active({ l, onRepaid }: { l: ActiveLoan; onRepaid(): void }) {
  const { t, i18n } = useTranslation()
  const balance = useSession((s) => s.user?.balance ?? 0)
  const now = useNow(30_000)
  const left = useLeft()
  const [busy, setBusy] = useState(false)
  const [confirm, setConfirm] = useState(false)
  const date = new Intl.DateTimeFormat(i18n.language, { weekday: 'short', day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit' })
  const cap = l.principal * 2
  const short = balance < l.owed_now

  async function pay() {
    setBusy(true)
    try {
      const r = await loan.repay(l.loan_id)
      useSession.getState().setBalance(r.balance_after)
      useUi.getState().toast(t('loan.repaid', { amount: formatU(r.owed_paid) }))
      onRepaid()
    } catch (err) {
      if (isApiError(err)) useUi.getState().toast(errorText(err.code), 'alert', err.requestId)
    } finally {
      setBusy(false)
      setConfirm(false)
    }
  }

  return (
    <>
      {l.overdue && (
        <div className="banner banner--alert" role="alert">
          <AlertTriangle size={22} strokeWidth={2.5} aria-hidden />
          <span>{t('loan.overdueBanner', { n: l.periods_overdue })}</span>
        </div>
      )}
      <section className="cel stack loan-active" aria-labelledby="la-title">
        <h2 id="la-title">{t('loan.activeTitle')}</h2>
        <div className="loan-owed">
          <span className="muted">{t('loan.owedNow')}</span>
          <span className="loan-owed__big">
            <Money value={l.owed_now} />
          </span>
          <span className="muted">
            {t('loan.principal')} <Money value={l.principal} />
          </span>
        </div>
        <dl className="facts">
          <div>
            <dt>{l.overdue ? t('loan.wasDue') : t('loan.due')}</dt>
            <dd>
              <AlarmClock size={18} strokeWidth={2.5} aria-hidden /> {date.format(new Date(l.due_at))}
              {!l.overdue && <span className="muted"> · {left(Date.parse(l.due_at) - now)}</span>}
            </dd>
          </div>
          <div>
            <dt>{t('loan.seizeAt')}</dt>
            <dd>
              {date.format(new Date(l.seize_at))} <span className="muted">· {left(Date.parse(l.seize_at) - now)}</span>
            </dd>
          </div>
          {l.overdue && (
            <div>
              <dt>{t('loan.ifSeized')}</dt>
              <dd>{t('loan.seizePreview', { take: formatU(Math.min(cap, Math.max(0, balance))), forgive: formatU(forgivenIfSeized(cap, balance)) })}</dd>
            </div>
          )}
        </dl>
        {short && <p className="field-error">{t('loan.notEnough', { missing: formatU(l.owed_now - balance) })}</p>}
        <button type="button" className="btn btn--primary btn--block" disabled={short || busy} onClick={() => setConfirm(true)}>
          {t('loan.repayCta', { amount: formatU(l.owed_now) })}
        </button>
      </section>
      <AnimatePresence>
        {confirm && (
          <Dialog title={t('loan.confirmRepay', { amount: formatU(l.owed_now) })} onClose={() => setConfirm(false)}>
            <div className="dialog-actions">
              <button type="button" className="btn" onClick={() => setConfirm(false)} autoFocus>
                {t('common.cancel')}
              </button>
              <button type="button" className="btn btn--primary" onClick={pay} disabled={busy} aria-busy={busy}>
                {t('loan.repay')}
              </button>
            </div>
          </Dialog>
        )}
      </AnimatePresence>
    </>
  )
}

function Borrow({ p, onBorrowed }: { p: LoanProducts; onBorrowed(): void }) {
  const { t, i18n } = useTranslation()
  const [principal, setPrincipal] = useState(p.packages[0])
  const [term, setTerm] = useState(p.terms[0]?.term ?? '24h')
  const [busy, setBusy] = useState(false)
  const [confirm, setConfirm] = useState(false)
  const rate = p.terms.find((x) => x.term === term)?.rate_bps ?? 0
  const owed = owedInTerm(principal, rate)
  const cap = principal * p.cap_multiplier
  const seizeDays = periodsToSeize(principal, rate, p.overdue_bps, p.cap_multiplier)
  const due = new Date(Date.now() + (TERM_MS[term] ?? 0))
  const date = new Intl.DateTimeFormat(i18n.language, { weekday: 'short', day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit' })
  const pct = (bps: number) => new Intl.NumberFormat(i18n.language, { style: 'percent', maximumFractionDigits: 1 }).format(bps / 10000)

  async function take() {
    setBusy(true)
    try {
      const r = await loan.borrow(principal, term)
      useSession.getState().setBalance(r.balance_after)
      useUi.getState().toast(t('loan.borrowed', { amount: formatU(principal) }))
      onBorrowed()
    } catch (err) {
      if (isApiError(err)) useUi.getState().toast(errorText(err.code), 'alert', err.requestId)
    } finally {
      setBusy(false)
      setConfirm(false)
    }
  }

  return (
    <section className="cel stack" aria-labelledby="lb-title">
      <h2 id="lb-title">{t('loan.borrowTitle')}</h2>
      <fieldset className="stack">
        <legend>{t('loan.amount')}</legend>
        <div className="chips" role="radiogroup" aria-label={t('loan.amount')}>
          {p.packages.map((v) => (
            <button key={v} type="button" role="radio" className="chip" aria-checked={principal === v} onClick={() => setPrincipal(v)}>
              <Money value={v} />
            </button>
          ))}
        </div>
      </fieldset>
      <fieldset className="stack">
        <legend>{t('loan.term')}</legend>
        <div className="chips" role="radiogroup" aria-label={t('loan.term')}>
          {p.terms.map((x) => (
            <button key={x.term} type="button" role="radio" className="chip" aria-checked={term === x.term} onClick={() => setTerm(x.term)}>
              {t(`loan.terms.${x.term}`, { defaultValue: x.term })} · {pct(x.rate_bps)}
            </button>
          ))}
        </div>
      </fieldset>
      <div className="loan-preview" aria-live="polite">
        <p>
          {t('loan.previewOwe')}{' '}
          <strong className="loan-preview__big">
            <Money value={owed} />
          </strong>
        </p>
        <p className="muted">{t('loan.previewDue', { date: date.format(due) })}</p>
        <p className="muted">{t('loan.previewLate', { rate: pct(p.overdue_bps), cap: formatU(cap), days: seizeDays })}</p>
      </div>
      <button type="button" className="btn btn--sun btn--block" onClick={() => setConfirm(true)} disabled={busy}>
        {t('loan.borrowCta', { amount: formatU(principal) })}
      </button>
      <p className="muted">{t('loan.note')}</p>
      <AnimatePresence>
        {confirm && (
          <Dialog title={t('loan.confirmBorrow', { amount: formatU(principal), owed: formatU(owed) })} description={t('loan.confirmBorrowBody', { cap: formatU(cap) })} onClose={() => setConfirm(false)}>
            <div className="dialog-actions">
              <button type="button" className="btn" onClick={() => setConfirm(false)} autoFocus>
                {t('common.cancel')}
              </button>
              <button type="button" className="btn btn--sun" onClick={take} disabled={busy} aria-busy={busy}>
                {t('loan.borrow')}
              </button>
            </div>
          </Dialog>
        )}
      </AnimatePresence>
    </section>
  )
}

export default function LoanPage() {
  const { t } = useTranslation()
  const reduce = useReducedMotion()
  const [p, setP] = useState<LoanProducts | null>(null)
  const [act, setAct] = useState<ActiveLoan | null | undefined>(undefined)
  const [stamp, setStamp] = useState(false)

  const refresh = useCallback(async () => {
    try {
      const a = await loan.active()
      setAct(a)
      useSession.setState({ activeLoan: a })
    } catch {
      // toast already shown
    }
  }, [])

  useEffect(() => {
    loan.products().then(setP).catch(() => {})
    void refresh()
  }, [refresh])

  return (
    <div className="page">
      <div className="page-head on-sun">
        <h1>{t('loan.title')}</h1>
        <p className="muted--sun">{t('loan.lede')}</p>
      </div>
      <AnimatePresence>
        {stamp && (
          <motion.div
            className="stamp stamp--good loan-stamp"
            initial={reduce ? { opacity: 0 } : { opacity: 0, scale: 2.4, rotate: -24 }}
            animate={{ opacity: 1, scale: 1, rotate: -8 }}
            exit={{ opacity: 0 }}
            transition={reduce ? { duration: 0.12 } : { type: 'spring', stiffness: 600, damping: 14 }}
            onAnimationComplete={() => setTimeout(() => setStamp(false), 1400)}
          >
            {t('loan.repaidStamp')}
          </motion.div>
        )}
      </AnimatePresence>
      {act === undefined || !p ? (
        <div className="empty" role="status">
          <span className="spinner" style={{ display: 'inline-block' }} />
        </div>
      ) : act ? (
        <Active
          l={act}
          onRepaid={() => {
            setStamp(true)
            void refresh()
          }}
        />
      ) : (
        <Borrow p={p} onBorrowed={refresh} />
      )}
    </div>
  )
}

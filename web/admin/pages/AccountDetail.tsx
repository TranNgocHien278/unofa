import { useCallback, useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { AnimatePresence } from 'framer-motion'
import { ArrowDownLeft, ArrowLeft, ArrowUpRight, Trash2 } from 'lucide-react'
import { account, adjustBalance, deleteAccount, type AdminDetail } from '../../src/api/admin'
import { isApiError } from '../../src/api/client'
import { Dialog } from '../../src/components/Dialog'
import { Money } from '../../src/components/Money'
import { errorText, formatU } from '../../src/i18n'
import { uuid } from '../../src/lib/uuid'
import { useAdmin } from '../state'

function Adjust({ d, onDone }: { d: AdminDetail; onDone(): void }) {
  const { t } = useTranslation()
  const [sign, setSign] = useState<1 | -1>(1)
  const [amount, setAmount] = useState('')
  const [confirm, setConfirm] = useState(false)
  const [busy, setBusy] = useState(false)
  // One request id per intended adjustment: a resend is deduplicated server side.
  const [rid, setRid] = useState(uuid)
  const n = Number(amount)
  const valid = Number.isInteger(n) && n > 0

  async function go() {
    setBusy(true)
    try {
      const r = await adjustBalance(d.user.user_id, sign * n, rid)
      useAdmin.getState().toast(t('admin.detail.adjusted', { amount: formatU(r.balance) }))
      setAmount('')
      setRid(uuid())
      onDone()
    } catch (err) {
      if (isApiError(err)) useAdmin.getState().toast(errorText(err.code), 'alert', err.requestId)
    } finally {
      setBusy(false)
      setConfirm(false)
    }
  }

  const label = sign > 0 ? 'admin.detail.adjustAdd' : 'admin.detail.adjustRemove'
  return (
    <section className="cel stack" aria-labelledby="adj">
      <h2 id="adj">{t('admin.detail.adjust')}</h2>
      <div className="chips" role="radiogroup" aria-label={t('admin.detail.adjust')}>
        <button type="button" role="radio" className="chip" aria-checked={sign > 0} onClick={() => setSign(1)}>
          {t('admin.detail.add')}
        </button>
        <button type="button" role="radio" className="chip" aria-checked={sign < 0} onClick={() => setSign(-1)}>
          {t('admin.detail.remove')}
        </button>
      </div>
      <label className="field">
        <span>{t('admin.detail.amount')}</span>
        <input className="input num" inputMode="numeric" value={amount} onChange={(e) => setAmount(e.target.value.replace(/\D/g, '').slice(0, 9))} />
      </label>
      <button type="button" className="btn btn--primary" disabled={!valid || busy || Boolean(d.user.deleted_at)} onClick={() => setConfirm(true)}>
        {t(label, { amount: valid ? formatU(n) : '' })}
      </button>
      <AnimatePresence>
        {confirm && (
          <Dialog
            title={t(sign > 0 ? 'admin.detail.confirmAdd' : 'admin.detail.confirmRemove', { amount: formatU(n), name: d.user.username })}
            description={t('admin.detail.auditNote')}
            onClose={() => setConfirm(false)}
          >
            <div className="dialog-actions">
              <button type="button" className="btn" onClick={() => setConfirm(false)} autoFocus>
                {t('common.cancel')}
              </button>
              <button type="button" className="btn btn--primary" onClick={go} disabled={busy} aria-busy={busy}>
                {t(label, { amount: formatU(n) })}
              </button>
            </div>
          </Dialog>
        )}
      </AnimatePresence>
    </section>
  )
}

function Delete({ d }: { d: AdminDetail }) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [open, setOpen] = useState(false)
  const [typed, setTyped] = useState('')
  const [busy, setBusy] = useState(false)

  async function go() {
    setBusy(true)
    try {
      await deleteAccount(d.user.user_id)
      useAdmin.getState().toast(t('admin.detail.deletedToast'))
      navigate('/accounts')
    } catch (err) {
      if (isApiError(err)) useAdmin.getState().toast(errorText(err.code), 'alert', err.requestId)
    } finally {
      setBusy(false)
    }
  }

  if (d.user.deleted_at) return null
  return (
    <div className="danger-zone">
      <button type="button" className="btn btn--danger" onClick={() => setOpen(true)}>
        <Trash2 size={18} strokeWidth={2.5} aria-hidden />
        {t('admin.detail.delete')}
      </button>
      <AnimatePresence>
        {open && (
          <Dialog
            role="alertdialog"
            title={t('admin.detail.deleteTitle', { name: d.user.username })}
            description={t('admin.detail.deleteBody', { id: d.user.user_id })}
            onClose={() => setOpen(false)}
          >
            <label className="field" style={{ marginTop: 16 }}>
              <span>{t('auth.username')}</span>
              <input className="input" autoCapitalize="none" spellCheck={false} value={typed} onChange={(e) => setTyped(e.target.value)} />
            </label>
            <div className="dialog-actions">
              <button type="button" className="btn" onClick={() => setOpen(false)}>
                {t('common.cancel')}
              </button>
              <button type="button" className="btn btn--danger" onClick={go} disabled={typed !== d.user.username || busy} aria-busy={busy}>
                {t('admin.detail.deleteCta')}
              </button>
            </div>
          </Dialog>
        )}
      </AnimatePresence>
    </div>
  )
}

export function AccountDetail() {
  const { t, i18n } = useTranslation()
  const id = Number(useParams().id)
  const [d, setD] = useState<AdminDetail | null>(null)
  const [missing, setMissing] = useState(false)
  const date = new Intl.DateTimeFormat(i18n.language, { dateStyle: 'medium', timeStyle: 'short' })

  const load = useCallback(() => {
    account(id)
      .then(setD)
      .catch((e) => isApiError(e, 'USER_NOT_FOUND') && setMissing(true))
  }, [id])
  useEffect(load, [load])

  const back = (
    <Link to="/accounts" className="back-link">
      <ArrowLeft size={18} strokeWidth={2.5} aria-hidden />
      {t('admin.detail.back')}
    </Link>
  )
  if (missing)
    return (
      <div className="admin-page">
        {back}
        <p className="empty">{errorText('USER_NOT_FOUND')}</p>
      </div>
    )
  if (!d) return <span className="spinner" role="status" />

  const l = d.active_loan
  return (
    <div className="admin-page">
      {back}
      <div className="admin-head">
        <h1>{d.user.username}</h1>
        <span className="muted num">#{d.user.user_id}</span>
        {d.user.deleted_at && <span className="stamp stamp--ink">{t('admin.accounts.deleted')}</span>}
      </div>
      <div className="detail-grid">
        <div className="stack">
          <section className="cel stack">
            <p className="tile__n num">
              <Money value={d.user.balance} />
            </p>
            <p className="muted">
              {d.user.locale} · {t('admin.accounts.joined', { date: date.format(new Date(d.user.created_at)) })}
            </p>
            <h2>{t('admin.detail.loan')}</h2>
            {l ? (
              <p>
                {l.overdue && <span className="stamp stamp--small stamp--alert">{t('admin.detail.overdue')}</span>}{' '}
                {t('admin.detail.owed', { owed: formatU(l.owed_now), date: date.format(new Date(l.due_at)) })}
              </p>
            ) : (
              <p className="muted">{t('admin.detail.noLoan')}</p>
            )}
          </section>
          <Adjust d={d} onDone={load} />
          <Delete d={d} />
        </div>
        <section className="cel stack">
          <h2>{t('admin.detail.tx')}</h2>
          <ul className="list">
            {d.transactions.map((x) => (
              <li key={x.transaction_id} className="list__row">
                {x.direction === 'in' ? (
                  <ArrowDownLeft className="amount-in" size={20} strokeWidth={2.75} aria-hidden />
                ) : (
                  <ArrowUpRight className="amount-out" size={20} strokeWidth={2.75} aria-hidden />
                )}
                <span className="list__main">
                  <span>{t(`reason.${x.reason}`, { defaultValue: x.reason })}</span>
                  <span className="muted">{date.format(new Date(x.at))}</span>
                </span>
                <Money value={x.direction === 'in' ? x.amount : -x.amount} signed className={x.direction === 'in' ? 'amount-in' : 'amount-out'} />
              </li>
            ))}
          </ul>
        </section>
      </div>
    </div>
  )
}

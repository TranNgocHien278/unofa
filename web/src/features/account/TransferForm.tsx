import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { AnimatePresence } from 'framer-motion'
import { transfer } from '../../api/money'
import { isApiError } from '../../api/client'
import { Dialog } from '../../components/Dialog'
import { errorText, formatU } from '../../i18n'
import { uuid } from '../../lib/uuid'
import { useSession } from '../../store/session'
import { useUi } from '../../store/ui'

export function TransferForm() {
  const { t } = useTranslation()
  const me = useSession((s) => s.user)
  const [to, setTo] = useState('')
  const [amount, setAmount] = useState('')
  const [confirm, setConfirm] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  // One uuid per intended transfer: a retry after a network blip is not a second payment.
  const [ref, setRef] = useState(uuid)

  const name = to.trim().toLowerCase()
  const n = Number(amount)
  const valid = name.length >= 3 && Number.isInteger(n) && n > 0 && name !== me?.username
  const tooMuch = valid && me ? n > me.balance : false

  async function send() {
    setBusy(true)
    setError(null)
    try {
      const r = await transfer(name, n, ref)
      useSession.getState().setBalance(r.balance_after)
      useUi.getState().toast(t('account.sent', { amount: formatU(r.amount), name: r.to_username }))
      setTo('')
      setAmount('')
      setRef(uuid())
    } catch (err) {
      if (isApiError(err)) setError(errorText(err.code))
    } finally {
      setBusy(false)
      setConfirm(false)
    }
  }

  return (
    <section className="cel stack" aria-labelledby="send-title">
      <h2 id="send-title">{t('account.sendTitle')}</h2>
      <form
        className="stack"
        onSubmit={(e) => {
          e.preventDefault()
          if (valid && !tooMuch) setConfirm(true)
        }}
        noValidate
      >
        <div className="row row--wrap">
          <label className="field" style={{ flex: '2 1 180px' }}>
            <span>{t('account.to')}</span>
            <input className="input" autoCapitalize="none" spellCheck={false} autoComplete="off" value={to} onChange={(e) => setTo(e.target.value)} />
          </label>
          <label className="field" style={{ flex: '1 1 120px' }}>
            <span>{t('account.amount')}</span>
            <input
              className="input num"
              inputMode="numeric"
              value={amount}
              aria-invalid={tooMuch}
              onChange={(e) => setAmount(e.target.value.replace(/\D/g, '').slice(0, 9))}
            />
          </label>
        </div>
        {tooMuch && <p className="field-error">{errorText('INSUFFICIENT_BALANCE')}</p>}
        {error && (
          <p className="field-error" role="alert">
            {error}
          </p>
        )}
        <button className="btn btn--primary" disabled={!valid || tooMuch || busy}>
          {t('account.sendCta')}
        </button>
      </form>
      <AnimatePresence>
        {confirm && (
          <Dialog title={t('account.confirmSend', { amount: formatU(n), name })} description={t('account.confirmSendBody')} onClose={() => setConfirm(false)}>
            <div className="dialog-actions">
              <button type="button" className="btn" onClick={() => setConfirm(false)} autoFocus>
                {t('common.cancel')}
              </button>
              <button type="button" className="btn btn--primary" onClick={send} disabled={busy} aria-busy={busy}>
                {t('account.sendCta')}
              </button>
            </div>
          </Dialog>
        )}
      </AnimatePresence>
    </section>
  )
}

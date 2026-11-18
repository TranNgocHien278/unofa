import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ArrowDownLeft, ArrowUpRight } from 'lucide-react'
import { myMatches, transactions } from '../../api/money'
import type { MatchRow, Transaction } from '../../api/types'
import { Money } from '../../components/Money'
import { useSession } from '../../store/session'

export const REASONS = ['BET', 'PAYOUT', 'TRANSFER', 'LOAN_DISBURSE', 'LOAN_REPAY', 'SEIZE', 'SIGNUP_BONUS', 'SYSTEM_ADD', 'SYSTEM_GET', 'BURN'] as const
type Dir = '' | 'in' | 'out'

function useDateFormat() {
  const { i18n } = useTranslation()
  const f = new Intl.DateTimeFormat(i18n.language, { day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit' })
  return (s: string) => f.format(new Date(s))
}

export function Transactions() {
  const { t } = useTranslation()
  const date = useDateFormat()
  const balance = useSession((s) => s.user?.balance)
  const [reasons, setReasons] = useState<string[]>([])
  const [dir, setDir] = useState<Dir>('')
  const [items, setItems] = useState<Transaction[]>([])
  const [next, setNext] = useState<number | null>(null)
  const [loading, setLoading] = useState(true)

  const load = useCallback(
    async (before: number | null) => {
      setLoading(true)
      try {
        const page = await transactions({ before, reason: reasons, direction: dir || undefined })
        setItems((cur) => (before ? [...cur, ...page.items] : page.items))
        setNext(page.next_cursor as number | null)
      } catch {
        // toast already shown by the client
      } finally {
        setLoading(false)
      }
    },
    [reasons, dir],
  )

  useEffect(() => {
    void load(null)
  }, [load, balance])

  const toggle = (r: string) => setReasons((cur) => (cur.includes(r) ? cur.filter((x) => x !== r) : [...cur, r]))

  return (
    <section className="cel stack" aria-labelledby="tx-title">
      <h2 id="tx-title">{t('account.txTitle')}</h2>
      <div className="chips" role="group" aria-label={t('account.direction')}>
        {(['', 'in', 'out'] as Dir[]).map((d) => (
          <button key={d || 'all'} type="button" className="chip chip--small" aria-pressed={dir === d} onClick={() => setDir(d)}>
            {t(`account.dir.${d || 'all'}`)}
          </button>
        ))}
      </div>
      <details className="filters">
        <summary>{t('account.reasonFilter', { count: reasons.length })}</summary>
        <div className="chips" role="group" aria-label={t('account.reason')}>
          {REASONS.map((r) => (
            <button key={r} type="button" className="chip chip--small" aria-pressed={reasons.includes(r)} onClick={() => toggle(r)}>
              {t(`reason.${r}`)}
            </button>
          ))}
        </div>
      </details>
      {items.length === 0 && !loading ? (
        <p className="empty">{t('account.txEmpty')}</p>
      ) : (
        <ul className="list" aria-busy={loading}>
          {items.map((x) => (
            <li key={x.transaction_id} className="list__row">
              {x.direction === 'in' ? (
                <ArrowDownLeft className="amount-in" size={20} strokeWidth={2.75} aria-hidden />
              ) : (
                <ArrowUpRight className="amount-out" size={20} strokeWidth={2.75} aria-hidden />
              )}
              <span className="list__main">
                <span>{t(`reason.${x.reason}`, { defaultValue: x.reason })}</span>
                <span className="muted">
                  {x.counterparty ? `${x.counterparty} · ` : ''}
                  {date(x.at)}
                </span>
              </span>
              <Money value={x.direction === 'in' ? x.amount : -x.amount} signed className={x.direction === 'in' ? 'amount-in' : 'amount-out'} />
            </li>
          ))}
        </ul>
      )}
      {next !== null && (
        <button type="button" className="btn btn--small" onClick={() => load(next)} disabled={loading} aria-busy={loading}>
          {t('common.more')}
        </button>
      )}
    </section>
  )
}

export function Matches() {
  const { t } = useTranslation()
  const date = useDateFormat()
  const meId = useSession((s) => s.user?.userId)
  const [items, setItems] = useState<MatchRow[]>([])
  const [next, setNext] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  const load = useCallback(async (before: string | null) => {
    setLoading(true)
    try {
      const page = await myMatches(before)
      setItems((cur) => (before ? [...cur, ...page.items] : page.items))
      setNext(page.next_cursor as string | null)
    } catch {
      // toast already shown
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load(null)
  }, [load])

  return (
    <section className="cel stack" aria-labelledby="m-title">
      <h2 id="m-title">{t('account.matchesTitle')}</h2>
      {items.length === 0 && !loading ? (
        <p className="empty">{t('account.matchesEmpty')}</p>
      ) : (
        <ul className="list" aria-busy={loading}>
          {items.map((m) => {
            const result = m.status !== 'ended' ? m.status : m.winner_id === meId ? 'win' : m.winner_id ? 'lose' : 'burn'
            return (
              <li key={m.match_id} className="list__row">
                <span className={`stamp stamp--small ${result === 'win' ? 'stamp--good' : result === 'lose' ? 'stamp--alert' : 'stamp--ink'}`}>
                  {t(`account.result.${result}`, { defaultValue: result })}
                </span>
                <span className="list__main">
                  <span>
                    {t('account.stakeOf')} <Money value={m.stake} /> · {t('table.pot')} <Money value={m.pot} />
                  </span>
                  <span className="muted">{date(m.ended_at ?? m.started_at)}</span>
                </span>
              </li>
            )
          })}
        </ul>
      )}
      {next !== null && (
        <button type="button" className="btn btn--small" onClick={() => load(next)} disabled={loading} aria-busy={loading}>
          {t('common.more')}
        </button>
      )}
    </section>
  )
}

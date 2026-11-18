import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { Search } from 'lucide-react'
import { accounts, type AdminUser } from '../../src/api/admin'
import { Money } from '../../src/components/Money'

export function Accounts() {
  const { t, i18n } = useTranslation()
  const [q, setQ] = useState('')
  const [items, setItems] = useState<AdminUser[]>([])
  const [next, setNext] = useState<number | null>(null)
  const [loading, setLoading] = useState(true)
  const date = new Intl.DateTimeFormat(i18n.language, { dateStyle: 'medium' })

  const load = useCallback(
    async (before: number | null) => {
      setLoading(true)
      try {
        const page = await accounts(q.trim().toLowerCase(), before)
        setItems((cur) => (before ? [...cur, ...page.items] : page.items))
        setNext(page.items.length === 30 ? (page.next_cursor as number | null) : null)
      } catch {
        // toasted by the client hooks
      } finally {
        setLoading(false)
      }
    },
    [q],
  )

  // Debounced search: one request after the admin stops typing.
  useEffect(() => {
    const id = setTimeout(() => void load(null), 250)
    return () => clearTimeout(id)
  }, [load])

  return (
    <div className="admin-page">
      <div className="admin-head">
        <h1>{t('admin.accounts.title')}</h1>
      </div>
      <section className="cel stack">
        <label className="field search">
          <span className="sr-only">{t('admin.accounts.search')}</span>
          <Search size={20} strokeWidth={2.5} aria-hidden />
          <input className="input" type="search" placeholder={t('admin.accounts.search')} value={q} onChange={(e) => setQ(e.target.value)} />
        </label>
        {items.length === 0 && !loading ? (
          <p className="empty">{t('admin.accounts.empty')}</p>
        ) : (
          <ul className="list" aria-busy={loading}>
            {items.map((u) => (
              <li key={u.user_id} className="list__row">
                <span className="list__main">
                  <Link to={`/accounts/${u.user_id}`} className="row-link">
                    {u.username}
                  </Link>
                  <span className="muted">
                    #{u.user_id} · {t('admin.accounts.joined', { date: date.format(new Date(u.created_at)) })}
                  </span>
                </span>
                {u.deleted_at && <span className="stamp stamp--small stamp--ink">{t('admin.accounts.deleted')}</span>}
                {u.login_locked_until && Date.parse(u.login_locked_until) > Date.now() && (
                  <span className="stamp stamp--small stamp--alert">{t('admin.accounts.locked')}</span>
                )}
                <Money value={u.balance} />
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
    </div>
  )
}

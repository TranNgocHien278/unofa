import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { ArrowRight } from 'lucide-react'
import { audit, type AuditRow } from '../../src/api/admin'
import { Money } from '../../src/components/Money'

export function Audit() {
  const { t, i18n } = useTranslation()
  const [items, setItems] = useState<AuditRow[]>([])
  const [next, setNext] = useState<number | null>(null)
  const [loading, setLoading] = useState(true)
  const date = new Intl.DateTimeFormat(i18n.language, { dateStyle: 'medium', timeStyle: 'short' })

  const load = useCallback(async (before: number | null) => {
    setLoading(true)
    try {
      const page = await audit(before)
      setItems((cur) => (before ? [...cur, ...page.items] : page.items))
      setNext(page.next_cursor as number | null)
    } catch {
      // toasted by the client hooks
    } finally {
      setLoading(false)
    }
  }, [])
  useEffect(() => {
    void load(null)
  }, [load])

  return (
    <div className="admin-page">
      <div className="admin-head">
        <h1>{t('admin.audit.title')}</h1>
      </div>
      <section className="cel stack">
        {items.length === 0 && !loading ? (
          <p className="empty">{t('admin.audit.empty')}</p>
        ) : (
          <ul className="list" aria-busy={loading}>
            {items.map((a) => (
              <li key={a.id} className="list__row">
                <span className="stamp stamp--small stamp--ink">{a.action}</span>
                <span className="list__main">
                  <span>
                    {a.admin_username}
                    {a.target_user_id !== undefined && (
                      <>
                        <ArrowRight size={16} strokeWidth={2.5} aria-hidden style={{ verticalAlign: '-3px', margin: '0 6px' }} />
                        <Link to={`/accounts/${a.target_user_id}`}>{t('admin.audit.target', { id: a.target_user_id })}</Link>
                      </>
                    )}
                  </span>
                  <span className="muted mono">
                    {date.format(new Date(a.at))} · {a.request_id}
                  </span>
                </span>
                {a.amount !== undefined && <Money value={a.amount} />}
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

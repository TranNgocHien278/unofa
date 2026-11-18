import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { AnimatePresence } from 'framer-motion'
import { clearFlag, codeMetrics, flags as listFlags, requestMetrics, type Flag } from '../../src/api/admin'
import { isApiError } from '../../src/api/client'
import { Dialog } from '../../src/components/Dialog'
import { errorText } from '../../src/i18n'
import { codeClassRows, sumKeys, topSeries } from '../buckets'
import { useAdmin } from '../state'

// Stat tiles from today's server buckets. There is no money-totals endpoint, so
// the dashboard shows traffic and the open flags, nothing invented.
function Stats() {
  const { t, i18n } = useTranslation()
  const [v, setV] = useState<{ req: number; c4: number; c5: number } | null>(null)
  useEffect(() => {
    Promise.all([requestMetrics('Today'), codeMetrics('Today')])
      .then(([r, c]) => {
        const req = topSeries(r.buckets, Infinity)
        const codes = codeClassRows(c.buckets)
        setV({ req: sumKeys(req.rows, req.keys), c4: sumKeys(codes, ['4xx']), c5: sumKeys(codes, ['5xx']) })
      })
      .catch(() => {})
  }, [])
  const nf = new Intl.NumberFormat(i18n.language)
  const tiles = [
    ['admin.dash.requests', v?.req],
    ['admin.dash.clientErrors', v?.c4],
    ['admin.dash.serverErrors', v?.c5],
  ] as const
  return (
    <section aria-labelledby="today">
      <h2 id="today">{t('admin.dash.today')}</h2>
      <div className="tiles">
        {tiles.map(([k, n]) => (
          <div key={k} className="cel tile">
            <span className="muted">{t(k)}</span>
            <strong className="tile__n num">{n === undefined ? '–' : nf.format(n)}</strong>
          </div>
        ))}
      </div>
    </section>
  )
}

function detailText(d: Record<string, unknown>) {
  return Object.entries(d)
    .map(([k, v]) => `${k}: ${typeof v === 'object' ? JSON.stringify(v) : String(v)}`)
    .join(' · ')
}

export function Dashboard() {
  const { t, i18n } = useTranslation()
  const [items, setItems] = useState<Flag[] | null>(null)
  const [target, setTarget] = useState<Flag | null>(null)
  const [busy, setBusy] = useState(false)
  const date = new Intl.DateTimeFormat(i18n.language, { dateStyle: 'medium', timeStyle: 'short' })

  const load = useCallback(() => {
    listFlags()
      .then((r) => setItems(r.items))
      .catch(() => {})
  }, [])
  useEffect(load, [load])

  async function clear() {
    if (!target) return
    setBusy(true)
    try {
      await clearFlag(target.id)
      useAdmin.getState().toast(t('admin.dash.cleared'))
      setTarget(null)
      load()
    } catch (err) {
      if (isApiError(err)) useAdmin.getState().toast(errorText(err.code), 'alert', err.requestId)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="admin-page">
      <Stats />
      <section className="cel stack" aria-labelledby="flags">
        <h2 id="flags">{t('admin.dash.flagsTitle')}</h2>
        {items === null ? (
          <span className="spinner" />
        ) : items.length === 0 ? (
          <p className="empty">{t('admin.dash.noFlags')}</p>
        ) : (
          <ul className="list">
            {items.map((f) => (
              <li key={f.id} className="list__row">
                <span className={`stamp stamp--small ${f.kind === 'MONEY_INVARIANT' || f.kind === 'REDIS_DOWN' ? 'stamp--alert' : 'stamp--ink'}`}>
                  {t(`admin.dash.kind.${f.kind}`, { defaultValue: f.kind })}
                </span>
                <span className="list__main">
                  <span className="mono">{detailText(f.detail)}</span>
                  <span className="muted">{date.format(new Date(f.at))}</span>
                </span>
                <button type="button" className="btn btn--small" onClick={() => setTarget(f)}>
                  {t('admin.dash.clear')}
                </button>
              </li>
            ))}
          </ul>
        )}
      </section>
      <AnimatePresence>
        {target && (
          <Dialog title={t('admin.dash.clearTitle')} description={t('admin.dash.clearBody')} onClose={() => setTarget(null)}>
            <div className="dialog-actions">
              <button type="button" className="btn" onClick={() => setTarget(null)} autoFocus>
                {t('common.cancel')}
              </button>
              <button type="button" className="btn btn--primary" onClick={clear} disabled={busy} aria-busy={busy}>
                {t('admin.dash.clear')}
              </button>
            </div>
          </Dialog>
        )}
      </AnimatePresence>
    </div>
  )
}

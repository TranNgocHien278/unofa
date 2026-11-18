import { useId, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import type { Range } from '../api/types'

export const RANGES: Range[] = ['Today', '24h', '3d', '7d', '30d', '60d']

// Categorical order is fixed and never cycled (dataviz reference palette,
// validated on --paper: CVD worst adjacent ΔE 9.1, normal 19.6). Three slots sit
// under 3:1 on paper, so every chart also ships a table view.
export const SERIES = ['#2a78d6', '#eb6834', '#1baf7a', '#eda100', '#e87ba4', '#008300', '#6250d6', '#e34948']
export const OTHER = '#898781'
// Reserved for HTTP status classes; never reused for a categorical series.
export const STATUS = { '2xx': '#0ca30c', '3xx': '#898781', '4xx': '#fab219', '5xx': '#d03b3b' } as const

export const AXIS = { stroke: '#c3c2b7', tick: { fill: '#6b6a64', fontSize: 12 }, tickLine: false } as const
export const GRID = { stroke: '#e1e0d9', strokeDasharray: undefined, vertical: false } as const

export interface TableView {
  columns: string[]
  rows: (string | number)[][]
}

interface Props {
  title: string
  // Omit both when one range row above several charts scopes them all.
  range?: Range
  onRangeChange?(r: Range): void
  loading?: boolean
  empty?: boolean
  table: TableView
  height?: number
  // Required for two or more series: identity never rides on colour alone.
  legend?: { key: string; label: string; color: string }[]
  children: ReactNode
}

// ChartFrame is the one place charts get their title, range selector, table view
// and loading treatment (06 §Chart). While refetching, the previous render stays
// at reduced opacity: no skeleton, no layout jump.
export function ChartFrame({ title, range, onRangeChange, loading, empty, table, height = 240, legend, children }: Props) {
  const { t } = useTranslation()
  const id = useId()
  return (
    <section className="cel chart" aria-labelledby={id}>
      <div className="chart__head">
        <h2 id={id}>{title}</h2>
        {range && onRangeChange && <RangeChips range={range} onChange={onRangeChange} />}
      </div>
      <div className="chart__plot" style={{ height, opacity: loading ? 0.5 : 1 }} aria-hidden>
        {empty ? <p className="empty">{t('chart.empty')}</p> : children}
      </div>
      {legend && legend.length > 1 && !empty && (
        <ul className="legend">
          {legend.map((i) => (
            <li key={i.key}>
              <span className="legend__swatch" style={{ background: i.color }} aria-hidden />
              {i.label}
            </li>
          ))}
        </ul>
      )}
      {!empty && (
        <details className="chart__table">
          <summary>{t('chart.table')}</summary>
          <div className="table-scroll">
            <table>
              <thead>
                <tr>
                  {table.columns.map((c) => (
                    <th key={c} scope="col">
                      {c}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {table.rows.map((r, i) => (
                  <tr key={i}>
                    {r.map((v, j) => (
                      <td key={j} className={typeof v === 'number' ? 'num' : undefined}>
                        {v}
                      </td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </details>
      )}
    </section>
  )
}

export function RangeChips({ range, onChange }: { range: Range; onChange(r: Range): void }) {
  const { t } = useTranslation()
  return (
    <div className="chips chips--tight" role="radiogroup" aria-label={t('chart.range')}>
      {RANGES.map((r) => (
        <button key={r} type="button" role="radio" className="chip chip--small" aria-checked={range === r} onClick={() => onChange(r)}>
          {t(`chart.ranges.${r}`)}
        </button>
      ))}
    </div>
  )
}

interface TipRow {
  name?: string | number
  value?: number | string | (number | string)[]
  color?: string
  stroke?: string
  fill?: string
}

// One tooltip for every chart: value first, series name after, a short line key.
export function ChartTooltip({
  active,
  payload,
  label,
  formatLabel,
  formatValue,
}: {
  active?: boolean
  payload?: TipRow[]
  label?: string | number
  formatLabel(l: string | number): string
  formatValue(v: number): string
}) {
  if (!active || !payload?.length || label == null) return null
  const rows = payload.filter((p) => typeof p.value === 'number' && p.value !== 0)
  return (
    <div className="chart-tip">
      <p className="chart-tip__label">{formatLabel(label)}</p>
      {(rows.length ? rows : payload.slice(0, 1)).map((p, i) => (
        <p key={i} className="chart-tip__row">
          <span className="chart-tip__key" style={{ background: p.stroke ?? p.fill ?? p.color }} aria-hidden />
          <strong className="num">{formatValue(Number(p.value ?? 0))}</strong>
          {payload.length > 1 && <span className="chart-tip__name">{String(p.name)}</span>}
        </p>
      ))}
    </div>
  )
}

// Tick and tooltip time labels scale with the range: hours for a day, dates beyond.
export function timeFormat(range: Range, locale: string) {
  const short = range === 'Today' || range === '24h'
  const tick = new Intl.DateTimeFormat(locale, short ? { hour: '2-digit', minute: '2-digit' } : { day: '2-digit', month: '2-digit' })
  const full = new Intl.DateTimeFormat(locale, { day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit' })
  return {
    tick: (v: string | number) => tick.format(new Date(v)),
    full: (v: string | number) => full.format(new Date(v)),
  }
}

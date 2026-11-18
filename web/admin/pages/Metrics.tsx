import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useReducedMotion } from 'framer-motion'
import { Area, AreaChart, Bar, BarChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import { codeMetrics, requestMetrics } from '../../src/api/admin'
import type { Range } from '../../src/api/types'
import { AXIS, ChartFrame, ChartTooltip, GRID, OTHER, RangeChips, SERIES, STATUS, timeFormat } from '../../src/components/ChartFrame'
import { CLASSES, codeClassRows, OTHER_KEY, sumKeys, topSeries, type Row } from '../buckets'

const GAP = { stroke: '#fafafa', strokeWidth: 2 }

export function Metrics() {
  const { t, i18n } = useTranslation()
  const reduce = useReducedMotion()
  const [range, setRange] = useState<Range>('Today')
  const [req, setReq] = useState<{ keys: string[]; rows: Row[] } | null>(null)
  const [codes, setCodes] = useState<Row[] | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let off = false
    setLoading(true)
    Promise.all([requestMetrics(range), codeMetrics(range)])
      .then(([r, c]) => {
        if (off) return
        setReq(topSeries(r.buckets))
        setCodes(codeClassRows(c.buckets))
      })
      .catch(() => {})
      .finally(() => !off && setLoading(false))
    return () => {
      off = true
    }
  }, [range])

  const fmt = timeFormat(range, i18n.language)
  const nf = new Intl.NumberFormat(i18n.language)
  const pf = new Intl.NumberFormat(i18n.language, { style: 'percent', maximumFractionDigits: 0 })
  const name = (k: string) => (k === OTHER_KEY ? t('admin.metrics.other') : k)
  const color = (k: string, i: number) => (k === OTHER_KEY ? OTHER : SERIES[i])
  const xAxis = (
    <XAxis
      dataKey="t"
      type="number"
      scale="time"
      domain={['dataMin', 'dataMax']}
      padding={{ left: 16, right: 16 }}
      tickFormatter={fmt.tick}
      minTickGap={32}
      {...AXIS}
    />
  )
  const tip = (
    <Tooltip cursor={{ fill: 'oklch(0.9 0.01 90 / 0.5)' }} content={<ChartTooltip formatLabel={(l) => fmt.full(l)} formatValue={(v) => nf.format(v)} />} />
  )
  const loadingFrame = loading && req !== null
  const classItems = CLASSES.map((c) => ({ key: c, label: c, color: STATUS[c] }))
  // A quiet bucket has no share: skip it rather than plot a fake 0%.
  const share = (codes ?? []).filter((r) => sumKeys([r], CLASSES) > 0)

  return (
    <div className="admin-page">
      <div className="admin-head">
        <h1>{t('admin.metrics.title')}</h1>
        <RangeChips range={range} onChange={setRange} />
      </div>

      <ChartFrame
        title={t('admin.metrics.requests')}
        legend={req?.keys.map((k, i) => ({ key: k, label: name(k), color: color(k, i) }))}
        loading={loadingFrame}
        empty={req !== null && req.rows.length === 0}
        height={280}
        table={{
          columns: [t('admin.metrics.endpoint'), t('admin.metrics.count')],
          rows: (req?.keys ?? []).map((k) => [name(k), nf.format(req!.rows.reduce((s, r) => s + (r[k] ?? 0), 0))]),
        }}
      >
        <ResponsiveContainer width="100%" height="100%">
          <BarChart data={req?.rows ?? []} margin={{ top: 8, right: 12, bottom: 0, left: 0 }}>
            <CartesianGrid {...GRID} />
            {xAxis}
            <YAxis width={48} allowDecimals={false} tickFormatter={(v: number) => nf.format(v)} {...AXIS} />
            {tip}
            {req?.keys.map((k, i) => (
              <Bar
                key={k}
                dataKey={k}
                name={name(k)}
                stackId="r"
                fill={color(k, i)}
                maxBarSize={24}
                radius={i === req.keys.length - 1 ? [4, 4, 0, 0] : 0}
                isAnimationActive={!reduce}
                {...GAP}
              />
            ))}
          </BarChart>
        </ResponsiveContainer>
      </ChartFrame>

      <ChartFrame
        title={t('admin.metrics.codesShare')}
        legend={classItems}
        loading={loadingFrame}
        empty={codes !== null && share.length === 0}
        table={{ columns: [t('chart.time'), ...CLASSES], rows: share.map((r) => [fmt.full(r.t), ...CLASSES.map((c) => nf.format(r[c]))]) }}
      >
        {share.length === 1 ? (
          <p className="empty">{t('admin.metrics.shareNeedsTwo')}</p>
        ) : (
          <ResponsiveContainer width="100%" height="100%">
            <AreaChart data={share} stackOffset="expand" margin={{ top: 8, right: 12, bottom: 0, left: 0 }}>
              <CartesianGrid {...GRID} />
              {xAxis}
              <YAxis width={48} tickFormatter={(v: number) => pf.format(v)} {...AXIS} />
              <Tooltip
                cursor={{ stroke: '#898781', strokeWidth: 1 }}
                content={<ChartTooltip formatLabel={(l) => fmt.full(l)} formatValue={(v) => nf.format(v)} />}
              />
              {CLASSES.map((c) => (
                <Area
                  key={c}
                  type="monotone"
                  dataKey={c}
                  name={c}
                  stackId="c"
                  stroke="#fafafa"
                  strokeWidth={1}
                  fill={STATUS[c]}
                  fillOpacity={0.85}
                  isAnimationActive={!reduce}
                />
              ))}
            </AreaChart>
          </ResponsiveContainer>
        )}
      </ChartFrame>

      <ChartFrame
        title={t('admin.metrics.codesCount')}
        legend={classItems}
        loading={loadingFrame}
        empty={codes !== null && codes.length === 0}
        table={{
          columns: [t('admin.metrics.code'), t('admin.metrics.count')],
          rows: CLASSES.map((c) => [c, nf.format((codes ?? []).reduce((s, r) => s + r[c], 0))]),
        }}
      >
        <ResponsiveContainer width="100%" height="100%">
          <BarChart data={codes ?? []} margin={{ top: 8, right: 12, bottom: 0, left: 0 }}>
            <CartesianGrid {...GRID} />
            {xAxis}
            <YAxis width={48} allowDecimals={false} tickFormatter={(v: number) => nf.format(v)} {...AXIS} />
            {tip}
            {CLASSES.map((c, i) => (
              <Bar
                key={c}
                dataKey={c}
                name={c}
                stackId="k"
                fill={STATUS[c]}
                maxBarSize={24}
                radius={i === CLASSES.length - 1 ? [4, 4, 0, 0] : 0}
                isAnimationActive={!reduce}
                {...GAP}
              />
            ))}
          </BarChart>
        </ResponsiveContainer>
      </ChartFrame>
    </div>
  )
}

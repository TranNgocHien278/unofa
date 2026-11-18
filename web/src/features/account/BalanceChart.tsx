import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useReducedMotion } from 'framer-motion'
import { Area, AreaChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import { balanceChart } from '../../api/money'
import type { Range } from '../../api/types'
import { AXIS, ChartFrame, ChartTooltip, GRID, SERIES, timeFormat } from '../../components/ChartFrame'
import { formatU } from '../../i18n'
import { useSession } from '../../store/session'

type Point = { at: string; balance: number }

export function BalanceChart() {
  const { t, i18n } = useTranslation()
  const reduce = useReducedMotion()
  const balance = useSession((s) => s.user?.balance)
  const [range, setRange] = useState<Range>('30d')
  const [points, setPoints] = useState<Point[] | null>(null)
  const [loading, setLoading] = useState(true)

  // Refetch when the range or the balance changes, so a payout shows up here too.
  useEffect(() => {
    let off = false
    setLoading(true)
    balanceChart(range)
      .then((r) => !off && setPoints(r.points))
      .catch(() => {})
      .finally(() => !off && setLoading(false))
    return () => {
      off = true
    }
  }, [range, balance])

  const fmt = timeFormat(range, i18n.language)
  const data = (points ?? []).map((p) => ({ ...p, t: Date.parse(p.at) }))
  // Carry the line to now at the live balance, so one bucket still draws a step.
  const last = data[data.length - 1]
  if (last && balance !== undefined && Date.now() > last.t) data.push({ at: new Date().toISOString(), balance, t: Date.now() })
  const color = SERIES[0]
  return (
    <ChartFrame
      title={t('account.chartTitle')}
      range={range}
      onRangeChange={setRange}
      loading={loading && points !== null}
      empty={points !== null && data.length === 0}
      table={{ columns: [t('chart.time'), t('account.balance')], rows: data.map((p) => [fmt.full(p.t), formatU(p.balance)]) }}
    >
      <ResponsiveContainer width="100%" height="100%">
        <AreaChart data={data} margin={{ top: 12, right: 12, bottom: 0, left: 0 }}>
          <CartesianGrid {...GRID} />
          <XAxis dataKey="t" type="number" scale="time" domain={['dataMin', 'dataMax']} tickFormatter={fmt.tick} minTickGap={32} {...AXIS} />
          <YAxis width={56} tickFormatter={(v: number) => new Intl.NumberFormat(i18n.language, { notation: 'compact' }).format(v)} allowDecimals={false} {...AXIS} />
          <Tooltip
            cursor={{ stroke: '#898781', strokeWidth: 1 }}
            content={<ChartTooltip formatLabel={(l) => fmt.full(l)} formatValue={(v) => formatU(v)} />}
          />
          <Area
            type="stepAfter"
            dataKey="balance"
            name={t('account.balance')}
            stroke={color}
            strokeWidth={2}
            fill={color}
            fillOpacity={0.1}
            dot={false}
            activeDot={{ r: 5, stroke: 'var(--paper)', strokeWidth: 2, fill: color }}
            isAnimationActive={!reduce}
          />
        </AreaChart>
      </ResponsiveContainer>
    </ChartFrame>
  )
}

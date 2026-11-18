import { api } from './client'
import type { MatchRow, Page, Range, Transaction } from './types'

export const transfer = (to_username: string, amount: number, client_uuid: string) =>
  api<{ amount: number; to_username: string; balance_after: number }>('/api/transfers', {
    method: 'POST',
    json: { to_username, amount, client_uuid },
  })

export function transactions(q: { before?: string | number | null; reason?: string[]; direction?: string }) {
  const p = new URLSearchParams({ limit: '30' })
  if (q.before != null) p.set('before', String(q.before))
  if (q.reason?.length) p.set('reason', q.reason.join(','))
  if (q.direction) p.set('direction', q.direction)
  return api<Page<Transaction>>(`/api/transactions?${p}`)
}

export const balanceChart = (range: Range) =>
  api<{ points: { at: string; balance: number }[] }>(`/api/me/balance-chart?range=${range}`)

export const myMatches = (before?: string | number | null) =>
  api<Page<MatchRow>>(`/api/me/matches?limit=20${before ? `&before=${encodeURIComponent(String(before))}` : ''}`)

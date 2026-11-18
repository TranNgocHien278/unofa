import { api } from './client'
import type { Page, Range } from './types'

export interface AdminUser {
  user_id: number
  username: string
  balance: number
  locale: string
  terms_version: number
  created_at: string
  login_locked_until?: string
  deleted_at?: string
}

export interface AdminDetail {
  user: AdminUser
  active_loan: { loan_id: number; principal: number; due_at: string; owed_now: number; overdue: boolean } | null
  transactions: { transaction_id: number; reason: string; amount: number; direction: 'in' | 'out'; at: string }[]
}

export interface Flag {
  id: number
  kind: string
  detail: Record<string, unknown>
  at: string
}

export interface AuditRow {
  id: number
  admin_username: string
  action: string
  target_user_id?: number
  amount?: number
  at: string
  request_id: string
}

export type Buckets<K extends string> = { buckets: ({ at: string } & Record<K, Record<string, number>>)[] }

export const adminLogin = (username: string, password: string) => api<void>('/api/admin/login', { method: 'POST', json: { username, password } })
export const adminLogout = () => api<void>('/api/admin/logout', { method: 'POST' })
export const adminHeartbeat = () => api<void>('/api/admin/session/heartbeat', { method: 'POST' })
export const requestMetrics = (range: Range) => api<Buckets<'by_endpoint'>>(`/api/admin/metrics/requests?range=${range}`)
export const codeMetrics = (range: Range) => api<Buckets<'by_code'>>(`/api/admin/metrics/status-codes?range=${range}`)

export function accounts(q: string, before?: number | null) {
  const p = new URLSearchParams({ limit: '30' })
  if (q) p.set('q', q)
  if (before) p.set('before', String(before))
  return api<Page<AdminUser>>(`/api/admin/accounts?${p}`)
}
export const account = (id: number) => api<AdminDetail>(`/api/admin/accounts/${id}`)
// The request id doubles as the idempotency key (04 §Admin): one id per intended adjustment.
export const adjustBalance = (id: number, delta: number, requestId: string) =>
  api<{ balance: number }>(`/api/admin/accounts/${id}/balance`, { method: 'POST', json: { delta }, headers: { 'X-Request-ID': requestId } })
export const deleteAccount = (id: number) => api<void>(`/api/admin/accounts/${id}`, { method: 'DELETE' })
export const flags = () => api<{ items: Flag[] }>('/api/admin/flags')
export const clearFlag = (id: number) => api<void>(`/api/admin/flags/${id}/clear`, { method: 'POST' })
export const audit = (before?: number | null) => api<Page<AuditRow>>(`/api/admin/audit?limit=50${before ? `&before=${before}` : ''}`)

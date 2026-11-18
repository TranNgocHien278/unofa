import { api } from './client'
import type { ActiveLoan, LoanProducts } from './types'

export const products = () => api<LoanProducts>('/api/loans/products')
export const active = () => api<ActiveLoan | null>('/api/loans/active')
export const borrow = (principal: number, term: string) =>
  api<{ loan_id: number; due_at: string; owed_now: number; balance_after: number }>('/api/loans', {
    method: 'POST',
    json: { principal, term },
  })
export const repay = (id: number) =>
  api<{ owed_paid: number; balance_after: number }>(`/api/loans/${id}/repay`, { method: 'POST' })

import { api } from './client'
import type { Locale, Me } from './types'

export const register = (username: string, password: string) =>
  api<{ user_id: number; username: string; balance: number }>('/api/register', {
    method: 'POST',
    json: { username, password, terms_accepted: true },
  })

export const login = (username: string, password: string, termsVersion?: number) =>
  api<Omit<Me, 'active_loan'>>('/api/login', {
    method: 'POST',
    json: { username, password, terms_version: termsVersion },
  })

export const logout = () => api<void>('/api/logout', { method: 'POST' })
export const heartbeat = () => api<void>('/api/session/heartbeat', { method: 'POST' })
export const me = () => api<Me>('/api/me')

export const setLocale = (locale: Locale) => api<{ locale: Locale }>('/api/me', { method: 'PATCH', json: { locale } })
export const changeUsername = (username: string, current_password: string) =>
  api<{ username: string }>('/api/me', { method: 'PATCH', json: { username, current_password } })
export const changePassword = (new_password: string, current_password: string) =>
  api<void>('/api/me', { method: 'PATCH', json: { new_password, current_password } })

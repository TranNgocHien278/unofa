import { api } from './client'
import type { Room } from './types'

const r = (code: string) => `/api/rooms/${encodeURIComponent(code)}`

export const createRoom = (stake: number) =>
  api<{ room_id: string; code: string; stake: number }>('/api/rooms', { method: 'POST', json: { stake } })
export const joinRoom = (code: string) => api<Room>(`${r(code)}/join`, { method: 'POST' })
export const leaveRoom = (code: string) => api<void>(`${r(code)}/leave`, { method: 'POST' })
export const setReady = (code: string, ready: boolean) => api<void>(`${r(code)}/ready`, { method: 'POST', json: { ready } })
export const setStake = (code: string, stake: number) => api<void>(`${r(code)}/stake`, { method: 'POST', json: { stake } })
export const kick = (code: string, user_id: number) => api<void>(`${r(code)}/kick`, { method: 'POST', json: { user_id } })
export const play = (code: string) => api<{ match_id: string }>(`${r(code)}/play`, { method: 'POST' })
export const getRoom = (code: string) => api<Room>(r(code))

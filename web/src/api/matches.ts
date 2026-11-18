import { api } from './client'
import type { MatchView } from './types'

export const matchState = (id: string) => api<MatchView>(`/api/matches/${encodeURIComponent(id)}/state`)

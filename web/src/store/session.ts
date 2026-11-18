import { create } from 'zustand'
import type { ActiveLoan, Locale, Me } from '../api/types'

export interface User {
  userId: number
  username: string
  balance: number
  locale: Locale
}

interface SessionState {
  user: User | null
  status: 'loading' | 'authenticated' | 'anonymous'
  termsVersion: number
  activeLoan: ActiveLoan | null
  // Last balance change, for the up/down arrow next to the balance (06 §Account).
  lastDelta: number
  setMe(me: Omit<Me, 'active_loan'> & { active_loan?: ActiveLoan | null }): void
  setBalance(balance: number): void
  patch(p: Partial<User>): void
  setAnonymous(): void
}

export const useSession = create<SessionState>((set, get) => ({
  user: null,
  status: 'loading',
  termsVersion: 1,
  activeLoan: null,
  lastDelta: 0,
  setMe(me) {
    const prev = get().user
    set({
      user: { userId: me.user_id, username: me.username, balance: me.balance, locale: me.locale },
      status: 'authenticated',
      termsVersion: me.terms_version,
      activeLoan: me.active_loan === undefined ? get().activeLoan : me.active_loan,
      lastDelta: prev && prev.userId === me.user_id ? me.balance - prev.balance || get().lastDelta : 0,
    })
  },
  setBalance(balance) {
    const u = get().user
    if (u && u.balance !== balance) set({ user: { ...u, balance }, lastDelta: balance - u.balance })
  },
  patch(p) {
    const u = get().user
    if (u) set({ user: { ...u, ...p } })
  },
  setAnonymous: () => set({ user: null, status: 'anonymous', activeLoan: null, lastDelta: 0 }),
}))

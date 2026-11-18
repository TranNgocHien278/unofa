import { create } from 'zustand'

interface Toast {
  id: number
  text: string
  tone: 'info' | 'alert'
  requestId?: string
}

interface AdminState {
  status: 'loading' | 'in' | 'out'
  toasts: Toast[]
  setStatus(s: AdminState['status']): void
  toast(text: string, tone?: Toast['tone'], requestId?: string): void
  dismiss(id: number): void
}

let nextId = 1

// Admin keeps its own tiny store: it never shares state with the game app (06 §Cấu trúc).
export const useAdmin = create<AdminState>((set, get) => ({
  status: 'loading',
  toasts: [],
  setStatus: (status) => set({ status }),
  toast(text, tone = 'info', requestId) {
    const id = nextId++
    set({ toasts: [...get().toasts.slice(-2), { id, text, tone, requestId }] })
    setTimeout(() => get().dismiss(id), tone === 'alert' ? 6000 : 3500)
  },
  dismiss: (id) => set({ toasts: get().toasts.filter((t) => t.id !== id) }),
}))

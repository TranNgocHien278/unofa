import { create } from 'zustand'

export interface Toast {
  id: number
  text: string
  tone: 'info' | 'alert'
  requestId?: string
}

interface UiState {
  toasts: Toast[]
  reloginOpen: boolean
  sound: boolean
  toast(text: string, tone?: Toast['tone'], requestId?: string): void
  dismiss(id: number): void
  openRelogin(): void
  closeRelogin(): void
  setSound(on: boolean): void
}

let nextId = 1

const readSound = () => {
  try {
    return localStorage.getItem('unofa.sound') === '1'
  } catch {
    return false
  }
}

export const useUi = create<UiState>((set, get) => ({
  toasts: [],
  reloginOpen: false,
  sound: readSound(),
  toast(text, tone = 'info', requestId) {
    const id = nextId++
    set({ toasts: [...get().toasts.slice(-2), { id, text, tone, requestId }] })
    setTimeout(() => get().dismiss(id), tone === 'alert' ? 6000 : 3500)
  },
  dismiss: (id) => set({ toasts: get().toasts.filter((t) => t.id !== id) }),
  openRelogin: () => set({ reloginOpen: true }),
  closeRelogin: () => set({ reloginOpen: false }),
  setSound(on) {
    try {
      localStorage.setItem('unofa.sound', on ? '1' : '0')
    } catch {
      // private mode: the switch still works for this tab
    }
    set({ sound: on })
  },
}))

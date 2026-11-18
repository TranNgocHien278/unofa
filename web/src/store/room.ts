import { create } from 'zustand'

// The room this player sits in, learned from `hello` or from joining. The table
// reads usernames from GET /api/rooms/{code}; match views carry user ids only.
interface RoomState {
  code: string | null
  // Set while the asset handshake runs (05 §ready_check), so pages can show
  // "Preparing the table…".
  preparing: boolean
  setCode(code: string | null): void
  setPreparing(on: boolean): void
}

export const useRoom = create<RoomState>((set) => ({
  code: null,
  preparing: false,
  setCode: (code) => set({ code }),
  setPreparing: (preparing) => set({ preparing }),
}))

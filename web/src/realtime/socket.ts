import type { ServerFrame } from '../api/types'

interface Opts {
  url: string
  onFrame(f: ServerFrame): void
  onOpen?(): void
  onClose?(info: { code: number; reason: string; willRetry: boolean }): void
}

const BACKOFF = [1000, 2000, 4000, 8000, 16000]
// Past the 90s reconnect grace the seat is forfeited anyway (05 §Reconnect).
const GIVE_UP_MS = 90_000

// Socket is the one WebSocket of the app (06 §Realtime wrapper): it reconnects
// with exponential backoff and ±20% jitter, and stops for good on SESSION_REPLACED.
export class Socket {
  private ws: WebSocket | null = null
  private attempt = 0
  private downSince = 0
  private timer: ReturnType<typeof setTimeout> | undefined
  private stopped = false

  constructor(private o: Opts) {
    this.open()
  }

  get connected() {
    return this.ws?.readyState === WebSocket.OPEN
  }

  send(frame: object): boolean {
    if (!this.connected) return false
    this.ws!.send(JSON.stringify(frame))
    return true
  }

  close() {
    this.stopped = true
    clearTimeout(this.timer)
    this.ws?.close(1000)
    this.ws = null
  }

  // retryNow skips the remaining backoff, e.g. when the tab becomes visible again.
  retryNow() {
    if (this.stopped || this.connected || this.ws?.readyState === WebSocket.CONNECTING) return
    clearTimeout(this.timer)
    this.open()
  }

  private open() {
    const ws = new WebSocket(this.o.url)
    this.ws = ws
    ws.onopen = () => {
      this.attempt = 0
      this.downSince = 0
      this.o.onOpen?.()
    }
    ws.onmessage = (m) => {
      try {
        this.o.onFrame(JSON.parse(m.data as string) as ServerFrame)
      } catch (err) {
        console.error('bad frame', err)
      }
    }
    ws.onclose = (ev) => {
      if (this.ws !== ws) return
      this.ws = null
      const replaced = ev.code === 1000 && ev.reason === 'SESSION_REPLACED'
      const fatal = this.stopped || replaced || ev.code === 1009 || ev.reason === 'PASSWORD_CHANGED'
      if (!this.downSince) this.downSince = Date.now()
      const giveUp = Date.now() - this.downSince > GIVE_UP_MS
      const willRetry = !fatal && !giveUp
      this.o.onClose?.({ code: ev.code, reason: ev.reason, willRetry })
      if (!willRetry) return
      // 1008 means the server rate-limited this socket; wait out the window.
      const base = ev.code === 1008 ? 60_000 : BACKOFF[Math.min(this.attempt, BACKOFF.length - 1)]
      this.attempt++
      const jitter = base * (Math.random() * 0.4 - 0.2)
      this.timer = setTimeout(() => this.open(), base + jitter)
    }
  }
}

export const wsURL = () => `${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}/api/ws`

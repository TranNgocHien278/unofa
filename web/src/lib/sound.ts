import { useUi } from '../store/ui'

type Kind = 'play' | 'draw' | 'win' | 'warn'

// Synthesized blips: no audio files to fetch, and nothing plays until the player
// turns sound on, which is itself a user gesture (06 §Asset).
const notes: Record<Kind, [number, number][]> = {
  play: [[520, 0.07], [780, 0.09]],
  draw: [[330, 0.08]],
  win: [[523, 0.1], [659, 0.1], [784, 0.18]],
  warn: [[880, 0.06], [880, 0.06]],
}

let ctx: AudioContext | null = null

export function sound(kind: Kind) {
  if (!useUi.getState().sound) return
  try {
    ctx ??= new AudioContext()
    let t = ctx.currentTime
    for (const [freq, dur] of notes[kind]) {
      const osc = ctx.createOscillator()
      const gain = ctx.createGain()
      osc.type = 'triangle'
      osc.frequency.value = freq
      gain.gain.setValueAtTime(0.15, t)
      gain.gain.exponentialRampToValueAtTime(0.001, t + dur)
      osc.connect(gain).connect(ctx.destination)
      osc.start(t)
      osc.stop(t + dur)
      t += dur
    }
  } catch {
    // no audio device: stay silent
  }
}

import { useLayoutEffect, useRef } from 'react'
import { useReducedMotion } from 'framer-motion'
import { CardBack, CardFace } from '../../components/Card'
import type { Flight } from '../../store/match'

interface Props {
  flights: Flight[]
  // The real elements a card leaves and lands on, measured at take-off.
  from(): Element | null
  to(f: Flight): Element | null
}

// DrawFlights draws the cards in the air between the draw pile and a pile on the
// table. The store keeps the clock and the counts; this only animates between the
// two measured rects, so no position is hard-coded.
export function DrawFlights({ flights, from, to }: Props) {
  return (
    <div className="flights" aria-hidden>
      {flights.map((f) => (
        <FlyingCard key={f.id} f={f} from={from} to={to} />
      ))}
    </div>
  )
}

function FlyingCard({ f, from, to }: Omit<Props, 'flights'> & { f: Flight }) {
  const ref = useRef<HTMLSpanElement>(null)
  const reduce = useReducedMotion()

  useLayoutEffect(() => {
    const el = ref.current
    const a = from()?.getBoundingClientRect()
    const b = to(f)?.getBoundingClientRect()
    if (!el || !a || !b) return
    // Laid out on the destination; the flight is a transform from the draw pile.
    Object.assign(el.style, { left: `${b.left}px`, top: `${b.top}px`, width: `${b.width}px`, height: `${b.height}px` })
    if (typeof el.animate !== 'function') return
    // A card mounted late (the table opened mid-flight) joins at its current point.
    const timing: KeyframeAnimationOptions = {
      duration: f.dur,
      delay: f.delay - (performance.now() - f.at),
      easing: 'cubic-bezier(0.3, 0.7, 0.4, 1)',
      fill: 'both',
    }
    if (reduce) {
      el.animate([{ opacity: 0 }, { opacity: 1 }], timing)
      return
    }
    const dx = a.left + a.width / 2 - (b.left + b.width / 2)
    const dy = a.top + a.height / 2 - (b.top + b.height / 2)
    const s = a.width / b.width
    const lift = Math.min(80, Math.hypot(dx, dy) * 0.2)
    // A tossed arc: straight line from the pile plus a parabola that peaks midway.
    const frames = [0, 0.25, 0.5, 0.75, 1].map((t) => ({
      offset: t,
      transform: `translate(${dx * (1 - t)}px, ${dy * (1 - t) - lift * 4 * t * (1 - t)}px) scale(${s + (1 - s) * t}) rotate(${-12 * (1 - t)}deg)`,
    }))
    el.animate(frames, timing)
    // Measured once at take-off: a re-render never restarts the flight.
  }, [])

  return <span ref={ref} className="flight">{f.card ? <CardFace card={f.card} /> : <CardBack />}</span>
}

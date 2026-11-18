import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

interface Props {
  deadline: string
  offsetMs: number
  totalMs?: number
  mine: boolean
}

const WARN_S = 5

// Timer counts down from an absolute server deadline corrected by the clock
// offset (05 §server_now). The bar is a transform, the digits change once per second,
// and the only spoken update is one polite warning at 5s on the player's own turn.
export function Timer({ deadline, offsetMs, totalMs = 20_000, mine }: Props) {
  const { t } = useTranslation()
  const bar = useRef<HTMLSpanElement>(null)
  const [secs, setSecs] = useState(0)
  const [warned, setWarned] = useState(false)
  const end = Date.parse(deadline)

  useEffect(() => {
    setWarned(false)
    let raf = 0
    const tick = () => {
      const left = Math.max(0, end - (Date.now() + offsetMs))
      if (bar.current) bar.current.style.transform = `scaleX(${Math.min(1, left / totalMs)})`
      setSecs(Math.ceil(left / 1000))
      if (left > 0) raf = requestAnimationFrame(tick)
    }
    tick()
    return () => cancelAnimationFrame(raf)
  }, [end, offsetMs, totalMs])

  const low = secs <= WARN_S
  useEffect(() => {
    if (low && mine && secs > 0 && !warned) setWarned(true)
  }, [low, mine, secs, warned])

  return (
    <div className={`timer ${low ? 'timer--low' : ''}`}>
      <span className="timer__track" aria-hidden>
        <span className="timer__bar" ref={bar} />
      </span>
      <span className="timer__secs num" aria-live="off">
        {t('table.seconds', { n: secs })}
      </span>
      <span className="sr-only" aria-live="polite">
        {warned ? t('table.fiveLeft') : ''}
      </span>
    </div>
  )
}

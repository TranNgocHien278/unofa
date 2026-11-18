import { forwardRef, useEffect, useImperativeHandle, useRef, useState } from 'react'
import { AnimatePresence, motion, useReducedMotion } from 'framer-motion'
import { useTranslation } from 'react-i18next'
import type { Card } from '../api/types'
import type { KeyedCard } from '../features/table/rules'
import { CardFace, cardLabel } from './Card'

interface Props {
  cards: KeyedCard[]
  isPlayable(c: Card): boolean
  pendingKey: string | null
  onPlay(k: KeyedCard, el: HTMLElement): void
}

export interface HandHandle {
  focusKey(key: string | null): void
}

// Hand is a horizontal listbox with a roving tabindex: ←/→ move, Enter plays
// (06 §Bàn phím). Playable cards stand up with their oval shadow left behind.
export const Hand = forwardRef<HandHandle, Props>(function Hand({ cards, isPlayable, pendingKey, onPlay }, ref) {
  const { t } = useTranslation()
  const reduce = useReducedMotion()
  const [active, setActive] = useState(0)
  const [nudge, setNudge] = useState<string | null>(null)
  const items = useRef(new Map<string, HTMLButtonElement>())
  const owned = useRef(false)

  const idx = Math.min(active, Math.max(0, cards.length - 1))

  useImperativeHandle(ref, () => ({
    focusKey(key) {
      const i = key ? cards.findIndex((c) => c.key === key) : -1
      const target = cards[i >= 0 ? i : idx]
      if (target) items.current.get(target.key)?.focus()
    },
  }))

  // A played card unmounts while focused; keep the keyboard user inside the hand.
  useEffect(() => {
    if (!owned.current || (document.activeElement && document.activeElement !== document.body)) return
    const k = cards[idx]
    if (k) items.current.get(k.key)?.focus()
  }, [cards, idx])

  useEffect(() => {
    if (nudge) {
      const id = setTimeout(() => setNudge(null), 350)
      return () => clearTimeout(id)
    }
  }, [nudge])

  function move(to: number) {
    const next = (to + cards.length) % cards.length
    setActive(next)
    const el = items.current.get(cards[next].key)
    el?.focus()
    el?.scrollIntoView({ block: 'nearest', inline: 'nearest', behavior: reduce ? 'auto' : 'smooth' })
  }

  function activate(k: KeyedCard, el: HTMLElement) {
    if (!isPlayable(k.card)) {
      setNudge(k.key)
      return
    }
    onPlay(k, el)
  }

  return (
    <div
      className="hand"
      style={{ ['--n' as string]: cards.length }}
      role="listbox"
      aria-label={t('table.handLabel', { count: cards.length })}
      aria-orientation="horizontal"
      onFocus={() => (owned.current = true)}
      onBlur={(e) => {
        if (e.relatedTarget && !e.currentTarget.contains(e.relatedTarget as Node)) owned.current = false
      }}
      onKeyDown={(e) => {
        if (!cards.length) return
        if (e.key === 'ArrowRight') {
          e.preventDefault()
          move(idx + 1)
        } else if (e.key === 'ArrowLeft') {
          e.preventDefault()
          move(idx - 1)
        } else if (e.key === 'Home') {
          e.preventDefault()
          move(0)
        } else if (e.key === 'End') {
          e.preventDefault()
          move(cards.length - 1)
        }
      }}
    >
      <AnimatePresence initial={false}>
        {cards.map((k, i) => {
          const can = isPlayable(k.card)
          const pending = pendingKey === k.key
          return (
            <motion.button
              key={k.key}
              layoutId={reduce ? undefined : `card-${k.key}`}
              layout={!reduce}
              ref={(el) => {
                if (el) items.current.set(k.key, el)
                else items.current.delete(k.key)
              }}
              type="button"
              role="option"
              className={`hand-card ${can ? 'hand-card--live' : ''}`}
              aria-label={cardLabel(k.card)}
              aria-selected={i === idx}
              aria-disabled={!can}
              aria-busy={pending}
              tabIndex={i === idx ? 0 : -1}
              onFocus={() => setActive(i)}
              onClick={(e) => {
                setActive(i)
                activate(k, e.currentTarget)
              }}
              onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault()
                  activate(k, e.currentTarget)
                }
              }}
              initial={reduce ? { opacity: 0 } : { opacity: 0, y: -140, rotate: -12, scale: 0.7 }}
              animate={
                nudge === k.key && !reduce
                  ? { opacity: 1, y: 0, rotate: [0, -6, 5, -3, 0], scale: 1 }
                  : { opacity: pending ? 0.5 : 1, y: 0, rotate: 0, scale: 1 }
              }
              transition={reduce ? { duration: 0.12 } : { type: 'spring', stiffness: 420, damping: 24 }}
            >
              <span className="hand-card__lift">
                <CardFace card={k.card} />
              </span>
              <span className="ground-shadow" aria-hidden />
            </motion.button>
          )
        })}
      </AnimatePresence>
    </div>
  )
})

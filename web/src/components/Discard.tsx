import { forwardRef, useState } from 'react'
import { motion, useReducedMotion } from 'framer-motion'
import { useTranslation } from 'react-i18next'
import type { Card } from '../api/types'
import { CardFace, cardLabel, colorVar, Shape } from './Card'
import type { Color } from '../api/types'

interface Props {
  top: Card
  activeColor: Color
  // Shared layoutId of the hand card that was just played, so it flies here.
  layoutId?: string
  // Where an opponent's card comes from, relative to the pile centre.
  from?: { x: number; y: number } | null
  seq: number
  playId: number
  // The round's first card is still flying here from the draw pile.
  hidden?: boolean
}

interface Laid {
  id: number
  card: Card
  rot: number
  layoutId?: string
  from?: { x: number; y: number } | null
}

// A small deterministic tilt per card makes the pile look tossed, not stacked.
const tilt = (n: number) => ((n * 37) % 17) - 8

export const Discard = forwardRef<HTMLDivElement, Props>(function Discard({ top, activeColor, layoutId, from, seq, playId, hidden }, ref) {
  const { t } = useTranslation()
  const reduce = useReducedMotion()
  // A new play is either a new play id or a different top card (snapshot after a gap).
  const sig = `${playId}|${top.color}|${top.rank}`
  const [st, setSt] = useState<{ sig: string; n: number; pile: Laid[] }>(() => ({ sig, n: 1, pile: [{ id: 0, card: top, rot: tilt(seq) }] }))
  // Derived during render, not in an effect, so the card mounts in the same commit
  // that removes it from the hand and the shared layout animation can fly it here.
  if (st.sig !== sig) {
    setSt({ sig, n: st.n + 1, pile: [...st.pile.slice(-2), { id: st.n, card: top, rot: tilt(seq + st.n), layoutId, from }] })
  }
  const pile = st.pile

  const wild = top.color === 'NONE'
  return (
    <div className={`discard ${hidden ? 'discard--waiting' : ''}`} ref={ref}>
      <span className="ground-shadow ground-shadow--pile" aria-hidden />
      {pile.map((l, i) => {
        const isTop = i === pile.length - 1
        const fly = isTop && !reduce && l.from
        return (
          <motion.div
            key={l.id}
            className="discard__card"
            layoutId={isTop && !reduce ? l.layoutId : undefined}
            style={{ zIndex: i }}
            initial={
              reduce
                ? { opacity: 0, rotate: l.rot }
                : fly
                  ? { x: l.from!.x, y: l.from!.y, scale: 0.45, rotate: l.rot - 40, opacity: 1 }
                  : l.layoutId
                    ? { rotate: 0 }
                    : false
            }
            animate={
              reduce || !isTop
                ? { opacity: 1, rotate: l.rot, x: 0, y: 0, scale: 1 }
                : { opacity: 1, rotate: l.rot, x: 0, y: 0, scaleX: [null, 1.14, 0.96, 1], scaleY: [null, 0.84, 1.05, 1] }
            }
            transition={
              reduce
                ? { duration: 0.12 }
                : {
                    x: { type: 'spring', stiffness: 260, damping: 22 },
                    y: { type: 'spring', stiffness: 260, damping: 22 },
                    rotate: { type: 'spring', stiffness: 200, damping: 16 },
                    scaleX: { duration: 0.42, times: [0, 0.55, 0.8, 1], delay: fly ? 0.16 : 0.1 },
                    scaleY: { duration: 0.42, times: [0, 0.55, 0.8, 1], delay: fly ? 0.16 : 0.1 },
                    layout: { type: 'spring', stiffness: 320, damping: 28 },
                  }
            }
          >
            <CardFace card={l.card} />
          </motion.div>
        )
      })}
      <p className="sr-only" aria-live="polite">
        {t('table.topCard', { card: cardLabel(top) })}
      </p>
      {wild && activeColor !== 'NONE' && (
        <span className="discard__chosen" style={{ background: colorVar(activeColor) }} aria-hidden>
          <svg viewBox="-40 -40 80 80" width="28" height="28">
            <Shape color={activeColor as Exclude<Color, 'NONE'>} fill="var(--card-paper)" size={0.8} sw={6} />
          </svg>
        </span>
      )}
    </div>
  )
})

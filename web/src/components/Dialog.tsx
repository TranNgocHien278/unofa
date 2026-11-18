import { useEffect, useId, useRef, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { motion, useReducedMotion } from 'framer-motion'

interface Props {
  title: ReactNode
  description?: ReactNode
  children?: ReactNode
  onClose?: () => void
  // alertdialog for decisions that cannot be dismissed (the +4 challenge).
  role?: 'dialog' | 'alertdialog'
  // Esc closes unless the dialog is a decision (06 §Dialog thách thức).
  dismissable?: boolean
  // Screen point the dialog grows from, e.g. the button that opened it.
  origin?: { x: number; y: number } | null
}

const FOCUSABLE = 'button:not([disabled]), [href], input:not([disabled]), select, textarea, [tabindex]:not([tabindex="-1"])'

export function Dialog({ title, description, children, onClose, role = 'dialog', dismissable = true, origin }: Props) {
  const ref = useRef<HTMLDivElement>(null)
  const titleId = useId()
  const descId = useId()
  const reduce = useReducedMotion()

  useEffect(() => {
    const opener = document.activeElement as HTMLElement | null
    const el = ref.current
    // autoFocus inside children wins; otherwise the first control.
    if (el && !el.contains(document.activeElement)) el.querySelector<HTMLElement>(FOCUSABLE)?.focus()
    return () => opener?.focus?.()
  }, [])

  function onKeyDown(e: React.KeyboardEvent) {
    if (e.key === 'Escape') {
      e.stopPropagation()
      if (dismissable) onClose?.()
      return
    }
    if (e.key !== 'Tab' || !ref.current) return
    const items = [...ref.current.querySelectorAll<HTMLElement>(FOCUSABLE)]
    if (!items.length) return
    const first = items[0]
    const last = items[items.length - 1]
    if (e.shiftKey && document.activeElement === first) {
      e.preventDefault()
      last.focus()
    } else if (!e.shiftKey && document.activeElement === last) {
      e.preventDefault()
      first.focus()
    }
  }

  const transformOrigin = origin ? `${origin.x}px ${origin.y}px` : '50% 100%'

  return createPortal(
    <motion.div
      className="scrim"
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      exit={{ opacity: 0 }}
      transition={{ duration: reduce ? 0.12 : 0.18 }}
      onMouseDown={(e) => {
        if (e.target === e.currentTarget && dismissable) onClose?.()
      }}
    >
      <motion.div
        ref={ref}
        role={role}
        aria-modal="true"
        aria-labelledby={titleId}
        aria-describedby={description ? descId : undefined}
        className="cel dialog"
        style={{ transformOrigin }}
        initial={reduce ? { opacity: 0 } : { opacity: 0, scale: 0.97, y: 12 }}
        animate={{ opacity: 1, scale: 1, y: 0 }}
        exit={reduce ? { opacity: 0 } : { opacity: 0, scale: 0.97, y: 8 }}
        transition={reduce ? { duration: 0.12 } : { type: 'spring', stiffness: 520, damping: 32 }}
        onKeyDown={onKeyDown}
      >
        <h2 id={titleId}>{title}</h2>
        {description && (
          <p id={descId} className="muted">
            {description}
          </p>
        )}
        {children}
      </motion.div>
    </motion.div>,
    document.body,
  )
}

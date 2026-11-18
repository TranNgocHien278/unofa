import { AnimatePresence, motion, useReducedMotion } from 'framer-motion'
import { AlertTriangle, Check } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useUi } from '../store/ui'

export function Toasts() {
  const toasts = useUi((s) => s.toasts)
  const dismiss = useUi((s) => s.dismiss)
  const reduce = useReducedMotion()
  const { t } = useTranslation()
  return (
    <div className="toasts" role="status" aria-live="polite">
      <AnimatePresence initial={false}>
        {toasts.map((x) => (
          <motion.div
            key={x.id}
            layout={!reduce}
            className={`toast ${x.tone === 'alert' ? 'toast--alert' : ''}`}
            initial={reduce ? { opacity: 0 } : { opacity: 0, y: -16, scale: 0.97 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            exit={{ opacity: 0, scale: 0.97 }}
            transition={reduce ? { duration: 0.12 } : { type: 'spring', stiffness: 600, damping: 30 }}
            onClick={() => dismiss(x.id)}
          >
            {x.tone === 'alert' ? <AlertTriangle size={20} strokeWidth={2.5} aria-hidden /> : <Check size={20} strokeWidth={2.5} aria-hidden />}
            <span>
              {x.text}
              {x.requestId && <small>{t('common.requestId', { id: x.requestId })}</small>}
            </span>
          </motion.div>
        ))}
      </AnimatePresence>
    </div>
  )
}

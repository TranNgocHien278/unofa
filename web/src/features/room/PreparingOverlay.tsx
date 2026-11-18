import { motion } from 'framer-motion'
import { useTranslation } from 'react-i18next'
import { CardBack } from '../../components/Card'

// Shown while the 5s asset handshake runs after the host presses Play.
export function PreparingOverlay() {
  const { t } = useTranslation()
  return (
    <motion.div className="scrim" style={{ placeItems: 'center' }} initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }}>
      <div className="cel stack" role="status" style={{ justifyItems: 'center', textAlign: 'center', width: 'min(320px, 100%)' }}>
        <motion.div
          className="deal-card"
          animate={{ rotate: [0, -8, 8, 0], y: [0, -10, 0] }}
          transition={{ duration: 0.9, repeat: Infinity, ease: 'easeInOut' }}
          aria-hidden
        >
          <CardBack />
        </motion.div>
        <p style={{ fontWeight: 700 }}>{t('room.preparing')}</p>
      </div>
    </motion.div>
  )
}

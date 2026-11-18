import { useEffect } from 'react'
import { Outlet, useNavigate } from 'react-router-dom'
import { AnimatePresence } from 'framer-motion'
import { useSessionBoot } from './providers'
import { setNavigator } from '../realtime/useMatchStream'
import { Toasts } from '../components/Toast'
import { ReloginDialog } from '../features/auth/ReloginDialog'
import { PreparingOverlay } from '../features/room/PreparingOverlay'
import { useUi } from '../store/ui'
import { useRoom } from '../store/room'

export function Root() {
  useSessionBoot()
  const navigate = useNavigate()
  useEffect(() => setNavigator((to) => navigate(to)), [navigate])
  const relogin = useUi((s) => s.reloginOpen)
  const preparing = useRoom((s) => s.preparing)
  return (
    <>
      <Outlet />
      <Toasts />
      <AnimatePresence>{relogin && <ReloginDialog key="relogin" />}</AnimatePresence>
      <AnimatePresence>{preparing && <PreparingOverlay key="prep" />}</AnimatePresence>
    </>
  )
}

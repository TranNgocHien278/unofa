import { useEffect } from 'react'
import { heartbeat, me } from '../api/auth'
import { isApiError } from '../api/client'
import { i18n } from '../i18n'
import { useSession } from '../store/session'
import { useUi } from '../store/ui'
import { useMatchStream } from '../realtime/useMatchStream'

const BEAT_MS = 60_000
// A tab hidden this long stops extending the session (05 §Heartbeat).
const HIDDEN_LIMIT_MS = 10 * 60_000

export function useSessionBoot() {
  const status = useSession((s) => s.status)
  const locale = useSession((s) => s.user?.locale)
  const reloginOpen = useUi((s) => s.reloginOpen)

  useEffect(() => {
    me()
      .then((m) => useSession.getState().setMe(m))
      .catch(() => useSession.getState().setAnonymous())
  }, [])

  useEffect(() => {
    if (locale && i18n.language !== locale) void i18n.changeLanguage(locale)
  }, [locale])

  useEffect(() => {
    if (status !== 'authenticated' || reloginOpen) return
    let hiddenAt = 0
    const beat = () => {
      if (document.visibilityState !== 'visible') return
      heartbeat().catch((e) => {
        if (isApiError(e, 'SESSION_EXPIRED') || isApiError(e, 'UNAUTHENTICATED')) useUi.getState().openRelogin()
      })
    }
    const onVis = () => {
      if (document.visibilityState === 'hidden') hiddenAt = Date.now()
      else if (hiddenAt && Date.now() - hiddenAt > 5000) beat()
    }
    const id = setInterval(() => {
      if (hiddenAt && document.visibilityState === 'hidden' && Date.now() - hiddenAt > HIDDEN_LIMIT_MS) return
      beat()
    }, BEAT_MS)
    document.addEventListener('visibilitychange', onVis)
    return () => {
      clearInterval(id)
      document.removeEventListener('visibilitychange', onVis)
    }
  }, [status, reloginOpen])

  useMatchStream()
}

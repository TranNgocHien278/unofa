import { setApiHooks } from './client'
import { useUi } from '../store/ui'
import { useSession } from '../store/session'
import { errorText, i18n } from '../i18n'

setApiHooks({
  network: () => useUi.getState().toast(i18n.t('errors.NETWORK'), 'alert'),
  failed(err) {
    const authed = useSession.getState().status === 'authenticated'
    if ((err.code === 'SESSION_EXPIRED' || err.code === 'UNAUTHENTICATED') && authed) {
      // Never log out silently: the player may be inside the 90s reconnect grace.
      useUi.getState().openRelogin()
    } else if (err.status === 429) {
      useUi.getState().toast(i18n.t('errors.RATE_LIMITED_IN', { count: err.retryAfter ?? 1 }), 'alert')
    } else if (err.status === 503) {
      useUi.getState().toast(errorText('SERVICE_UNAVAILABLE'), 'alert', err.requestId)
    }
  },
})

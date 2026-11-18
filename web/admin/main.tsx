import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { MotionConfig } from 'framer-motion'
import '@fontsource/coiny/400.css'
import '@fontsource/be-vietnam-pro/400.css'
import '@fontsource/be-vietnam-pro/700.css'
import '../src/styles/tokens.css'
import '../src/styles/app.css'
import '../src/styles/pages.css'
import './admin.css'
import { errorText, i18n } from '../src/i18n'
import { setApiHooks } from '../src/api/client'
import en from './locales/en.json'
import vi from './locales/vi.json'
import { useAdmin } from './state'
import { App } from './App'

i18n.addResourceBundle('en', 'translation', en, true, false)
i18n.addResourceBundle('vi', 'translation', vi, true, false)

setApiHooks({
  network: () => useAdmin.getState().toast(i18n.t('errors.NETWORK'), 'alert'),
  failed(err) {
    if (err.code === 'UNAUTHENTICATED' || err.code === 'SESSION_EXPIRED') useAdmin.getState().setStatus('out')
    else if (err.status === 429) useAdmin.getState().toast(i18n.t('errors.RATE_LIMITED_IN', { count: err.retryAfter ?? 1 }), 'alert')
    else if (err.status >= 500) useAdmin.getState().toast(errorText(err.code), 'alert', err.requestId)
  },
})

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <MotionConfig reducedMotion="user">
      <App />
    </MotionConfig>
  </StrictMode>,
)

import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import en from './locales/en.json'
import vi from './locales/vi.json'
import type { Locale } from '../api/types'

// Signed-out visitors get vi only when the browser says so; everyone else en (06 §i18n).
export const detectLocale = (): Locale =>
  typeof navigator !== 'undefined' && navigator.language?.toLowerCase().startsWith('vi') ? 'vi' : 'en'

void i18n.use(initReactI18next).init({
  resources: { en: { translation: en }, vi: { translation: vi } },
  lng: detectLocale(),
  fallbackLng: 'en',
  interpolation: { escapeValue: false },
})

const syncLang = (lng: string) => {
  if (typeof document !== 'undefined') document.documentElement.lang = lng
}
syncLang(i18n.language)
i18n.on('languageChanged', syncLang)

// errorText translates a server error code; an unknown code shows raw and is
// reported, never rendered as an empty string (06 §i18n).
export function errorText(code: string): string {
  const key = `errors.${code}`
  if (i18n.exists(key)) return i18n.t(key)
  console.warn('untranslated error code', code)
  return code
}

export function formatU(n: number, locale: string = i18n.language): string {
  return `${new Intl.NumberFormat(locale === 'vi' ? 'vi-VN' : 'en-US').format(n)}U`
}

export { i18n }

import { formatU } from '../i18n'
import { useTranslation } from 'react-i18next'

// Money renders a U amount with the locale's grouping (1,000U / 1.000U) and tabular digits.
export function Money({ value, signed, className = '' }: { value: number; signed?: boolean; className?: string }) {
  useTranslation()
  const sign = signed && value > 0 ? '+' : signed && value < 0 ? '−' : ''
  return <span className={`num ${className}`}>{sign + formatU(signed ? Math.abs(value) : value)}</span>
}

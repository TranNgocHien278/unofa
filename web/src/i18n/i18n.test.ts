import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import en from './locales/en.json'
import vi from './locales/vi.json'
import { errorText, formatU, i18n } from './index'

type Tree = { [k: string]: string | Tree }
const flat = (t: Tree, p = ''): string[] => Object.entries(t).flatMap(([k, v]) => (typeof v === 'string' ? [p + k] : flat(v, `${p}${k}.`)))

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((f) => {
    const p = join(dir, f)
    if (statSync(p).isDirectory()) return sources(p)
    return /\.tsx?$/.test(f) && !f.endsWith('.test.ts') ? [p] : []
  })
}

const enKeys = new Set(flat(en as Tree))
const has = (k: string) => enKeys.has(k) || enKeys.has(`${k}_other`)

describe('i18n (F7)', () => {
  it('every literal key used in code exists in en', () => {
    const used = new Set<string>()
    const re = /'((?:nav|footer|common|auth|lobby|room|table|announce|card|account|reason|chart|settings|loan|rules|keys|errors)\.[A-Za-z0-9_.]+)'/g
    for (const f of sources(join(__dirname, '..'))) for (const m of readFileSync(f, 'utf8').matchAll(re)) used.add(m[1])
    const missing = [...used].filter((k) => !has(k) && !has(`rules.${k}`))
    expect(missing).toEqual([])
  })

  it('vi covers every en key', () => {
    const viKeys = new Set(flat(vi as Tree))
    expect([...enKeys].filter((k) => !viKeys.has(k))).toEqual([])
  })

  it('formats U per locale', () => {
    expect(formatU(1000, 'vi')).toBe('1.000U')
    expect(formatU(1000, 'en')).toBe('1,000U')
  })

  it('shows a Vietnamese error, and the raw code when untranslated', async () => {
    await i18n.changeLanguage('vi')
    expect(errorText('INSUFFICIENT_BALANCE')).toBe('Không đủ U')
    expect(errorText('SOME_NEW_CODE')).toBe('SOME_NEW_CODE')
    expect(i18n.t('table.cards', { count: 3 })).toBe('3 lá')
    i18n.addResource('en', 'translation', 'test.onlyEn', 'English only')
    expect(i18n.t('test.onlyEn')).toBe('English only')
    await i18n.changeLanguage('en')
    expect(i18n.t('table.cards', { count: 1 })).toBe('1 card')
  })
})

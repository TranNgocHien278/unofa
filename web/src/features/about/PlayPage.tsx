import { useTranslation } from 'react-i18next'
import type { Card } from '../../api/types'
import { CardFace } from '../../components/Card'

const ACTIONS: { card: Card; key: string }[] = [
  { card: { color: 'BLUE', rank: 'SKIP' }, key: 'skip' },
  { card: { color: 'GREEN', rank: 'REVERSE' }, key: 'reverse' },
  { card: { color: 'RED', rank: 'DRAW2' }, key: 'draw2' },
  { card: { color: 'NONE', rank: 'WILD' }, key: 'wild' },
  { card: { color: 'NONE', rank: 'WILD4' }, key: 'wild4' },
]

const KEYS = [
  ['←  →', 'keys.move'],
  ['Enter', 'keys.play'],
  ['D', 'keys.draw'],
  ['P', 'keys.pass'],
  ['U', 'keys.uno'],
  ['C', 'keys.catch'],
  ['1–4', 'keys.color'],
] as const

export function PlayPage() {
  const { t } = useTranslation()
  return (
    <div className="page rules">
      <div className="page-head on-sun">
        <h1>{t('rules.title')}</h1>
        <p className="muted--sun">{t('rules.lede')}</p>
      </div>

      <section className="cel stack">
        <h2>{t('rules.matchTitle')}</h2>
        <div className="rules__match" aria-hidden>
          <span className="mini-card">
            <CardFace card={{ color: 'RED', rank: '7' }} />
          </span>
          <span className="mini-card">
            <CardFace card={{ color: 'RED', rank: '2' }} />
          </span>
          <span className="mini-card">
            <CardFace card={{ color: 'BLUE', rank: '7' }} />
          </span>
        </div>
        <p>{t('rules.match')}</p>
        <p>{t('rules.draw')}</p>
      </section>

      <section className="cel stack">
        <h2>{t('rules.actionsTitle')}</h2>
        <ul className="rules__actions">
          {ACTIONS.map((a) => (
            <li key={a.key}>
              <span className="mini-card" aria-hidden>
                <CardFace card={a.card} />
              </span>
              <span>
                <strong>{t(`rules.${a.key}.name`)}</strong>
                <span className="muted">{t(`rules.${a.key}.body`)}</span>
              </span>
            </li>
          ))}
        </ul>
      </section>

      <section className="cel stack">
        <h2>{t('rules.unoTitle')}</h2>
        <p>{t('rules.uno')}</p>
        <p>{t('rules.timer')}</p>
      </section>

      <section className="cel stack">
        <h2>{t('rules.moneyTitle')}</h2>
        <p>{t('rules.money')}</p>
        <p className="bet-note">{t('room.betNote')}</p>
      </section>

      <section className="cel stack">
        <h2>{t('rules.keysTitle')}</h2>
        <table className="keys">
          <tbody>
            {KEYS.map(([k, label]) => (
              <tr key={k}>
                <th scope="row">
                  <kbd className="kbd">{k}</kbd>
                </th>
                <td>{t(`rules.${label}`)}</td>
              </tr>
            ))}
          </tbody>
        </table>
        <p className="muted">{t('rules.keysNote')}</p>
      </section>
    </div>
  )
}

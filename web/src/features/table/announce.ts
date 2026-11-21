import { useTranslation } from 'react-i18next'
import type { Announce } from '../../store/match'
import { cardLabel } from '../../components/Card'

export interface Bubble {
  id: number
  text: string
  loud: boolean
}

// useAnnouncement turns the last action event into one short line for the screen
// reader's live region.
// Plain number plays return null: the discard pile already announces its top card.
export function useAnnouncement(a: Announce | null, nameOf: (id?: number | null) => string, meId: number): Bubble | null {
  const { t } = useTranslation()
  if (!a) return null
  const name = nameOf(a.playerId)
  const target = nameOf(a.targetId)
  const vars = { name, target }
  const say = (key: string, loud = false, extra: Record<string, string> = {}) => ({ id: a.id, text: t(key, { ...vars, ...extra }), loud })
  switch (a.kind) {
    case 'play':
      if (!a.card) return null
      if (a.card.rank === 'WILD' || a.card.rank === 'WILD4')
        return say(a.card.rank === 'WILD4' ? 'announce.wild4' : 'announce.wild', a.card.rank === 'WILD4', { color: t(`card.color.${a.color ?? 'NONE'}`) })
      if (a.card.rank === 'DRAW2') return say('announce.draw2', true)
      if (a.card.rank === 'SKIP') return say('announce.skip')
      if (a.card.rank === 'REVERSE') return say('announce.reverse')
      return a.playerId === meId ? null : { id: a.id, text: t('announce.played', { name, card: cardLabel(a.card) }), loud: false }
    case 'draw':
      return a.playerId === meId ? null : say('announce.draw')
    case 'pass':
      return say('announce.pass')
    case 'call_uno':
      return say('announce.uno', true)
    case 'catch_uno':
      return say(a.result === 'success' ? 'announce.caught' : 'announce.catchMiss', a.result === 'success')
    case 'challenge':
      return say(a.result === 'success' ? 'announce.challengeRight' : 'announce.challengeWrong', true)
    case 'accept':
      return say('announce.accept')
    case 'timeout':
      return say('announce.timeout')
    case 'draw2_penalty':
      return say('announce.penalty')
    case 'status':
      if (a.status === 'disconnected') return say('announce.offline')
      if (a.status === 'forfeit') return say('announce.forfeit')
      return say('announce.back')
    default:
      return null
  }
}

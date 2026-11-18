import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

// The OS asks for reduced motion before framer-motion reads the preference.
window.matchMedia = ((q: string) => ({
  matches: q.includes('prefers-reduced-motion'),
  media: q,
  addEventListener() {},
  removeEventListener() {},
  addListener() {},
  removeListener() {},
})) as unknown as typeof window.matchMedia

await import('../i18n')
const { Hand } = await import('./Hand')
const { Discard } = await import('./Discard')
const { sortHand } = await import('../features/table/rules')

describe('F9 prefers-reduced-motion', () => {
  it('deals a new hand card without the fly-in transform', () => {
    const a = { color: 'RED', rank: '5' } as const
    const b = { color: 'BLUE', rank: '1' } as const
    const props = { isPlayable: () => true, pendingKey: null, onPlay() {} }
    const { rerender } = render(<Hand cards={sortHand([a])} {...props} />)
    rerender(<Hand cards={sortHand([a, b])} {...props} />)
    const dealt = screen.getAllByRole('option')[1]
    expect(dealt.style.transform ?? '').not.toMatch(/translateY\(-140px\)/)
  })

  it('lays an opponent card on the pile without flying it from the seat', () => {
    const top = { color: 'RED', rank: '7' } as const
    const { rerender, container } = render(<Discard top={top} activeColor="RED" seq={1} playId={0} />)
    rerender(<Discard top={{ color: 'RED', rank: '2' }} activeColor="RED" seq={2} playId={1} from={{ x: 0, y: -300 }} />)
    const cards = container.querySelectorAll<HTMLElement>('.discard__card')
    expect(cards[cards.length - 1].style.transform ?? '').not.toMatch(/translateY\(-300px\)/)
  })
})

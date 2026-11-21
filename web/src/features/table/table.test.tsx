import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { view } from '../../test/fixtures'
import type { MatchView } from '../../api/types'

const sendAction = vi.fn()
vi.mock('../../realtime/useMatchStream', () => ({ sendAction: (...a: unknown[]) => sendAction(...a) }))
vi.mock('../../api/matches', () => ({ matchState: vi.fn(() => new Promise(() => {})) }))
vi.mock('../../api/rooms', () => ({ getRoom: vi.fn(() => new Promise(() => {})), leaveRoom: vi.fn() }))
vi.mock('../../lib/sound', () => ({ sound: vi.fn() }))

await import('../../i18n')
const { useMatch } = await import('../../store/match')
const { useSession } = await import('../../store/session')
const { default: TablePage } = await import('./TablePage')

function mount(v: MatchView) {
  useMatch.getState().reset()
  useMatch.getState().setMatch('m1', 1)
  useMatch.getState().applySnapshot(v)
  return render(
    <MemoryRouter initialEntries={['/table/m1']}>
      <Routes>
        <Route path="/table/:matchId" element={<TablePage />} />
      </Routes>
    </MemoryRouter>,
  )
}

beforeEach(() => {
  sendAction.mockReset()
  useSession.setState({ user: { userId: 1, username: 'me', balance: 500, locale: 'en' }, status: 'authenticated' })
})
afterEach(() => {
  cleanup()
  vi.useRealTimers()
})

describe('F3 UNO button', () => {
  it('is not rendered and U does nothing when can_call_uno is false', () => {
    mount(view())
    expect(screen.queryByRole('button', { name: /UNO!/ })).toBeNull()
    fireEvent.keyDown(screen.getByRole('region', { name: 'Card table' }), { key: 'u' })
    expect(sendAction).not.toHaveBeenCalled()
  })

  it('renders and U calls it when allowed with one card left', () => {
    mount(view({ hand: [{ color: 'RED', rank: '5' }], you: { can_call_uno: true, is_your_turn: false } }))
    expect(screen.getByRole('button', { name: /UNO!/ })).toBeTruthy()
    fireEvent.keyDown(screen.getByRole('region', { name: 'Card table' }), { key: 'u' })
    expect(sendAction).toHaveBeenCalledWith('call_uno')
  })

  it('stays hidden with two cards even if the flag says otherwise', () => {
    mount(view({ you: { can_call_uno: true } }))
    expect(screen.queryByRole('button', { name: /UNO!/ })).toBeNull()
    fireEvent.keyDown(screen.getByRole('region', { name: 'Card table' }), { key: 'u' })
    expect(sendAction).not.toHaveBeenCalled()
  })
})

describe('round table', () => {
  const players = (n: number) =>
    Array.from({ length: n }, (_, i) => ({ user_id: i + 1, seat: i, hand_count: 5, status: 'connected' as const, uno_called: false }))
  const seatsOf = (c: HTMLElement) => [...c.querySelectorAll<HTMLElement>('.seat')]
  const angle = (s: HTMLElement) => Math.atan2(Number(s.style.getPropertyValue('--y')), Number(s.style.getPropertyValue('--x')))

  it.each([2, 3, 4, 6])('seats %i players 360/N apart with me at the bottom', (n) => {
    const { container } = mount(view({ pub: { players: players(n) } }))
    const seats = seatsOf(container)
    expect(seats).toHaveLength(n)
    expect(angle(seats[0])).toBeCloseTo(Math.PI / 2, 3)
    for (let i = 1; i < n; i++) {
      const step = (angle(seats[i]) - angle(seats[i - 1]) + 2 * Math.PI) % (2 * Math.PI)
      expect(step).toBeCloseTo((2 * Math.PI) / n, 3)
    }
    expect(seats[0].querySelector('.seat__name')?.textContent).toBe('You')
    expect(seats[1].querySelector('.seat__count')?.textContent).toBe('5')
  })

  it('glows exactly one pile: the current player, or the +4 challenger', () => {
    const { container, unmount } = mount(view({ you: { is_your_turn: false }, pub: { players: players(4), current_player: 3 } }))
    expect(container.querySelectorAll('.seat--turn')).toHaveLength(1)
    expect(seatsOf(container)[2].classList).toContain('seat--turn')
    unmount()

    const c2 = mount(view({ you: { is_your_turn: false }, pub: { players: players(4), current_player: 3, phase: 'awaiting_challenge', pending_challenger: 4 } })).container
    expect(c2.querySelectorAll('.seat--turn')).toHaveLength(1)
    expect(seatsOf(c2)[3].classList).toContain('seat--turn')
  })
})

describe('draw flights', () => {
  it('locks my actions while cards are in the air and unlocks after the last lands', () => {
    vi.useFakeTimers()
    mount(view())
    const pile = screen.getByRole('button', { name: /Draw a card/ })
    expect(pile.getAttribute('aria-disabled')).toBe('false')
    act(() => {
      useMatch.getState().applyEvent({ name: 'action', seq: 4, server_now: new Date().toISOString(), data: { action: 'draw2_penalty', player_id: 2, target_player_id: 2, result: 'success' } })
    })
    expect(document.querySelectorAll('.flight')).toHaveLength(2)
    expect(pile.getAttribute('aria-disabled')).toBe('true')
    expect(screen.getAllByRole('option').every((c) => c.getAttribute('aria-disabled') === 'true')).toBe(true)
    fireEvent.keyDown(screen.getByRole('region', { name: 'Card table' }), { key: 'd' })
    expect(sendAction).not.toHaveBeenCalled()

    act(() => vi.advanceTimersByTime(550))
    expect(document.querySelectorAll('.flight')).toHaveLength(0)
    expect(pile.getAttribute('aria-disabled')).toBe('false')
    fireEvent.keyDown(screen.getByRole('region', { name: 'Card table' }), { key: 'd' })
    expect(sendAction).toHaveBeenCalledWith('draw')
  })
})

describe('F4 challenge dialog', () => {
  it('focuses Accept, ignores Esc, and keeps Tab inside', async () => {
    mount(view({ you: { is_your_turn: false }, pub: { phase: 'awaiting_challenge', pending_challenger: 1, current_player: 1 } }))
    const dialog = await screen.findByRole('alertdialog')
    const accept = screen.getByRole('button', { name: 'Accept, draw 4' })
    expect(document.activeElement).toBe(accept)

    fireEvent.keyDown(dialog, { key: 'Escape' })
    expect(screen.getByRole('alertdialog')).toBeTruthy()

    const challenge = screen.getByRole('button', { name: 'Challenge' })
    act(() => challenge.focus())
    fireEvent.keyDown(dialog, { key: 'Tab' })
    expect(dialog.contains(document.activeElement)).toBe(true)
    fireEvent.keyDown(dialog, { key: 'Tab', shiftKey: true })
    expect(dialog.contains(document.activeElement)).toBe(true)
  })
})

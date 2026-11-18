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
afterEach(cleanup)

describe('F3 UNO button', () => {
  it('is not rendered and U does nothing when can_call_uno is false', () => {
    mount(view())
    expect(screen.queryByRole('button', { name: /UNO!/ })).toBeNull()
    fireEvent.keyDown(screen.getByRole('region', { name: 'Card table' }), { key: 'u' })
    expect(sendAction).not.toHaveBeenCalled()
  })

  it('renders and U calls it when allowed', () => {
    mount(view({ you: { can_call_uno: true } }))
    expect(screen.getByRole('button', { name: /UNO!/ })).toBeTruthy()
    fireEvent.keyDown(screen.getByRole('region', { name: 'Card table' }), { key: 'u' })
    expect(sendAction).toHaveBeenCalledWith('call_uno')
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

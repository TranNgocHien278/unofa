import { lazy, Suspense, type ReactNode } from 'react'
import { createBrowserRouter, Navigate, Outlet, useLocation } from 'react-router-dom'
import { useSession } from '../store/session'
import { Shell } from './Shell'
import { LoginPage } from '../features/auth/LoginPage'
import { RegisterPage } from '../features/auth/RegisterPage'
import { LobbyPage } from '../features/lobby/LobbyPage'
import { RoomPage } from '../features/room/RoomPage'
import { AboutPage } from '../features/about/AboutPage'
import { PlayPage } from '../features/about/PlayPage'
import { Root } from './Root'

// The table and the chart pages are the heavy routes; they load on demand (06 §Asset).
const TablePage = lazy(() => import('../features/table/TablePage'))
const AccountPage = lazy(() => import('../features/account/AccountPage'))
const LoanPage = lazy(() => import('../features/loan/LoanPage'))

function Spinner() {
  return (
    <div className="empty" role="status">
      <span className="spinner" style={{ display: 'inline-block' }} />
    </div>
  )
}

function RequireAuth({ children }: { children: ReactNode }) {
  const status = useSession((s) => s.status)
  const loc = useLocation()
  if (status === 'loading') return <Spinner />
  if (status === 'anonymous') return <Navigate to="/login" replace state={{ from: loc.pathname }} />
  return <>{children}</>
}

function GuestOnly({ children }: { children: ReactNode }) {
  const status = useSession((s) => s.status)
  if (status === 'loading') return <Spinner />
  if (status === 'authenticated') return <Navigate to="/lobby" replace />
  return <>{children}</>
}

const lazyPage = (node: ReactNode) => <Suspense fallback={<Spinner />}>{node}</Suspense>

export const router = createBrowserRouter([
  {
    element: <Root />,
    children: [
      {
        path: '/table/:matchId',
        element: <RequireAuth>{lazyPage(<TablePage />)}</RequireAuth>,
      },
      {
        element: <Shell><Outlet /></Shell>,
        children: [
          { path: '/', element: <GuestOnly><LoginPage /></GuestOnly> },
          { path: '/login', element: <GuestOnly><LoginPage /></GuestOnly> },
          { path: '/register', element: <GuestOnly><RegisterPage /></GuestOnly> },
          { path: '/lobby', element: <RequireAuth><LobbyPage /></RequireAuth> },
          { path: '/room/:code', element: <RequireAuth><RoomPage /></RequireAuth> },
          { path: '/account', element: <RequireAuth>{lazyPage(<AccountPage />)}</RequireAuth> },
          { path: '/loan', element: <RequireAuth>{lazyPage(<LoanPage />)}</RequireAuth> },
          { path: '/play', element: <PlayPage /> },
          { path: '/about', element: <AboutPage /> },
          { path: '*', element: <Navigate to="/" replace /> },
        ],
      },
    ],
  },
])

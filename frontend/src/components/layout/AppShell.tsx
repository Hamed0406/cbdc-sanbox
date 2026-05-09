import { Navigate, Outlet } from 'react-router-dom'
import { useAuthStore } from '@/store/authStore'
import { useWebSocket } from '@/hooks/useWebSocket'
import { ToastContainer } from '@/components/ui/Toast'
import { Sidebar } from './Sidebar'

// AppShell wraps all authenticated routes.
// It checks auth, starts the WebSocket connection, and renders the layout.
export function AppShell() {
  const user = useAuthStore((s) => s.user)

  // WebSocket is enabled for all authenticated users.
  // It starts here (not in individual pages) so notifications arrive regardless
  // of which page the user is on.
  useWebSocket({ enabled: !!user })

  if (!user) {
    return <Navigate to="/login" replace />
  }

  return (
    <div className="flex h-screen bg-slate-950 overflow-hidden">
      <Sidebar />
      <main className="flex-1 overflow-y-auto">
        <Outlet />
      </main>
      <ToastContainer />
    </div>
  )
}

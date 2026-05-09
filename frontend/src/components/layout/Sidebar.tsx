import { NavLink } from 'react-router-dom'
import { useAuthStore } from '@/store/authStore'
import { useLogout } from '@/hooks/useAuth'
import { cn } from '@/lib/cn'

const userLinks = [
  { to: '/dashboard', label: 'Dashboard', icon: '⬡' },
  { to: '/send', label: 'Send', icon: '↑' },
  { to: '/history', label: 'History', icon: '⏱' },
]

const adminLinks = [
  { to: '/admin/issue', label: 'Issue CBDC', icon: '✦' },
]

export function Sidebar() {
  const user = useAuthStore((s) => s.user)
  const logout = useLogout()

  const links = [
    ...userLinks,
    ...(user?.role === 'admin' ? adminLinks : []),
  ]

  return (
    <aside className="w-56 flex-shrink-0 bg-slate-900 border-r border-slate-800 flex flex-col">
      {/* Logo */}
      <div className="px-5 py-5 border-b border-slate-800">
        <span className="text-gold-400 font-bold text-xl">DD$</span>
        <span className="text-slate-400 text-sm ml-1">Wallet</span>
      </div>

      {/* Nav */}
      <nav className="flex-1 px-3 py-4 space-y-1">
        {links.map((link) => (
          <NavLink
            key={link.to}
            to={link.to}
            className={({ isActive }) =>
              cn(
                'flex items-center gap-3 px-3 py-2 rounded-lg text-sm font-medium transition-colors',
                isActive
                  ? 'bg-emerald-600/20 text-emerald-400'
                  : 'text-slate-400 hover:text-slate-100 hover:bg-slate-800',
              )
            }
          >
            <span className="text-base">{link.icon}</span>
            {link.label}
          </NavLink>
        ))}
      </nav>

      {/* User footer */}
      <div className="px-3 py-4 border-t border-slate-800">
        <div className="px-3 py-2 mb-1">
          <p className="text-xs text-slate-500 truncate">{user?.email}</p>
          <p className="text-xs text-slate-400 capitalize">{user?.role}</p>
        </div>
        <button
          onClick={() => logout.mutate()}
          className="w-full text-left px-3 py-2 text-sm text-slate-400 hover:text-red-400 hover:bg-slate-800 rounded-lg transition-colors"
        >
          Sign out
        </button>
      </div>
    </aside>
  )
}

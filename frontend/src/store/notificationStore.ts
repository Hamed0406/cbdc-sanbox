import { create } from 'zustand'
import type { WsEvent } from '@/types/api'

export interface Notification {
  id: string
  event: WsEvent
  // Whether the user has dismissed this notification from the toast.
  dismissed: boolean
}

interface NotificationState {
  notifications: Notification[]
  // Latest balance from a WebSocket event — used to update the balance display
  // without a REST round-trip after each received payment.
  liveBalance: number | null

  addNotification: (event: WsEvent) => void
  dismiss: (id: string) => void
  clearAll: () => void
  setLiveBalance: (cents: number) => void
}

export const useNotificationStore = create<NotificationState>((set) => ({
  notifications: [],
  liveBalance: null,

  addNotification: (event) =>
    set((state) => ({
      notifications: [
        { id: crypto.randomUUID(), event, dismissed: false },
        ...state.notifications,
      ].slice(0, 50), // cap at 50 — prevents unbounded growth in long sessions
    })),

  dismiss: (id) =>
    set((state) => ({
      notifications: state.notifications.map((n) =>
        n.id === id ? { ...n, dismissed: true } : n,
      ),
    })),

  clearAll: () => set({ notifications: [] }),

  setLiveBalance: (cents) => set({ liveBalance: cents }),
}))

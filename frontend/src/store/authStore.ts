import { create } from 'zustand'
import type { User, WalletSummary } from '@/types/api'

interface AuthState {
  user: User | null
  wallet: WalletSummary | null
  // Access token lives in memory only — NEVER written to localStorage.
  // An XSS attack cannot exfiltrate it because there is no persistent storage.
  // The refresh token is in an HttpOnly cookie; JS cannot read it at all.
  accessToken: string | null

  setAuth: (user: User, wallet: WalletSummary, token: string) => void
  setAccessToken: (token: string) => void
  clearAuth: () => void
}

export const useAuthStore = create<AuthState>((set) => ({
  user: null,
  wallet: null,
  accessToken: null,

  setAuth: (user, wallet, token) => set({ user, wallet, accessToken: token }),
  setAccessToken: (token) => set({ accessToken: token }),
  clearAuth: () => set({ user: null, wallet: null, accessToken: null }),
}))

import client from './client'
import type { BalanceResponse, TransactionListResponse } from '@/types/api'

export interface WalletSearchResult {
  wallet_id: string
  owner_name: string
  owner_email: string
  balance_cents: number
  balance_display: string
  is_frozen: boolean
}

export const walletApi = {
  getBalance: (walletId: string) =>
    client.get<BalanceResponse>(`/wallets/${walletId}/balance`).then((r) => r.data),

  getTransactions: (walletId: string, params?: { page?: number; limit?: number }) =>
    client
      .get<TransactionListResponse>(`/wallets/${walletId}/transactions`, { params })
      .then((r) => r.data),

  search: (q: string) =>
    client
      .get<{ wallets: WalletSearchResult[] }>('/wallets/search', { params: { q } })
      .then((r) => r.data.wallets),
}

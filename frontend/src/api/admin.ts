import client from './client'

export interface IssueCBDCRequest {
  wallet_id: string
  amount_cents: number
  reason: string
}

export interface IssuanceDetail {
  id: string
  wallet_id: string
  transaction_id: string
  amount_cents: number
  amount_display: string
  reason: string
  created_at: string
}

export interface IssueCBDCResponse {
  issuance: IssuanceDetail
  new_balance_cents: number
  new_balance_display: string
}

export interface WalletLookupResponse {
  id: string
  user_id: string
  currency: string
  balance_cents: number
  balance_display: string
  is_frozen: boolean
}

export const adminApi = {
  issueCBDC: (data: IssueCBDCRequest) =>
    client
      .post<IssueCBDCResponse>('/admin/issue-cbdc', data, {
        headers: { 'X-Idempotency-Key': crypto.randomUUID() },
      })
      .then((r) => r.data),

  getWallet: (walletId: string) =>
    client.get<WalletLookupResponse>(`/wallets/${walletId}`).then((r) => r.data),
}

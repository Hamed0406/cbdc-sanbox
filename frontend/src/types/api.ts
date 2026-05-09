// API response types — mirrors backend Go structs exactly.
// Keeping them in one file makes it easy to catch divergence when the backend changes.

export interface ApiError {
  error: {
    code: string
    message: string
    request_id: string
    timestamp: string
  }
}

// ── Auth ──────────────────────────────────────────────────────────────────────

export interface User {
  id: string
  email: string
  full_name: string
  role: 'user' | 'merchant' | 'admin'
  created_at?: string
}

export interface WalletSummary {
  id: string
  currency: string
  balance_cents: number
  balance_display: string
}

export interface Tokens {
  access_token: string
  expires_in: number
}

export interface AuthResponse {
  user: User
  wallet: WalletSummary
  tokens: Tokens
}

export interface RegisterRequest {
  email: string
  password: string
  full_name: string
}

export interface LoginRequest {
  email: string
  password: string
}

// ── Wallet ────────────────────────────────────────────────────────────────────

export interface BalanceResponse {
  balance_cents: number
  balance_display: string
}

export interface Transaction {
  id: string
  type: 'TRANSFER' | 'ISSUANCE' | 'PAYMENT' | 'REFUND'
  status: 'PENDING' | 'CONFIRMED' | 'SETTLED' | 'FAILED' | 'REFUNDED'
  direction: 'DEBIT' | 'CREDIT'
  sender_wallet_id?: string
  receiver_wallet_id?: string
  counterparty_name?: string
  amount_cents: number
  amount_display: string
  fee_cents: number
  reference?: string
  signature: string
  created_at: string
  settled_at?: string
}

export interface Pagination {
  page: number
  limit: number
  total: number
  pages: number
}

export interface TransactionListResponse {
  transactions: Transaction[]
  pagination: Pagination
}

// ── Payments ──────────────────────────────────────────────────────────────────

export interface SendRequest {
  to_wallet_id: string
  amount_cents: number
  reference?: string
}

export interface SendResponse {
  transaction: Transaction
  new_balance_cents: number
  new_balance_display: string
}

// ── WebSocket events ──────────────────────────────────────────────────────────

export type WsEventType = 'payment.sent' | 'payment.received' | 'issuance.received'

export interface WsPaymentPayload {
  transaction_id: string
  direction: 'DEBIT' | 'CREDIT'
  amount_cents: number
  amount_display: string
  counterparty_name?: string
  reference?: string
  new_balance_cents: number
  new_balance_display: string
}

export interface WsIssuancePayload {
  transaction_id: string
  amount_cents: number
  amount_display: string
  reason: string
  new_balance_cents: number
  new_balance_display: string
}

export interface WsEvent {
  type: WsEventType
  wallet_id: string
  payload: WsPaymentPayload | WsIssuancePayload
  timestamp: string
}

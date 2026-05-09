import client from './client'
import type { SendRequest, SendResponse, TransactionListResponse } from '@/types/api'

export const paymentApi = {
  send: (data: SendRequest) =>
    client
      .post<SendResponse>('/payments/send', data, {
        // Generate a fresh idempotency key per request.
        // crypto.randomUUID() is available in all modern browsers (no extra package needed).
        headers: { 'X-Idempotency-Key': crypto.randomUUID() },
      })
      .then((r) => r.data),

  list: (params?: { page?: number; limit?: number; type?: string; status?: string }) =>
    client.get<TransactionListResponse>('/payments/', { params }).then((r) => r.data),

  getById: (id: string) =>
    client.get<import('@/types/api').Transaction>(`/payments/${id}`).then((r) => r.data),
}

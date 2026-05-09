import { http, HttpResponse } from 'msw'

const BASE = '/api/v1'

export const paymentHandlers = [
  http.post(`${BASE}/payments/send`, () =>
    HttpResponse.json(
      {
        transaction: {
          id: 'txn-new',
          type: 'TRANSFER',
          status: 'SETTLED',
          direction: 'DEBIT',
          amount_cents: 1000,
          amount_display: 'DD$ 10.00',
          fee_cents: 0,
          counterparty_name: 'Bob Smith',
          reference: 'Test',
          signature: 'sig',
          created_at: '2026-05-09T12:00:00Z',
          settled_at: '2026-05-09T12:00:01Z',
        },
        new_balance_cents: 490000,
        new_balance_display: 'DD$ 4,900.00',
      },
      { status: 201 },
    ),
  ),

  http.get(`${BASE}/payments/`, () =>
    HttpResponse.json({
      transactions: [],
      pagination: { page: 1, limit: 20, total: 0, pages: 0 },
    }),
  ),
]

import { http, HttpResponse } from 'msw'

const BASE = '/api/v1'

export const walletHandlers = [
  http.get(`${BASE}/wallets/:id/balance`, () =>
    HttpResponse.json({ balance_cents: 500000, balance_display: 'DD$ 5,000.00' }),
  ),

  http.get(`${BASE}/wallets/:id/transactions`, () =>
    HttpResponse.json({
      transactions: [
        {
          id: 'txn-1',
          type: 'TRANSFER',
          status: 'SETTLED',
          direction: 'DEBIT',
          amount_cents: 1000,
          amount_display: 'DD$ 10.00',
          fee_cents: 0,
          counterparty_name: 'Bob Smith',
          reference: 'Coffee',
          signature: 'sig',
          created_at: '2026-05-09T10:00:00Z',
          settled_at: '2026-05-09T10:00:01Z',
        },
      ],
      pagination: { page: 1, limit: 20, total: 1, pages: 1 },
    }),
  ),
]

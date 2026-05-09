import { http, HttpResponse } from 'msw'

const BASE = '/api/v1'

export const authHandlers = [
  http.post(`${BASE}/auth/login`, () =>
    HttpResponse.json({
      user: { id: 'usr-1', email: 'alice@example.com', full_name: 'Alice Johnson', role: 'user' },
      wallet: { id: 'wlt-1', currency: 'DD$', balance_cents: 500000, balance_display: 'DD$ 5,000.00' },
      tokens: { access_token: 'fake-jwt', expires_in: 900 },
    }),
  ),

  http.post(`${BASE}/auth/register`, () =>
    HttpResponse.json(
      {
        user: { id: 'usr-2', email: 'bob@example.com', full_name: 'Bob Smith', role: 'user' },
        wallet: { id: 'wlt-2', currency: 'DD$', balance_cents: 0, balance_display: 'DD$ 0.00' },
        tokens: { access_token: 'fake-jwt-new', expires_in: 900 },
      },
      { status: 201 },
    ),
  ),

  http.post(`${BASE}/auth/logout`, () => new HttpResponse(null, { status: 204 })),

  http.post(`${BASE}/auth/refresh`, () =>
    HttpResponse.json({ tokens: { access_token: 'refreshed-jwt', expires_in: 900 } }),
  ),
]

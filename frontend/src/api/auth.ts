import client from './client'
import type { AuthResponse, LoginRequest, RegisterRequest } from '@/types/api'

export const authApi = {
  login: (data: LoginRequest) =>
    client.post<AuthResponse>('/auth/login', data).then((r) => r.data),

  register: (data: RegisterRequest) =>
    client.post<AuthResponse>('/auth/register', data).then((r) => r.data),

  logout: () => client.post('/auth/logout'),

  refresh: () =>
    client.post<{ tokens: { access_token: string; expires_in: number } }>('/auth/refresh')
      .then((r) => r.data),
}

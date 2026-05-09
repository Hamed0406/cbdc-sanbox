import axios, { AxiosError } from 'axios'
import { useAuthStore } from '@/store/authStore'

const client = axios.create({
  baseURL: '/api/v1',
  withCredentials: true, // send HttpOnly refresh-token cookie on every request
  headers: { 'Content-Type': 'application/json' },
})

// Inject access token from memory into every request.
// The token is NEVER stored in localStorage — stored in Zustand (memory only).
client.interceptors.request.use((config) => {
  const token = useAuthStore.getState().accessToken
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

let isRefreshing = false
let queue: Array<(token: string) => void> = []

// On 401: silently attempt a token refresh, then retry the original request.
// If the refresh itself fails, clear auth state and redirect to /login.
client.interceptors.response.use(
  (res) => res,
  async (error: AxiosError) => {
    const original = error.config as typeof error.config & { _retry?: boolean }

    if (error.response?.status !== 401 || original._retry) {
      return Promise.reject(error)
    }

    original._retry = true

    if (isRefreshing) {
      // Another request is already refreshing — queue this one.
      return new Promise((resolve) => {
        queue.push((newToken) => {
          original.headers!.Authorization = `Bearer ${newToken}`
          resolve(client(original))
        })
      })
    }

    isRefreshing = true
    try {
      const { data } = await axios.post(
        '/api/v1/auth/refresh',
        {},
        { withCredentials: true },
      )
      const newToken: string = data.tokens.access_token
      useAuthStore.getState().setAccessToken(newToken)
      queue.forEach((cb) => cb(newToken))
      queue = []
      original.headers!.Authorization = `Bearer ${newToken}`
      return client(original)
    } catch {
      useAuthStore.getState().clearAuth()
      window.location.href = '/login'
      return Promise.reject(error)
    } finally {
      isRefreshing = false
    }
  },
)

export default client

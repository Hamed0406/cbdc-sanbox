import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useWebSocket } from './useWebSocket'
import { useNotificationStore } from '@/store/notificationStore'
import { useAuthStore } from '@/store/authStore'
import type { WsEvent } from '@/types/api'

// ── Fake WebSocket ─────────────────────────────────────────────────────────────
// jsdom doesn't include a WebSocket implementation. We provide a minimal mock
// that exposes the same interface as the browser's native WebSocket.

class FakeWebSocket {
  static instances: FakeWebSocket[] = []
  static CONNECTING = 0
  static OPEN = 1
  static CLOSING = 2
  static CLOSED = 3

  url: string
  readyState = FakeWebSocket.CONNECTING
  onopen: (() => void) | null = null
  onmessage: ((e: MessageEvent) => void) | null = null
  onclose: (() => void) | null = null
  onerror: ((e: Event) => void) | null = null

  constructor(url: string) {
    this.url = url
    FakeWebSocket.instances.push(this)
  }

  open() {
    this.readyState = FakeWebSocket.OPEN
    this.onopen?.()
  }

  receive(data: WsEvent) {
    this.onmessage?.(new MessageEvent('message', { data: JSON.stringify(data) }))
  }

  triggerClose() {
    this.readyState = FakeWebSocket.CLOSED
    this.onclose?.()
  }

  close() {
    this.triggerClose()
  }

  send(_: string) {}
}

// ── Wrapper for hooks that need QueryClient + Router ─────────────────────────
import { createWrapper } from '@/test/utils'

beforeEach(() => {
  FakeWebSocket.instances = []
  vi.stubGlobal('WebSocket', FakeWebSocket)
  // Seed auth store so the hook thinks a user is logged in
  useAuthStore.setState({
    user: { id: 'usr-1', email: 'a@b.com', full_name: 'Alice', role: 'user' },
    wallet: { id: 'wlt-1', currency: 'DD$', balance_cents: 500000, balance_display: 'DD$ 5,000.00' },
    accessToken: 'fake-jwt',
  })
  useNotificationStore.setState({ notifications: [], liveBalance: null })
})

afterEach(() => {
  vi.unstubAllGlobals()
  useAuthStore.setState({ user: null, wallet: null, accessToken: null })
})

function makePaymentEvent(): WsEvent {
  return {
    type: 'payment.received',
    wallet_id: 'wlt-1',
    payload: {
      transaction_id: 'txn-abc',
      direction: 'CREDIT',
      amount_cents: 1500,
      amount_display: 'DD$ 15.00',
      counterparty_name: 'Bob Smith',
      new_balance_cents: 501500,
      new_balance_display: 'DD$ 5,015.00',
    },
    timestamp: new Date().toISOString(),
  }
}

describe('useWebSocket', () => {
  it('connects with the access token in the URL', () => {
    renderHook(() => useWebSocket(), { wrapper: createWrapper() })

    expect(FakeWebSocket.instances).toHaveLength(1)
    expect(FakeWebSocket.instances[0].url).toContain('token=fake-jwt')
  })

  it('does not connect when disabled', () => {
    renderHook(() => useWebSocket({ enabled: false }), { wrapper: createWrapper() })
    expect(FakeWebSocket.instances).toHaveLength(0)
  })

  it('adds a notification when a message is received', () => {
    renderHook(() => useWebSocket(), { wrapper: createWrapper() })
    const ws = FakeWebSocket.instances[0]
    act(() => { ws.open() })
    act(() => { ws.receive(makePaymentEvent()) })

    expect(useNotificationStore.getState().notifications).toHaveLength(1)
    expect(useNotificationStore.getState().notifications[0].event.type).toBe('payment.received')
  })

  it('updates liveBalance from payment.received event', () => {
    renderHook(() => useWebSocket(), { wrapper: createWrapper() })
    const ws = FakeWebSocket.instances[0]
    act(() => { ws.open() })
    act(() => { ws.receive(makePaymentEvent()) })

    expect(useNotificationStore.getState().liveBalance).toBe(501500)
  })

  it('updates liveBalance from issuance.received event', () => {
    renderHook(() => useWebSocket(), { wrapper: createWrapper() })
    const ws = FakeWebSocket.instances[0]
    act(() => { ws.open() })
    act(() => {
      ws.receive({
        type: 'issuance.received',
        wallet_id: 'wlt-1',
        payload: {
          transaction_id: 'txn-iss',
          amount_cents: 100000,
          amount_display: 'DD$ 1,000.00',
          reason: 'Top up',
          new_balance_cents: 600000,
          new_balance_display: 'DD$ 6,000.00',
        },
        timestamp: new Date().toISOString(),
      })
    })

    expect(useNotificationStore.getState().liveBalance).toBe(600000)
  })

  it('silently ignores malformed JSON messages', () => {
    renderHook(() => useWebSocket(), { wrapper: createWrapper() })
    const ws = FakeWebSocket.instances[0]
    act(() => { ws.open() })
    act(() => {
      ws.onmessage?.(new MessageEvent('message', { data: 'not-json' }))
    })

    // No crash, no notification added
    expect(useNotificationStore.getState().notifications).toHaveLength(0)
  })

  it('reconnects after connection closes', () => {
    vi.useFakeTimers()
    renderHook(() => useWebSocket(), { wrapper: createWrapper() })
    const ws = FakeWebSocket.instances[0]
    act(() => { ws.open() })
    act(() => { ws.triggerClose() })

    // After backoff delay (1000ms), a new connection should be attempted
    act(() => { vi.advanceTimersByTime(1500) })
    expect(FakeWebSocket.instances).toHaveLength(2)
    vi.useRealTimers()
  })

  it('cleans up the connection on unmount', () => {
    const { unmount } = renderHook(() => useWebSocket(), { wrapper: createWrapper() })
    const ws = FakeWebSocket.instances[0]
    act(() => { ws.open() })
    unmount()

    // After unmount the ws is closed and no reconnect fires
    expect(ws.readyState).toBe(FakeWebSocket.CLOSED)
  })
})

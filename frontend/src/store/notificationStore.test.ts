import { describe, it, expect, beforeEach } from 'vitest'
import { useNotificationStore } from './notificationStore'
import type { WsEvent } from '@/types/api'

function makeEvent(type: WsEvent['type'] = 'payment.received'): WsEvent {
  return {
    type,
    wallet_id: 'wlt-1',
    payload: {
      transaction_id: 'txn-1',
      direction: 'CREDIT',
      amount_cents: 1000,
      amount_display: 'DD$ 10.00',
      new_balance_cents: 510000,
      new_balance_display: 'DD$ 5,100.00',
    },
    timestamp: new Date().toISOString(),
  }
}

// Reset store state between tests to avoid cross-test contamination.
beforeEach(() => {
  useNotificationStore.setState({ notifications: [], liveBalance: null })
})

describe('notificationStore', () => {
  it('adds a notification', () => {
    useNotificationStore.getState().addNotification(makeEvent())
    expect(useNotificationStore.getState().notifications).toHaveLength(1)
  })

  it('prepends new notifications (newest first)', () => {
    const e1 = makeEvent('payment.received')
    const e2 = makeEvent('payment.sent')
    useNotificationStore.getState().addNotification(e1)
    useNotificationStore.getState().addNotification(e2)
    expect(useNotificationStore.getState().notifications[0].event.type).toBe('payment.sent')
  })

  it('caps at 50 notifications', () => {
    for (let i = 0; i < 60; i++) {
      useNotificationStore.getState().addNotification(makeEvent())
    }
    expect(useNotificationStore.getState().notifications).toHaveLength(50)
  })

  it('dismiss marks notification as dismissed', () => {
    useNotificationStore.getState().addNotification(makeEvent())
    const id = useNotificationStore.getState().notifications[0].id
    useNotificationStore.getState().dismiss(id)
    expect(useNotificationStore.getState().notifications[0].dismissed).toBe(true)
  })

  it('dismiss does not remove the notification from the list', () => {
    useNotificationStore.getState().addNotification(makeEvent())
    const id = useNotificationStore.getState().notifications[0].id
    useNotificationStore.getState().dismiss(id)
    expect(useNotificationStore.getState().notifications).toHaveLength(1)
  })

  it('clearAll empties the list', () => {
    useNotificationStore.getState().addNotification(makeEvent())
    useNotificationStore.getState().addNotification(makeEvent())
    useNotificationStore.getState().clearAll()
    expect(useNotificationStore.getState().notifications).toHaveLength(0)
  })

  it('setLiveBalance updates liveBalance', () => {
    useNotificationStore.getState().setLiveBalance(123456)
    expect(useNotificationStore.getState().liveBalance).toBe(123456)
  })
})

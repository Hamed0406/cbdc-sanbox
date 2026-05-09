import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, act } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ToastContainer } from './Toast'
import { useNotificationStore } from '@/store/notificationStore'
import type { WsEvent } from '@/types/api'

function makeNotification(type: WsEvent['type'] = 'payment.received') {
  const event: WsEvent = {
    type,
    wallet_id: 'wlt-1',
    payload: type === 'issuance.received'
      ? {
          transaction_id: 'txn-1',
          amount_cents: 100000,
          amount_display: 'DD$ 1,000.00',
          reason: 'Initial funding',
          new_balance_cents: 600000,
          new_balance_display: 'DD$ 6,000.00',
        }
      : {
          transaction_id: 'txn-1',
          direction: type === 'payment.sent' ? 'DEBIT' : 'CREDIT',
          amount_cents: 1000,
          amount_display: 'DD$ 10.00',
          counterparty_name: 'Bob Smith',
          new_balance_cents: 490000,
          new_balance_display: 'DD$ 4,900.00',
        },
    timestamp: new Date().toISOString(),
  }
  useNotificationStore.getState().addNotification(event)
}

beforeEach(() => {
  useNotificationStore.setState({ notifications: [], liveBalance: null })
})

describe('ToastContainer', () => {
  it('renders nothing when there are no notifications', () => {
    const { container } = render(<ToastContainer />)
    expect(container).toBeEmptyDOMElement()
  })

  it('renders a payment received toast', () => {
    makeNotification('payment.received')
    render(<ToastContainer />)
    expect(screen.getByText('Payment Received')).toBeInTheDocument()
    expect(screen.getByText('From Bob Smith')).toBeInTheDocument()
    expect(screen.getByText(/\+ DD\$ 10\.00/)).toBeInTheDocument()
  })

  it('renders a payment sent toast', () => {
    makeNotification('payment.sent')
    render(<ToastContainer />)
    expect(screen.getByText('Payment Sent')).toBeInTheDocument()
    expect(screen.getByText('To Bob Smith')).toBeInTheDocument()
    expect(screen.getByText(/- DD\$ 10\.00/)).toBeInTheDocument()
  })

  it('renders an issuance toast', () => {
    makeNotification('issuance.received')
    render(<ToastContainer />)
    expect(screen.getByText('CBDC Received')).toBeInTheDocument()
    expect(screen.getByText('Initial funding')).toBeInTheDocument()
  })

  it('dismisses toast when × is clicked', async () => {
    makeNotification('payment.received')
    render(<ToastContainer />)

    await userEvent.click(screen.getByRole('button', { name: /dismiss/i }))
    expect(screen.queryByText('Payment Received')).not.toBeInTheDocument()
  })

  it('auto-dismisses after 5 seconds', () => {
    vi.useFakeTimers()
    makeNotification('payment.received')
    render(<ToastContainer />)

    expect(screen.getByText('Payment Received')).toBeInTheDocument()
    act(() => { vi.advanceTimersByTime(5100) })
    expect(screen.queryByText('Payment Received')).not.toBeInTheDocument()
    vi.useRealTimers()
  })

  it('shows at most 5 toasts at once', () => {
    for (let i = 0; i < 8; i++) makeNotification()
    render(<ToastContainer />)
    expect(screen.getAllByRole('alert')).toHaveLength(5)
  })
})

import { useEffect, useRef } from 'react'
import { useNotificationStore, type Notification } from '@/store/notificationStore'
import type { WsPaymentPayload, WsIssuancePayload } from '@/types/api'
import { cn } from '@/lib/cn'

// Auto-dismiss after 5 seconds. Long enough to read, short enough not to clutter.
const AUTO_DISMISS_MS = 5000

function ToastItem({ notification }: { notification: Notification }) {
  const dismiss = useNotificationStore((s) => s.dismiss)
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    timerRef.current = setTimeout(() => dismiss(notification.id), AUTO_DISMISS_MS)
    return () => {
      if (timerRef.current) clearTimeout(timerRef.current)
    }
  }, [notification.id, dismiss])

  const { event } = notification
  const isReceived = event.type === 'payment.received' || event.type === 'issuance.received'
  const isSent = event.type === 'payment.sent'

  let title = ''
  let body = ''
  let amountDisplay = ''

  if (event.type === 'payment.received') {
    const p = event.payload as WsPaymentPayload
    title = 'Payment Received'
    body = p.counterparty_name ? `From ${p.counterparty_name}` : 'From unknown sender'
    amountDisplay = p.amount_display
  } else if (event.type === 'payment.sent') {
    const p = event.payload as WsPaymentPayload
    title = 'Payment Sent'
    body = p.counterparty_name ? `To ${p.counterparty_name}` : 'Payment confirmed'
    amountDisplay = p.amount_display
  } else if (event.type === 'issuance.received') {
    const p = event.payload as WsIssuancePayload
    title = 'CBDC Received'
    body = p.reason
    amountDisplay = p.amount_display
  }

  return (
    <div
      role="alert"
      aria-live="polite"
      className={cn(
        'flex items-start gap-3 bg-slate-900 border rounded-xl p-4 shadow-xl min-w-[280px] max-w-sm',
        'animate-slide-in',
        isReceived ? 'border-emerald-500/40' : isSent ? 'border-sky-500/40' : 'border-slate-700',
      )}
    >
      {/* Colour dot */}
      <div
        className={cn(
          'mt-0.5 w-2 h-2 rounded-full flex-shrink-0',
          isReceived ? 'bg-emerald-500' : isSent ? 'bg-sky-500' : 'bg-slate-500',
        )}
      />
      <div className="flex-1 min-w-0">
        <p className="text-sm font-semibold text-slate-100">{title}</p>
        <p className="text-xs text-slate-400 truncate">{body}</p>
        <p
          className={cn(
            'text-sm font-bold mt-0.5',
            isReceived ? 'text-emerald-400' : isSent ? 'text-slate-300' : 'text-emerald-400',
          )}
        >
          {isSent ? `- ${amountDisplay}` : `+ ${amountDisplay}`}
        </p>
      </div>
      <button
        onClick={() => dismiss(notification.id)}
        className="text-slate-500 hover:text-slate-300 text-lg leading-none flex-shrink-0"
        aria-label="Dismiss notification"
      >
        ×
      </button>
    </div>
  )
}

// ToastContainer is rendered once at the app root and shows all active toasts.
export function ToastContainer() {
  const notifications = useNotificationStore((s) =>
    s.notifications.filter((n) => !n.dismissed),
  )

  if (notifications.length === 0) return null

  return (
    <div
      className="fixed bottom-4 right-4 z-50 flex flex-col gap-2"
      aria-label="Notifications"
    >
      {notifications.slice(0, 5).map((n) => (
        <ToastItem key={n.id} notification={n} />
      ))}
    </div>
  )
}

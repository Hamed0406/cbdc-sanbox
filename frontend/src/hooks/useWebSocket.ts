import { useEffect, useRef, useCallback } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useAuthStore } from '@/store/authStore'
import { useNotificationStore } from '@/store/notificationStore'
import type { WsEvent, WsPaymentPayload, WsIssuancePayload } from '@/types/api'

// Reconnect delays in ms for exponential backoff: 1s, 2s, 4s, 8s, capped at 30s.
const RECONNECT_DELAYS = [1000, 2000, 4000, 8000, 16000, 30000]

export interface UseWebSocketOptions {
  enabled?: boolean
}

export function useWebSocket({ enabled = true }: UseWebSocketOptions = {}) {
  const wsRef = useRef<WebSocket | null>(null)
  const reconnectAttempt = useRef(0)
  const reconnectTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const isMounted = useRef(true)

  const accessToken = useAuthStore((s) => s.accessToken)
  const walletId = useAuthStore((s) => s.wallet?.id)
  const { addNotification, setLiveBalance } = useNotificationStore()
  const queryClient = useQueryClient()

  const handleEvent = useCallback(
    (event: WsEvent) => {
      addNotification(event)

      // Update live balance from the event payload so the UI reflects the new
      // balance immediately, without waiting for the next REST poll.
      if (event.type === 'payment.sent' || event.type === 'payment.received') {
        const p = event.payload as WsPaymentPayload
        setLiveBalance(p.new_balance_cents)
      } else if (event.type === 'issuance.received') {
        const p = event.payload as WsIssuancePayload
        setLiveBalance(p.new_balance_cents)
      }

      // Also invalidate the balance query so the next REST call is fresh.
      queryClient.invalidateQueries({ queryKey: ['balance', walletId] })
      // Invalidate transactions so the history list shows the new transaction.
      queryClient.invalidateQueries({ queryKey: ['transactions', walletId] })
    },
    [addNotification, setLiveBalance, queryClient, walletId],
  )

  const connect = useCallback(() => {
    if (!accessToken || !walletId || !enabled || !isMounted.current) return

    // Passing the token as a query param because the browser's native WebSocket API
    // does not support custom request headers on the initial upgrade request.
    const url = `/api/v1/ws?token=${encodeURIComponent(accessToken)}`
    const ws = new WebSocket(url)
    wsRef.current = ws

    ws.onopen = () => {
      reconnectAttempt.current = 0
    }

    ws.onmessage = (e: MessageEvent<string>) => {
      try {
        const event = JSON.parse(e.data) as WsEvent
        handleEvent(event)
      } catch {
        // Malformed JSON from server — ignore and continue.
      }
    }

    ws.onclose = () => {
      wsRef.current = null
      if (!isMounted.current) return

      // Exponential backoff reconnect — capped at the last delay value.
      const delay =
        RECONNECT_DELAYS[Math.min(reconnectAttempt.current, RECONNECT_DELAYS.length - 1)]
      reconnectAttempt.current += 1
      reconnectTimer.current = setTimeout(connect, delay)
    }

    ws.onerror = () => {
      // onerror is always followed by onclose, which handles reconnect.
      ws.close()
    }
  }, [accessToken, walletId, enabled, handleEvent])

  useEffect(() => {
    isMounted.current = true
    connect()

    return () => {
      isMounted.current = false
      if (reconnectTimer.current) clearTimeout(reconnectTimer.current)
      wsRef.current?.close()
    }
  }, [connect])

  const disconnect = useCallback(() => {
    isMounted.current = false
    if (reconnectTimer.current) clearTimeout(reconnectTimer.current)
    wsRef.current?.close()
  }, [])

  return { disconnect }
}

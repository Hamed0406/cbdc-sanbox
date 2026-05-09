import { useQuery } from '@tanstack/react-query'
import { walletApi } from '@/api/wallet'
import { useAuthStore } from '@/store/authStore'
import { useNotificationStore } from '@/store/notificationStore'

export function useBalance() {
  const walletId = useAuthStore((s) => s.wallet?.id)
  // Live balance from WebSocket takes priority over the last REST response.
  // This avoids a visible balance "flicker" back to the stale REST value
  // when a WebSocket event arrives before the next refetch.
  const liveBalance = useNotificationStore((s) => s.liveBalance)

  const query = useQuery({
    queryKey: ['balance', walletId],
    queryFn: () => walletApi.getBalance(walletId!),
    enabled: !!walletId,
    staleTime: 30_000, // consider balance stale after 30s
  })

  return {
    ...query,
    balanceCents: liveBalance ?? query.data?.balance_cents,
    balanceDisplay: query.data?.balance_display,
  }
}

export function useTransactions(page = 1) {
  const walletId = useAuthStore((s) => s.wallet?.id)

  return useQuery({
    queryKey: ['transactions', walletId, page],
    queryFn: () => walletApi.getTransactions(walletId!, { page }),
    enabled: !!walletId,
    staleTime: 10_000,
  })
}

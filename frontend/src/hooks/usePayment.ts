import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { paymentApi } from '@/api/payment'
import { useAuthStore } from '@/store/authStore'
import type { SendRequest } from '@/types/api'

export function useSendPayment() {
  const queryClient = useQueryClient()
  const walletId = useAuthStore((s) => s.wallet?.id)

  return useMutation({
    mutationFn: (data: SendRequest) => paymentApi.send(data),
    onSuccess: () => {
      // Invalidate balance and transaction queries so they refetch with the new values.
      // WebSocket will also push a live balance update, but the REST refetch ensures
      // consistency if the WS event was missed (e.g. brief disconnect).
      queryClient.invalidateQueries({ queryKey: ['balance', walletId] })
      queryClient.invalidateQueries({ queryKey: ['transactions', walletId] })
    },
  })
}

export function usePaymentList(page = 1) {
  return useQuery({
    queryKey: ['payments', page],
    queryFn: () => paymentApi.list({ page }),
    staleTime: 10_000,
  })
}

import { Card } from '@/components/ui/Card'
import { Spinner } from '@/components/ui/Spinner'
import { useBalance } from '@/hooks/useWallet'
import { formatCents } from '@/lib/currency'

export function BalanceCard() {
  const { balanceCents, isLoading, isError } = useBalance()

  return (
    <Card className="text-center">
      <p className="text-sm text-slate-400 mb-2">Available Balance</p>
      {isLoading && <Spinner className="mx-auto" />}
      {isError && <p className="text-red-400 text-sm">Failed to load balance</p>}
      {balanceCents !== undefined && (
        <p
          data-testid="balance-display"
          className="text-4xl font-bold text-gold-400 tracking-tight"
        >
          {formatCents(balanceCents)}
        </p>
      )}
    </Card>
  )
}

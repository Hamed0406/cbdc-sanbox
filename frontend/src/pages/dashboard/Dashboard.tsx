import { Link } from 'react-router-dom'
import { BalanceCard } from '@/components/wallet/BalanceCard'
import { TransactionRow } from '@/components/transaction/TransactionRow'
import { Card, CardHeader, CardTitle } from '@/components/ui/Card'
import { Button } from '@/components/ui/Button'
import { Spinner } from '@/components/ui/Spinner'
import { useTransactions } from '@/hooks/useWallet'
import { useAuthStore } from '@/store/authStore'

export function DashboardPage() {
  const user = useAuthStore((s) => s.user)
  const { data, isLoading } = useTransactions()

  const recent = data?.transactions.slice(0, 5) ?? []

  return (
    <div className="p-6 max-w-2xl mx-auto space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-slate-100">
          Welcome, {user?.full_name?.split(' ')[0]}
        </h1>
        <p className="text-slate-400 text-sm mt-1">Your DigitalDollar wallet</p>
      </div>

      <BalanceCard />

      {/* Quick actions */}
      <div className="grid grid-cols-2 gap-3">
        <Link to="/send">
          <Button variant="primary" size="lg" className="w-full">
            ↑ Send
          </Button>
        </Link>
        <Button variant="secondary" size="lg" disabled>
          ↓ Receive (soon)
        </Button>
      </div>

      {/* Recent transactions */}
      <Card>
        <CardHeader className="flex items-center justify-between">
          <CardTitle>Recent Activity</CardTitle>
          <Link to="/history" className="text-xs text-emerald-400 hover:text-emerald-300">
            View all
          </Link>
        </CardHeader>

        {isLoading && <Spinner className="mx-auto my-4" />}

        {!isLoading && recent.length === 0 && (
          <p className="text-sm text-slate-500 text-center py-4">No transactions yet.</p>
        )}

        {recent.map((tx) => (
          <TransactionRow key={tx.id} tx={tx} />
        ))}
      </Card>
    </div>
  )
}

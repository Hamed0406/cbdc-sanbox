import { useState } from 'react'
import { TransactionRow } from '@/components/transaction/TransactionRow'
import { Card, CardHeader, CardTitle } from '@/components/ui/Card'
import { Button } from '@/components/ui/Button'
import { Spinner } from '@/components/ui/Spinner'
import { useTransactions } from '@/hooks/useWallet'

export function HistoryPage() {
  const [page, setPage] = useState(1)
  const { data, isLoading, isError } = useTransactions(page)

  const txns = data?.transactions ?? []
  const pagination = data?.pagination

  return (
    <div className="p-6 max-w-2xl mx-auto">
      <h1 className="text-2xl font-bold text-slate-100 mb-6">Transaction History</h1>

      <Card>
        <CardHeader>
          <CardTitle>All Transactions</CardTitle>
        </CardHeader>

        {isLoading && <Spinner className="mx-auto my-6" />}

        {isError && (
          <p className="text-sm text-red-400 text-center py-4">Failed to load transactions.</p>
        )}

        {!isLoading && txns.length === 0 && (
          <p className="text-sm text-slate-500 text-center py-6">No transactions found.</p>
        )}

        {txns.map((tx) => (
          <TransactionRow key={tx.id} tx={tx} />
        ))}

        {/* Pagination */}
        {pagination && pagination.pages > 1 && (
          <div className="flex items-center justify-between mt-4 pt-4 border-t border-slate-800">
            <Button
              variant="ghost"
              size="sm"
              disabled={page <= 1}
              onClick={() => setPage((p) => p - 1)}
            >
              ← Previous
            </Button>
            <span className="text-sm text-slate-400">
              Page {pagination.page} of {pagination.pages}
            </span>
            <Button
              variant="ghost"
              size="sm"
              disabled={page >= pagination.pages}
              onClick={() => setPage((p) => p + 1)}
            >
              Next →
            </Button>
          </div>
        )}
      </Card>
    </div>
  )
}

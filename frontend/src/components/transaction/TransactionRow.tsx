import { format } from 'date-fns'
import { Badge } from '@/components/ui/Badge'
import type { Transaction } from '@/types/api'
import { cn } from '@/lib/cn'

interface TransactionRowProps {
  tx: Transaction
}

export function TransactionRow({ tx }: TransactionRowProps) {
  const isCredit = tx.direction === 'CREDIT'
  const isDebit = tx.direction === 'DEBIT'

  return (
    <div className="flex items-center justify-between py-3 border-b border-slate-800 last:border-0">
      <div className="flex items-center gap-3">
        {/* Direction indicator */}
        <div
          className={cn(
            'w-9 h-9 rounded-full flex items-center justify-center text-sm flex-shrink-0',
            isCredit ? 'bg-emerald-500/15 text-emerald-400' : 'bg-slate-700/50 text-slate-400',
          )}
        >
          {isCredit ? '↓' : '↑'}
        </div>

        <div className="min-w-0">
          <p className="text-sm font-medium text-slate-100 truncate">
            {tx.counterparty_name ?? (tx.type === 'ISSUANCE' ? 'CBDC System' : 'Unknown')}
          </p>
          <p className="text-xs text-slate-500">
            {format(new Date(tx.created_at), 'MMM d, h:mm a')}
            {tx.reference ? ` · ${tx.reference}` : ''}
          </p>
        </div>
      </div>

      <div className="flex items-center gap-3 flex-shrink-0">
        <StatusBadge status={tx.status} />
        <p
          className={cn(
            'text-sm font-semibold w-24 text-right',
            isCredit ? 'text-emerald-400' : isDebit ? 'text-slate-300' : 'text-slate-400',
          )}
        >
          {isCredit ? '+ ' : isDebit ? '- ' : ''}
          {tx.amount_display}
        </p>
      </div>
    </div>
  )
}

function StatusBadge({ status }: { status: Transaction['status'] }) {
  const map: Record<Transaction['status'], { label: string; variant: 'success' | 'warning' | 'danger' | 'muted' }> = {
    SETTLED:   { label: 'Settled',  variant: 'success' },
    CONFIRMED: { label: 'Confirmed',variant: 'success' },
    PENDING:   { label: 'Pending',  variant: 'warning' },
    FAILED:    { label: 'Failed',   variant: 'danger'  },
    REFUNDED:  { label: 'Refunded', variant: 'muted'   },
  }
  const { label, variant } = map[status] ?? { label: status, variant: 'muted' }
  return <Badge variant={variant}>{label}</Badge>
}

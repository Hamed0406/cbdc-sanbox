import { cn } from '@/lib/cn'

export function Spinner({ className }: { className?: string }) {
  return (
    <div
      role="status"
      aria-label="Loading"
      className={cn(
        'w-5 h-5 border-2 border-slate-600 border-t-emerald-500 rounded-full animate-spin',
        className,
      )}
    />
  )
}

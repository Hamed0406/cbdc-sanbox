import { useState, useRef, useEffect, useCallback } from 'react'
import { useQuery } from '@tanstack/react-query'
import { walletApi, type WalletSearchResult } from '@/api/wallet'
import { useAuthStore } from '@/store/authStore'
import { cn } from '@/lib/cn'

interface WalletPickerProps {
  value: string            // the currently selected wallet ID (empty string = nothing selected)
  onChange: (walletId: string, result?: WalletSearchResult) => void
  label?: string
  placeholder?: string
  disabled?: boolean
  error?: string
}

export function WalletPicker({
  value,
  onChange,
  label = 'Recipient Wallet',
  placeholder = 'Search by name or email…',
  disabled = false,
  error,
}: WalletPickerProps) {
  const [query, setQuery] = useState('')
  const [open, setOpen] = useState(false)
  const [selected, setSelected] = useState<WalletSearchResult | null>(null)
  const inputRef = useRef<HTMLInputElement>(null)
  const dropdownRef = useRef<HTMLDivElement>(null)
  const ownWalletId = useAuthStore((s) => s.wallet?.id)

  // Sync external resets: if parent clears value, clear internal selection too
  useEffect(() => {
    if (!value) {
      setSelected(null)
      setQuery('')
    }
  }, [value])

  // Debounced search — only fire after 250ms of idle typing
  const [debouncedQuery, setDebouncedQuery] = useState('')
  useEffect(() => {
    const id = setTimeout(() => setDebouncedQuery(query), 250)
    return () => clearTimeout(id)
  }, [query])

  const { data: results = [], isFetching } = useQuery({
    queryKey: ['wallet-search', debouncedQuery],
    queryFn: () => walletApi.search(debouncedQuery),
    enabled: debouncedQuery.length >= 2 && open,
    staleTime: 10_000,
  })

  // Exclude the user's own wallet from results (can't send to yourself)
  const filtered = results.filter((r) => r.wallet_id !== ownWalletId)

  // Close dropdown on outside click
  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (
        dropdownRef.current &&
        !dropdownRef.current.contains(e.target as Node) &&
        !inputRef.current?.contains(e.target as Node)
      ) {
        setOpen(false)
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [])

  const handleSelect = useCallback((result: WalletSearchResult) => {
    setSelected(result)
    setQuery('')
    setOpen(false)
    onChange(result.wallet_id, result)
  }, [onChange])

  const handleClear = () => {
    setSelected(null)
    setQuery('')
    onChange('')
    setTimeout(() => inputRef.current?.focus(), 0)
  }

  const handleInputChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    setQuery(e.target.value)
    setOpen(true)
    if (selected) {
      setSelected(null)
      onChange('')
    }
  }

  const showDropdown = open && debouncedQuery.length >= 2

  return (
    <div className="relative">
      {label && (
        <label className="block text-sm font-medium text-slate-300 mb-1.5">
          {label}
        </label>
      )}

      {/* Selected state: show a chip instead of the input */}
      {selected ? (
        <div className="flex items-center gap-3 px-3 py-2.5 bg-slate-800 border border-emerald-600/50 rounded-lg">
          <Avatar name={selected.owner_name} />
          <div className="flex-1 min-w-0">
            <p className="text-sm font-medium text-slate-100 truncate">{selected.owner_name}</p>
            <p className="text-xs text-slate-400 truncate">{selected.owner_email}</p>
          </div>
          {selected.is_frozen && (
            <span className="text-xs text-amber-400 shrink-0">Frozen</span>
          )}
          {!disabled && (
            <button
              type="button"
              onClick={handleClear}
              className="text-slate-500 hover:text-slate-300 shrink-0 ml-1"
              aria-label="Clear selection"
            >
              ✕
            </button>
          )}
        </div>
      ) : (
        <input
          ref={inputRef}
          type="text"
          value={query}
          onChange={handleInputChange}
          onFocus={() => { if (query.length >= 2) setOpen(true) }}
          placeholder={placeholder}
          disabled={disabled}
          className={cn(
            'w-full px-3 py-2.5 bg-slate-800 border rounded-lg text-sm text-slate-100',
            'placeholder:text-slate-500 focus:outline-none focus:ring-2 focus:ring-emerald-500/50',
            error ? 'border-red-500' : 'border-slate-700 focus:border-emerald-500',
            disabled && 'opacity-50 cursor-not-allowed',
          )}
        />
      )}

      {error && !selected && (
        <p className="text-xs text-red-400 mt-1">{error}</p>
      )}

      {/* Dropdown */}
      {showDropdown && (
        <div
          ref={dropdownRef}
          className="absolute z-50 w-full mt-1 bg-slate-800 border border-slate-700 rounded-lg shadow-xl overflow-hidden"
        >
          {isFetching && (
            <div className="px-4 py-3 text-sm text-slate-400">Searching…</div>
          )}

          {!isFetching && filtered.length === 0 && (
            <div className="px-4 py-3 text-sm text-slate-400">
              No wallets found for "{debouncedQuery}"
            </div>
          )}

          {!isFetching && filtered.length > 0 && (
            <ul>
              {filtered.map((result) => (
                <li key={result.wallet_id}>
                  <button
                    type="button"
                    onMouseDown={(e) => {
                      // mousedown fires before blur — prevent the blur from closing
                      // the dropdown before the click registers
                      e.preventDefault()
                      handleSelect(result)
                    }}
                    className={cn(
                      'w-full flex items-center gap-3 px-3 py-2.5 text-left',
                      'hover:bg-slate-700 transition-colors',
                      result.is_frozen && 'opacity-60',
                    )}
                  >
                    <Avatar name={result.owner_name} />
                    <div className="flex-1 min-w-0">
                      <p className="text-sm font-medium text-slate-100 truncate">
                        {result.owner_name}
                        {result.is_frozen && (
                          <span className="ml-2 text-xs text-amber-400">Frozen</span>
                        )}
                      </p>
                      <p className="text-xs text-slate-400 truncate">{result.owner_email}</p>
                    </div>
                    <span className="text-xs text-slate-500 shrink-0">
                      {result.balance_display}
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </div>
  )
}

// Avatar shows the first letter of the name in a coloured circle.
function Avatar({ name }: { name: string }) {
  const letter = name.charAt(0).toUpperCase()
  // Deterministic colour from name: cycle through a set of muted palette colours
  const colours = [
    'bg-emerald-700', 'bg-blue-700', 'bg-purple-700',
    'bg-pink-700',    'bg-amber-700', 'bg-teal-700',
  ]
  const idx = name.charCodeAt(0) % colours.length
  return (
    <span className={cn('w-8 h-8 rounded-full flex items-center justify-center text-sm font-bold text-white shrink-0', colours[idx])}>
      {letter}
    </span>
  )
}

import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { adminApi, type IssueCBDCResponse } from '@/api/admin'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'
import { Card, CardHeader, CardTitle } from '@/components/ui/Card'
import { WalletPicker } from '@/components/wallet/WalletPicker'
import type { WalletSearchResult } from '@/api/wallet'
import { parseToCents, formatCents } from '@/lib/currency'

type Step = 'form' | 'confirm' | 'success'

export function AdminIssuePage() {
  const [step, setStep] = useState<Step>('form')
  const [walletId, setWalletId] = useState('')
  const [selectedWallet, setSelectedWallet] = useState<WalletSearchResult | null>(null)
  const [amountStr, setAmountStr] = useState('')
  const [reason, setReason] = useState('')
  const [fieldError, setFieldError] = useState('')
  const [result, setResult] = useState<IssueCBDCResponse | null>(null)

  const issue = useMutation({
    mutationFn: adminApi.issueCBDC,
    onSuccess: (data) => {
      setResult(data)
      setStep('success')
    },
    onError: (err: any) => {
      const msg = err?.response?.data?.error?.message ?? 'Issuance failed. Check the wallet ID and try again.'
      setFieldError(msg)
    },
  })

  const amountCents = parseToCents(amountStr)

  const handleContinue = (e: React.FormEvent) => {
    e.preventDefault()
    if (!walletId.trim()) {
      setFieldError('Please select a destination wallet.')
      return
    }
    if (isNaN(amountCents) || amountCents <= 0) {
      setFieldError('Enter a valid amount greater than zero.')
      return
    }
    if (amountCents > 100_000_000) {
      setFieldError('Maximum issuance is DD$ 1,000,000 per action.')
      return
    }
    if (!reason.trim()) {
      setFieldError('Reason is required.')
      return
    }
    setFieldError('')
    setStep('confirm')
  }

  const handleConfirm = () => {
    issue.mutate({
      wallet_id: walletId.trim(),
      amount_cents: amountCents,
      reason: reason.trim(),
    })
  }

  const handleReset = () => {
    setStep('form')
    setWalletId('')
    setSelectedWallet(null)
    setAmountStr('')
    setReason('')
    setFieldError('')
    setResult(null)
  }

  if (step === 'success' && result) {
    return (
      <div className="p-6 max-w-md mx-auto">
        <Card className="text-center py-10">
          <p className="text-5xl mb-4">✓</p>
          <p className="text-xl font-semibold text-emerald-400 mb-1">CBDC Issued!</p>
          <p className="text-slate-400 text-sm mb-6">
            {result.issuance.amount_display} minted successfully.
          </p>
          <div className="text-left space-y-2 mb-6 px-4">
            <Row label="New Balance" value={result.new_balance_display} highlight />
            <Row label="Transaction ID" value={result.issuance.transaction_id} mono />
            <Row label="Reason" value={result.issuance.reason} />
          </div>
          <Button variant="secondary" onClick={handleReset}>
            Issue Again
          </Button>
        </Card>
      </div>
    )
  }

  return (
    <div className="p-6 max-w-md mx-auto">
      <div className="mb-6">
        <h1 className="text-2xl font-bold text-slate-100">Issue CBDC</h1>
        <p className="text-slate-400 text-sm mt-1">
          Mint new DD$ into a wallet. All issuances are audit-logged.
        </p>
      </div>

      {step === 'form' && (
        <Card>
          <CardHeader>
            <CardTitle>Issuance Details</CardTitle>
          </CardHeader>
          <form onSubmit={handleContinue} className="space-y-4">
            <WalletPicker
              value={walletId}
              onChange={(id, result) => {
                setWalletId(id)
                setSelectedWallet(result ?? null)
                setFieldError('')
              }}
              label="Destination Wallet"
              error={!walletId && fieldError === 'Please select a destination wallet.' ? fieldError : undefined}
            />
            <Input
              id="amount"
              label="Amount (DD$)"
              type="number"
              placeholder="0.00"
              min="0.01"
              max="1000000"
              step="0.01"
              value={amountStr}
              onChange={(e) => {
                setAmountStr(e.target.value)
                setFieldError('')
              }}
              required
            />
            <Input
              id="reason"
              label="Reason / Justification"
              placeholder="e.g. Initial funding, stimulus disbursement"
              maxLength={500}
              value={reason}
              onChange={(e) => {
                setReason(e.target.value)
                setFieldError('')
              }}
              required
            />
            {fieldError && (
              <p role="alert" className="text-sm text-red-400">
                {fieldError}
              </p>
            )}
            <Button type="submit" variant="primary" size="lg" className="w-full">
              Review
            </Button>
          </form>
        </Card>
      )}

      {step === 'confirm' && (
        <Card>
          <CardHeader>
            <CardTitle>Confirm Issuance</CardTitle>
          </CardHeader>
          <div className="space-y-3 mb-6">
            {selectedWallet && <Row label="Recipient" value={`${selectedWallet.owner_name} · ${selectedWallet.owner_email}`} />}
            <Row label="Wallet ID" value={walletId} mono />
            <Row label="Amount" value={formatCents(amountCents)} highlight />
            <Row label="Reason" value={reason} />
          </div>
          <div className="rounded-lg bg-amber-900/20 border border-amber-700/40 px-4 py-3 mb-4">
            <p className="text-xs text-amber-300">
              This action mints new DD$ and is permanently recorded in the audit log.
            </p>
          </div>
          {fieldError && (
            <p role="alert" className="text-sm text-red-400 mb-3">
              {fieldError}
            </p>
          )}
          <div className="flex gap-3">
            <Button
              variant="secondary"
              className="flex-1"
              onClick={() => { setStep('form'); setFieldError('') }}
            >
              Back
            </Button>
            <Button
              variant="primary"
              className="flex-1"
              loading={issue.isPending}
              onClick={handleConfirm}
            >
              Issue DD$
            </Button>
          </div>
        </Card>
      )}
    </div>
  )
}

function Row({
  label,
  value,
  mono = false,
  highlight = false,
}: {
  label: string
  value: string
  mono?: boolean
  highlight?: boolean
}) {
  return (
    <div className="flex justify-between items-start py-2 border-b border-slate-800 gap-4">
      <span className="text-sm text-slate-400 shrink-0">{label}</span>
      <span
        className={[
          'text-sm font-medium text-right',
          mono ? 'font-mono text-slate-300 text-xs break-all' : '',
          highlight ? 'text-emerald-400 font-bold text-base' : 'text-slate-100',
        ].join(' ')}
      >
        {value}
      </span>
    </div>
  )
}

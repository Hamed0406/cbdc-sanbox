import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useSendPayment } from '@/hooks/usePayment'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'
import { Card, CardHeader, CardTitle } from '@/components/ui/Card'
import { WalletPicker } from '@/components/wallet/WalletPicker'
import type { WalletSearchResult } from '@/api/wallet'
import { parseToCents, formatCents } from '@/lib/currency'

type Step = 'form' | 'confirm' | 'success'

export function SendPage() {
  const navigate = useNavigate()
  const send = useSendPayment()

  const [step, setStep] = useState<Step>('form')
  const [toWalletId, setToWalletId] = useState('')
  const [selectedWallet, setSelectedWallet] = useState<WalletSearchResult | null>(null)
  const [amountStr, setAmountStr] = useState('')
  const [reference, setReference] = useState('')
  const [fieldError, setFieldError] = useState('')

  const amountCents = parseToCents(amountStr)

  const handleContinue = (e: React.FormEvent) => {
    e.preventDefault()
    if (!toWalletId.trim()) {
      setFieldError('Please select a recipient wallet.')
      return
    }
    if (isNaN(amountCents) || amountCents <= 0) {
      setFieldError('Enter a valid amount greater than zero.')
      return
    }
    setFieldError('')
    setStep('confirm')
  }

  const handleConfirm = () => {
    send.mutate(
      { to_wallet_id: toWalletId, amount_cents: amountCents, reference: reference || undefined },
      {
        onSuccess: () => setStep('success'),
        onError: () => setFieldError('Payment failed. Please try again.'),
      },
    )
  }

  if (step === 'success') {
    return (
      <div className="p-6 max-w-md mx-auto">
        <Card className="text-center py-10">
          <p className="text-5xl mb-4">✓</p>
          <p className="text-xl font-semibold text-emerald-400 mb-1">Payment Sent!</p>
          <p className="text-slate-400 text-sm mb-6">{formatCents(amountCents)} sent successfully.</p>
          <Button variant="secondary" onClick={() => navigate('/dashboard')}>
            Back to Dashboard
          </Button>
        </Card>
      </div>
    )
  }

  return (
    <div className="p-6 max-w-md mx-auto">
      <h1 className="text-2xl font-bold text-slate-100 mb-6">Send DD$</h1>

      {step === 'form' && (
        <Card>
          <CardHeader>
            <CardTitle>Payment Details</CardTitle>
          </CardHeader>
          <form onSubmit={handleContinue} className="space-y-4">
            <WalletPicker
              value={toWalletId}
              onChange={(id, result) => {
                setToWalletId(id)
                setSelectedWallet(result ?? null)
                setFieldError('')
              }}
              label="Recipient Wallet"
              error={!toWalletId && fieldError === 'Please select a recipient wallet.' ? fieldError : undefined}
            />
            <Input
              id="amount"
              label="Amount (DD$)"
              type="number"
              placeholder="0.00"
              min="0.01"
              step="0.01"
              value={amountStr}
              onChange={(e) => setAmountStr(e.target.value)}
              required
            />
            <Input
              id="reference"
              label="Reference (optional)"
              placeholder="Coffee, dinner, etc."
              maxLength={256}
              value={reference}
              onChange={(e) => setReference(e.target.value)}
            />
            {fieldError && <p role="alert" className="text-sm text-red-400">{fieldError}</p>}
            <Button type="submit" variant="primary" size="lg" className="w-full">
              Continue
            </Button>
          </form>
        </Card>
      )}

      {step === 'confirm' && (
        <Card>
          <CardHeader>
            <CardTitle>Confirm Payment</CardTitle>
          </CardHeader>
          <div className="space-y-3 mb-6">
            {selectedWallet && <Row label="Recipient" value={`${selectedWallet.owner_name} · ${selectedWallet.owner_email}`} />}
            <Row label="Wallet ID" value={toWalletId} mono />
            <Row label="Amount" value={formatCents(amountCents)} highlight />
            {reference && <Row label="Reference" value={reference} />}
            <Row label="Fee" value="DD$ 0.00" />
          </div>
          {fieldError && <p role="alert" className="text-sm text-red-400 mb-3">{fieldError}</p>}
          <div className="flex gap-3">
            <Button variant="secondary" className="flex-1" onClick={() => setStep('form')}>
              Back
            </Button>
            <Button
              variant="primary"
              className="flex-1"
              loading={send.isPending}
              onClick={handleConfirm}
            >
              Confirm
            </Button>
          </div>
        </Card>
      )}
    </div>
  )
}

function Row({ label, value, mono = false, highlight = false }: {
  label: string; value: string; mono?: boolean; highlight?: boolean
}) {
  return (
    <div className="flex justify-between items-center py-2 border-b border-slate-800">
      <span className="text-sm text-slate-400">{label}</span>
      <span className={`text-sm font-medium ${mono ? 'font-mono text-slate-300 text-xs truncate max-w-[180px]' : ''} ${highlight ? 'text-gold-400 font-bold text-base' : 'text-slate-100'}`}>
        {value}
      </span>
    </div>
  )
}

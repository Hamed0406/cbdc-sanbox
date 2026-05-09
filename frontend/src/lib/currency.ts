import { CURRENCY_SYMBOL } from './constants'

// Format an integer cent amount to a human-readable string.
// 100050 → "DD$ 1,000.50"
// Matches the backend's currency.Format() output exactly.
export function formatCents(cents: number, symbol = CURRENCY_SYMBOL): string {
  const abs = Math.abs(cents)
  const dollars = Math.floor(abs / 100)
  const pennies = abs % 100
  const formatted = dollars.toLocaleString('en-US') + '.' + String(pennies).padStart(2, '0')
  return `${symbol} ${cents < 0 ? '-' : ''}${formatted}`
}

// Parse a display string back to cents. Returns NaN on invalid input.
export function parseToCents(value: string): number {
  const clean = value.replace(/[^0-9.]/g, '')
  const num = parseFloat(clean)
  if (isNaN(num)) return NaN
  return Math.round(num * 100)
}

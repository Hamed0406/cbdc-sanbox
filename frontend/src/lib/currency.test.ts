import { describe, it, expect } from 'vitest'
import { formatCents, parseToCents } from './currency'

describe('formatCents', () => {
  it('formats zero', () => {
    expect(formatCents(0)).toBe('DD$ 0.00')
  })
  it('formats cents under 100', () => {
    expect(formatCents(5)).toBe('DD$ 0.05')
  })
  it('formats whole dollars', () => {
    expect(formatCents(1000)).toBe('DD$ 10.00')
  })
  it('formats large amount with thousands separator', () => {
    expect(formatCents(500000)).toBe('DD$ 5,000.00')
  })
  it('formats negative amounts', () => {
    expect(formatCents(-1050)).toBe('DD$ -10.50')
  })
  it('uses a custom symbol', () => {
    expect(formatCents(100, '$')).toBe('$ 1.00')
  })
})

describe('parseToCents', () => {
  it('parses a decimal string', () => {
    expect(parseToCents('10.50')).toBe(1050)
  })
  it('parses a whole number string', () => {
    expect(parseToCents('5')).toBe(500)
  })
  it('strips non-numeric characters', () => {
    expect(parseToCents('DD$ 10.00')).toBe(1000)
  })
  it('returns NaN for empty string', () => {
    expect(parseToCents('')).toBeNaN()
  })
  it('returns NaN for non-numeric input', () => {
    expect(parseToCents('abc')).toBeNaN()
  })
})

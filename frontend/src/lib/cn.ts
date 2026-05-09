import { clsx, type ClassValue } from 'clsx'
import { twMerge } from 'tailwind-merge'

// Merge Tailwind classes safely — deduplicates conflicting utilities.
// Usage: cn('px-4 py-2', condition && 'bg-red-500', className)
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

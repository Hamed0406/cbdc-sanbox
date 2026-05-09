export const API_BASE = import.meta.env.VITE_API_BASE_URL ?? ''
export const WS_BASE = import.meta.env.VITE_WS_URL ?? ''
export const CURRENCY_SYMBOL = import.meta.env.VITE_CURRENCY_SYMBOL ?? 'DD$'
export const CURRENCY_NAME = import.meta.env.VITE_CURRENCY_NAME ?? 'DigitalDollar'

// Access token TTL in ms — used to schedule silent refresh before expiry.
// Backend default is 900s; we refresh at 80% of that (720s) to avoid clock skew.
export const ACCESS_TOKEN_REFRESH_AT_MS = 720_000

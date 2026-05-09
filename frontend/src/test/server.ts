import { setupServer } from 'msw/node'
import { authHandlers } from './handlers/auth'
import { walletHandlers } from './handlers/wallet'
import { paymentHandlers } from './handlers/payment'

// Central MSW server used by all tests.
// Handlers are split by domain to keep them manageable.
export const server = setupServer(
  ...authHandlers,
  ...walletHandlers,
  ...paymentHandlers,
)

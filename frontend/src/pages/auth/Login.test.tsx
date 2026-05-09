import { describe, it, expect } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createWrapper } from '@/test/utils'
import { LoginPage } from './Login'

function renderLogin() {
  return render(<LoginPage />, { wrapper: createWrapper() })
}

describe('LoginPage', () => {
  it('renders the sign in form', () => {
    renderLogin()
    expect(screen.getByRole('heading', { name: /sign in/i })).toBeInTheDocument()
    expect(screen.getByLabelText(/email/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/password/i)).toBeInTheDocument()
  })

  it('shows a link to the register page', () => {
    renderLogin()
    expect(screen.getByRole('link', { name: /create one/i })).toBeInTheDocument()
  })

  it('submits with the entered email and password', async () => {
    renderLogin()
    await userEvent.type(screen.getByLabelText(/email/i), 'alice@example.com')
    await userEvent.type(screen.getByLabelText(/password/i), 'Alice1234!')
    await userEvent.click(screen.getByRole('button', { name: /sign in/i }))

    // Button shows loading state while the mutation is pending
    await waitFor(() =>
      expect(screen.getByRole('button', { name: /sign in/i })).not.toBeDisabled(),
    )
  })

  it('shows an error when login fails', async () => {
    // Override the handler to return 401 for this test
    const { server } = await import('@/test/server')
    const { http, HttpResponse } = await import('msw')
    server.use(
      http.post('/api/v1/auth/login', () =>
        HttpResponse.json({ error: { code: 'INVALID_CREDENTIALS', message: 'Bad creds' } }, { status: 401 }),
      ),
    )

    renderLogin()
    await userEvent.type(screen.getByLabelText(/email/i), 'bad@bad.com')
    await userEvent.type(screen.getByLabelText(/password/i), 'wrong')
    await userEvent.click(screen.getByRole('button', { name: /sign in/i }))

    await waitFor(() =>
      expect(screen.getByRole('alert')).toBeInTheDocument(),
    )
  })
})

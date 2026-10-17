import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { useState } from 'react'
import { api, errorMessage } from '~/api/client'

export const Route = createFileRoute('/login_/verify')({
  validateSearch: (s: Record<string, unknown>) => ({
    token: typeof s.token === 'string' ? s.token : '',
    callbackURL: typeof s.callbackURL === 'string' ? s.callbackURL : undefined,
  }),
  head: () => ({ meta: [{ name: 'referrer', content: 'no-referrer' }] }),
  component: Verify,
})

// The link only opens this page. Signing in needs the button, so a mail scanner that prefetches the link uses nothing.
function Verify() {
  const { token, callbackURL } = Route.useSearch()
  const navigate = useNavigate()
  const [state, setState] = useState<{ busy: boolean; error?: string }>({ busy: false })

  async function confirm() {
    setState({ busy: true })
    const { data, error } = await api().POST('/api/auth/magic-link/verify', { body: { token, callbackURL } })
    if (error || !data) {
      setState({ busy: false, error: errorMessage(error, 'This link does not work any more. Ask for a new one.') })
      return
    }
    await navigate({ href: data.redirectTo.startsWith('/') ? data.redirectTo : '/', replace: true })
  }

  if (!token) {
    return (
      <main>
        <h1>This link is incomplete</h1>
        <a href="/login">Ask for a new sign-in link</a>
      </main>
    )
  }
  return (
    <main>
      <h1>Sign in to Open Communities</h1>
      <button type="button" onClick={confirm} disabled={state.busy}>
        {state.busy ? 'Signing in…' : 'Sign in'}
      </button>
      {state.error && (
        <p role="alert">
          {state.error} <a href="/login">Ask for a new link</a>
        </p>
      )}
    </main>
  )
}

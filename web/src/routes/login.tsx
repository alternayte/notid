import { useMutation, useSuspenseQuery } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { type FormEvent, useState } from 'react'
import { errorMessage } from '~/api/errors'
import { magicLinkSendMutation, signUpMutation, signupModeOptions } from '~/api/gen/@tanstack/react-query.gen'

export const Route = createFileRoute('/login')({
  loader: ({ context }) => context.queryClient.ensureQueryData(signupModeOptions()),
  component: Login,
})

type Status = { kind: 'idle' } | { kind: 'busy' } | { kind: 'sent'; email: string } | { kind: 'error'; message: string }

function Login() {
  const { mode } = useSuspenseQuery(signupModeOptions()).data
  const [tab, setTab] = useState<'signin' | 'signup'>('signin')
  const [status, setStatus] = useState<Status>({ kind: 'idle' })
  const sendLinkMutation = useMutation(magicLinkSendMutation())
  const signUp = useMutation(signUpMutation())

  async function sendLink(email: string) {
    await sendLinkMutation.mutateAsync({ body: { email } })
    setStatus({ kind: 'sent', email })
  }

  async function onSignIn(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const email = String(new FormData(e.currentTarget).get('email') ?? '')
    setStatus({ kind: 'busy' })
    try {
      await sendLink(email)
    } catch (err) {
      setStatus({ kind: 'error', message: errorMessage(err, 'The sign-in link could not be sent. Try again.') })
    }
  }

  async function onSignUp(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const form = new FormData(e.currentTarget)
    const email = String(form.get('email') ?? '')
    setStatus({ kind: 'busy' })
    try {
      await signUp.mutateAsync({
        body: {
          email,
          name: String(form.get('name') ?? ''),
          invite: String(form.get('invite') ?? '') || undefined,
        },
      })
    } catch (err) {
      setStatus({ kind: 'error', message: errorMessage(err, 'The account could not be created. Try again.') })
      return
    }
    try {
      await sendLink(email)
    } catch (err) {
      setStatus({ kind: 'error', message: errorMessage(err, 'The sign-in link could not be sent. Try again.') })
    }
  }

  if (status.kind === 'sent') {
    return (
      <main>
        <h1>Check your email</h1>
        <p>
          If {status.email} can receive a sign-in link, one is on its way. Open it on this device. The link works once
          and expires in 15 minutes.
        </p>
        <button type="button" onClick={() => setStatus({ kind: 'idle' })}>
          Use another address
        </button>
      </main>
    )
  }

  const busy = status.kind === 'busy'
  return (
    <main>
      <h1>{tab === 'signin' ? 'Sign in' : 'Create an account'}</h1>
      {mode !== 'closed' && (
        <div role="tablist">
          <button type="button" role="tab" aria-selected={tab === 'signin'} onClick={() => setTab('signin')}>
            Sign in
          </button>
          <button type="button" role="tab" aria-selected={tab === 'signup'} onClick={() => setTab('signup')}>
            Create an account
          </button>
        </div>
      )}
      {tab === 'signin' ? (
        <form onSubmit={onSignIn}>
          <label>
            Email
            <input name="email" type="email" autoComplete="email" required />
          </label>
          <button type="submit" disabled={busy}>
            {busy ? 'Sending…' : 'Email me a sign-in link'}
          </button>
        </form>
      ) : (
        <form onSubmit={onSignUp}>
          <label>
            Email
            <input name="email" type="email" autoComplete="email" required />
          </label>
          <label>
            Name
            <input
              name="name"
              required
              pattern="[a-z][a-z0-9_]{2,19}"
              title="3 to 20 characters: lowercase letters, digits and underscores, starting with a letter"
              autoComplete="username"
            />
          </label>
          {mode === 'invite' && (
            <label>
              Invite code
              <input name="invite" required autoComplete="off" />
            </label>
          )}
          <button type="submit" disabled={busy}>
            {busy ? 'Creating…' : 'Create account'}
          </button>
        </form>
      )}
      {status.kind === 'error' && <p role="alert">{status.message}</p>}
    </main>
  )
}

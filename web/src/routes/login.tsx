import { useMutation, useSuspenseQuery } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { type FormEvent, type ReactNode, useState } from 'react'
import { errorMessage } from '~/api/errors'
import { magicLinkSendMutation, signUpMutation, signupModeOptions } from '~/api/gen/@tanstack/react-query.gen'
import { Kicker } from '~/ui/Kicker'
import { Button } from '~/ui/primitives/button'
import { Input } from '~/ui/primitives/input'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '~/ui/primitives/tabs'
import { Page } from './__root'

export const Route = createFileRoute('/login')({
  loader: ({ context }) => context.queryClient.ensureQueryData(signupModeOptions()),
  head: () => ({ meta: [{ title: 'Sign in · Open Communities' }] }),
  component: Login,
})

type Status = { kind: 'idle' } | { kind: 'busy' } | { kind: 'sent'; email: string } | { kind: 'error'; message: string }
type Tab = 'signin' | 'signup'

function Login() {
  const { mode } = useSuspenseQuery(signupModeOptions()).data
  const [tab, setTab] = useState<Tab>('signin')
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
      <Column>
        <Kicker>One more step</Kicker>
        <h1 className="font-display text-4xl tracking-tight">Check your email</h1>
        <p className="font-serif text-lg text-ink-soft">
          If <span className="font-semibold text-ink break-all">{status.email}</span> can receive a sign-in link, one is
          on its way. Open it on this device.
        </p>
        <p className="font-sans text-sm text-ink-faint">
          The link works once and expires in 15 minutes. Nothing there? Look in spam, or ask again.
        </p>
        <Button variant="outline" className="h-11 w-full sm:w-fit" onClick={() => setStatus({ kind: 'idle' })}>
          Use another address
        </Button>
      </Column>
    )
  }

  const busy = status.kind === 'busy'
  const error = status.kind === 'error' && (
    <p role="alert" className="rounded-sm bg-danger-wash px-3 py-2 font-sans text-sm text-danger">
      {status.message}
    </p>
  )

  const signIn = (
    <form onSubmit={onSignIn} className="flex flex-col gap-5">
      <Field id="signin-email" label="Email">
        <Input id="signin-email" name="email" type="email" autoComplete="email" inputMode="email" required />
      </Field>
      {error}
      <Button type="submit" className="h-11 w-full text-base" disabled={busy}>
        {busy ? 'Sending…' : 'Email me a sign-in link'}
      </Button>
      <p className="font-sans text-sm text-ink-faint">No password. We send a link that signs you in.</p>
    </form>
  )

  return (
    <Column>
      <Kicker>{tab === 'signin' ? 'Sign in' : 'New here'}</Kicker>
      <h1 className="font-display text-4xl tracking-tight">{tab === 'signin' ? 'Welcome back' : 'Join in'}</h1>
      <p className="font-serif text-lg text-ink-soft">
        {tab === 'signin'
          ? 'Sign in to post, vote and join communities here and on Lemmy and PieFed.'
          : 'One account to take part here and in communities on Lemmy and PieFed.'}
      </p>
      {mode === 'closed' ? (
        <>
          {signIn}
          <p className="font-sans text-sm text-ink-soft">This site is not taking new accounts at the moment.</p>
        </>
      ) : (
        <Tabs
          value={tab}
          onValueChange={(t) => {
            setTab(t as Tab)
            setStatus({ kind: 'idle' })
          }}
          className="mt-2 gap-6"
        >
          <TabsList aria-label="Sign in or create an account">
            <TabsTrigger value="signin">Sign in</TabsTrigger>
            <TabsTrigger value="signup">Create an account</TabsTrigger>
          </TabsList>
          <TabsContent value="signin">{signIn}</TabsContent>
          <TabsContent value="signup">
            <form onSubmit={onSignUp} className="flex flex-col gap-5">
              <Field id="signup-email" label="Email" hint="We use it to send sign-in links.">
                <Input
                  id="signup-email"
                  aria-describedby="signup-email-hint"
                  name="email"
                  type="email"
                  autoComplete="email"
                  inputMode="email"
                  required
                />
              </Field>
              <Field
                id="signup-name"
                label="Name"
                hint="Others see you by this name, and it never changes. 3 to 20 lowercase letters, digits or underscores."
              >
                <Input
                  id="signup-name"
                  aria-describedby="signup-name-hint"
                  name="name"
                  required
                  pattern="[a-z][a-z0-9_]{2,19}"
                  title="3 to 20 characters: lowercase letters, digits and underscores, starting with a letter"
                  autoComplete="username"
                  autoCapitalize="none"
                  autoCorrect="off"
                  spellCheck={false}
                />
              </Field>
              {mode === 'invite' && (
                <Field id="signup-invite" label="Invite code" hint="Ask someone who is already here for one.">
                  <Input
                    id="signup-invite"
                    aria-describedby="signup-invite-hint"
                    name="invite"
                    required
                    autoComplete="off"
                    autoCapitalize="none"
                    className="font-mono"
                  />
                </Field>
              )}
              {error}
              <Button type="submit" className="h-11 w-full text-base" disabled={busy}>
                {busy ? 'Creating…' : 'Create account'}
              </Button>
            </form>
          </TabsContent>
        </Tabs>
      )}
    </Column>
  )
}

function Column({ children }: { children: ReactNode }) {
  return (
    <Page>
      <div className="mx-auto flex w-full max-w-md flex-col gap-4 sm:pt-8">{children}</div>
    </Page>
  )
}

function Field({ id, label, hint, children }: { id: string; label: string; hint?: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-2">
      <label htmlFor={id} className="font-sans text-sm font-semibold">
        {label}
      </label>
      {children}
      {hint && (
        <p id={`${id}-hint`} className="font-sans text-xs text-ink-faint">
          {hint}
        </p>
      )}
    </div>
  )
}

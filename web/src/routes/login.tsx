import { useMutation, useSuspenseQuery } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { type ComponentProps, type FormEvent, type ReactNode, useState } from 'react'
import { errorMessage } from '~/api/errors'
import {
  gitHubStartMutation,
  magicLinkSendMutation,
  signUpMutation,
  signupModeOptions,
} from '~/api/gen/@tanstack/react-query.gen'
import { Kicker } from '~/ui/Kicker'
import { Button } from '~/ui/primitives/button'
import { Input } from '~/ui/primitives/input'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '~/ui/primitives/tabs'
import { Page } from './__root'

export const Route = createFileRoute('/login')({
  validateSearch: (s: Record<string, unknown>): { error?: string } =>
    typeof s.error === 'string' ? { error: s.error } : {},
  loader: ({ context }) => context.queryClient.ensureQueryData(signupModeOptions()),
  head: () => ({ meta: [{ title: 'Sign in · Open Communities' }] }),
  component: Login,
})

type Status = { kind: 'idle' } | { kind: 'busy' } | { kind: 'sent'; email: string } | { kind: 'error'; message: string }
type Tab = 'signin' | 'signup'

// The GitHub callback lands here as ?error=CODE.
const gitHubErrors: Record<string, string> = {
  ACCOUNT_REQUIRED: 'No account uses this GitHub login. Create one first.',
  NAME_REQUIRED:
    'Your GitHub username is taken here, or does not fit the rules for a name. Choose a name, then continue with GitHub again.',
  NAME_TAKEN: 'That name is taken. Choose another.',
  INVITE_INVALID: 'That invite code is not valid or was already used.',
  SIGNUP_CLOSED: 'This site is not taking new accounts at the moment.',
  EMAIL_ALREADY_EXISTS:
    'An account here already uses the email address of this GitHub login. Sign in with an email link, then try GitHub again.',
  ACCOUNT_ALREADY_LINKED: 'This GitHub login belongs to another account here.',
  USER_DISABLED: 'This account is disabled.',
}
const signUpErrors = new Set(['ACCOUNT_REQUIRED', 'NAME_REQUIRED', 'NAME_TAKEN', 'INVITE_INVALID'])
const inviteKey = 'notid.invite'

function savedInvite() {
  try {
    return sessionStorage.getItem(inviteKey) ?? ''
  } catch {
    return ''
  }
}

function Login() {
  const { mode, github } = useSuspenseQuery(signupModeOptions()).data
  const { error: code } = Route.useSearch()
  const [tab, setTab] = useState<Tab>(code && signUpErrors.has(code) && mode !== 'closed' ? 'signup' : 'signin')
  const [status, setStatus] = useState<Status>(
    code
      ? { kind: 'error', message: gitHubErrors[code] ?? 'GitHub sign-in did not complete. Try again.' }
      : { kind: 'idle' },
  )
  const [invite, setInvite] = useState(savedInvite)
  const nameStep = code === 'NAME_REQUIRED' || code === 'NAME_TAKEN'
  const sendLinkMutation = useMutation(magicLinkSendMutation())
  const signUp = useMutation(signUpMutation())
  const gitHub = useMutation(gitHubStartMutation())

  async function startGitHub(signUp: boolean, name?: string) {
    if (signUp && mode === 'invite' && !invite.trim()) {
      setStatus({ kind: 'error', message: 'Enter your invite code first.' })
      return
    }
    setStatus({ kind: 'busy' })
    try {
      sessionStorage.setItem(inviteKey, invite)
    } catch {}
    try {
      const { url } = await gitHub.mutateAsync({
        body: { signUp, name: name || undefined, invite: (signUp && invite.trim()) || undefined },
      })
      window.location.assign(url)
    } catch (err) {
      setStatus({ kind: 'error', message: errorMessage(err, 'GitHub sign-in could not start. Try again.') })
    }
  }

  function onGitHubSignUp(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    startGitHub(true, String(new FormData(e.currentTarget).get('name') ?? ''))
  }

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
          invite: invite.trim() || undefined,
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
    <div className="flex flex-col gap-5">
      {error}
      {github && (
        <>
          <GitHubButton disabled={busy} onClick={() => startGitHub(false)}>
            Sign in with GitHub
          </GitHubButton>
          <Or />
        </>
      )}
      <form onSubmit={onSignIn} className="flex flex-col gap-5">
        <Field id="signin-email" label="Email">
          <Input id="signin-email" name="email" type="email" autoComplete="email" inputMode="email" required />
        </Field>
        <Button
          type="submit"
          className="h-11 w-full text-base"
          variant={github ? 'outline' : undefined}
          disabled={busy}
        >
          {busy ? 'Sending…' : 'Email me a sign-in link'}
        </Button>
        <p className="font-sans text-sm text-ink-faint">No password. We send a link that signs you in.</p>
      </form>
    </div>
  )

  const inviteField = mode === 'invite' && (
    <Field id="signup-invite" label="Invite code" hint="Ask someone who is already here for one.">
      <Input
        id="signup-invite"
        aria-describedby="signup-invite-hint"
        name="invite"
        value={invite}
        onChange={(e) => setInvite(e.target.value)}
        required={!github}
        autoComplete="off"
        autoCapitalize="none"
        className="font-mono"
      />
    </Field>
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
          <TabsContent value="signup" className="flex flex-col gap-5">
            {github && error}
            {github && inviteField}
            {github && (
              <form onSubmit={onGitHubSignUp} className="flex flex-col gap-5">
                {nameStep && (
                  <Field
                    id="github-name"
                    label="Name"
                    hint="Others see you by this name, and it never changes. 3 to 20 lowercase letters, digits or underscores."
                  >
                    <Input
                      id="github-name"
                      aria-describedby="github-name-hint"
                      name="name"
                      required
                      pattern="[a-z][a-z0-9_]{2,19}"
                      title="3 to 20 characters: lowercase letters, digits and underscores, starting with a letter"
                      autoCapitalize="none"
                      autoCorrect="off"
                      spellCheck={false}
                    />
                  </Field>
                )}
                <GitHubButton type="submit" disabled={busy}>
                  Continue with GitHub
                </GitHubButton>
                {!nameStep && (
                  <p className="font-sans text-sm text-ink-faint">Your GitHub username becomes your name here.</p>
                )}
              </form>
            )}
            {github && <Or />}
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
              {!github && inviteField}
              {!github && error}
              <Button
                type="submit"
                className="h-11 w-full text-base"
                variant={github ? 'outline' : undefined}
                disabled={busy}
              >
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

function Or() {
  return (
    <div className="flex items-center gap-3 font-sans text-xs text-ink-faint" aria-hidden>
      <span className="h-px flex-1 bg-rule" />
      or with email
      <span className="h-px flex-1 bg-rule" />
    </div>
  )
}

function GitHubButton({ children, ...props }: ComponentProps<typeof Button>) {
  return (
    <Button type="button" className="h-11 w-full gap-2 text-base" {...props}>
      <svg viewBox="0 0 16 16" className="size-5" fill="currentColor" aria-hidden>
        <path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0016 8c0-4.42-3.58-8-8-8z" />
      </svg>
      {children}
    </Button>
  )
}

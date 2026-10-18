import { useMutation, useQueryClient } from '@tanstack/react-query'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { errorMessage } from '~/api/errors'
import { magicLinkVerifyMutation, meQueryKey } from '~/api/gen/@tanstack/react-query.gen'
import { Kicker } from '~/ui/Kicker'
import { Button } from '~/ui/primitives/button'
import { Page } from './__root'

export const Route = createFileRoute('/login_/verify')({
  validateSearch: (s: Record<string, unknown>) => ({
    token: typeof s.token === 'string' ? s.token : '',
    callbackURL: typeof s.callbackURL === 'string' ? s.callbackURL : undefined,
  }),
  head: () => ({ meta: [{ title: 'Sign in · Open Communities' }, { name: 'referrer', content: 'no-referrer' }] }),
  component: Verify,
})

// The link only opens this page. Signing in needs the button, so a mail scanner that prefetches the link uses nothing.
function Verify() {
  const { token, callbackURL } = Route.useSearch()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const verify = useMutation({
    ...magicLinkVerifyMutation(),
    onSuccess: async (data) => {
      await queryClient.invalidateQueries({ queryKey: meQueryKey() })
      await navigate({ href: data.redirectTo.startsWith('/') ? data.redirectTo : '/', replace: true })
    },
  })

  if (!token) {
    return (
      <Page>
        <div className="mx-auto flex w-full max-w-md flex-col gap-4 sm:pt-8">
          <Kicker>Sign in</Kicker>
          <h1 className="font-display text-4xl tracking-tight">This link is incomplete</h1>
          <p className="font-serif text-lg text-ink-soft">Part of the link is missing. Ask for a new one.</p>
          <Button asChild className="h-11 w-full text-base sm:w-fit">
            <a href="/login">Ask for a new sign-in link</a>
          </Button>
        </div>
      </Page>
    )
  }
  return (
    <Page>
      <div className="mx-auto flex w-full max-w-md flex-col gap-4 sm:pt-8">
        <Kicker>Sign in</Kicker>
        <h1 className="font-display text-4xl tracking-tight">Sign in to Open Communities</h1>
        <p className="font-serif text-lg text-ink-soft">Your link is ready. Press the button to finish signing in.</p>
        <Button
          className="h-11 w-full text-base"
          onClick={() => verify.mutate({ body: { token, callbackURL } })}
          disabled={verify.isPending}
        >
          {verify.isPending ? 'Signing in…' : 'Sign in'}
        </Button>
        {verify.isError && (
          <div
            role="alert"
            className="flex flex-col gap-2 rounded-sm bg-danger-wash px-3 py-2 font-sans text-sm text-danger"
          >
            <p>{errorMessage(verify.error, 'This link does not work any more.')}</p>
            <a href="/login" className="w-fit font-semibold text-mark">
              Ask for a new link
            </a>
          </div>
        )}
      </div>
    </Page>
  )
}

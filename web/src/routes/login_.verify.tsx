import { useMutation, useQueryClient } from '@tanstack/react-query'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { errorMessage } from '~/api/errors'
import { magicLinkVerifyMutation, meQueryKey } from '~/api/gen/@tanstack/react-query.gen'

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
      <main>
        <h1>This link is incomplete</h1>
        <a href="/login">Ask for a new sign-in link</a>
      </main>
    )
  }
  return (
    <main>
      <h1>Sign in to Open Communities</h1>
      <button type="button" onClick={() => verify.mutate({ body: { token, callbackURL } })} disabled={verify.isPending}>
        {verify.isPending ? 'Signing in…' : 'Sign in'}
      </button>
      {verify.isError && (
        <p role="alert">
          {errorMessage(verify.error, 'This link does not work any more.')} <a href="/login">Ask for a new link</a>
        </p>
      )}
    </main>
  )
}

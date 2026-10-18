import { useMutation, useQuery } from '@tanstack/react-query'
import { createFileRoute, Link, useNavigate } from '@tanstack/react-router'
import { errorMessage } from '~/api/errors'
import { createCommunityMutation, meOptions } from '~/api/gen/@tanstack/react-query.gen'
import { SettingsForm } from '~/communities/SettingsForm'
import { Input } from '~/ui/primitives/input'
import { Page } from './__root'

export const Route = createFileRoute('/communities/new')({
  head: () => ({ meta: [{ title: 'Start a community · Open Communities' }] }),
  component: NewCommunity,
})

function NewCommunity() {
  const { data: me, isPending } = useQuery({ ...meOptions(), retry: false })
  const create = useMutation(createCommunityMutation())
  const navigate = useNavigate()

  return (
    <Page>
      <div className="flex flex-col gap-2">
        <h1 className="font-display text-4xl tracking-tight">Start a community</h1>
        <p className="max-w-(--container-measure) font-serif text-lg text-ink-soft">
          A community has a name, a purpose and rules. You become its owner. Anyone here, on Lemmy or on PieFed can join
          it.
        </p>
      </div>
      {!me && !isPending ? (
        <p className="font-sans text-sm text-ink-soft">
          <Link to="/login" className="font-semibold text-mark">
            Sign in
          </Link>{' '}
          to start a community.
        </p>
      ) : me?.trust === 'new' ? (
        <section className="flex max-w-(--container-measure) flex-col gap-2 rounded-sm bg-paper-sunk p-4">
          <h2 className="font-display text-lg tracking-tight">Not yet</h2>
          <p className="font-serif text-sm text-ink-soft">
            New accounts cannot start communities. Join a few, post and comment for a while, and this page opens for
            you.
          </p>
          <Link to="/discover" className="w-fit font-sans text-sm font-semibold text-mark">
            Find communities
          </Link>
        </section>
      ) : (
        <SettingsForm
          initial={{ title: '', description: '', rules: [], theme: {}, appealsContact: '', nsfw: false }}
          submitLabel="Start the community"
          busyLabel="Starting…"
          onSubmit={async (values, form) => {
            const name = String(form.get('name') ?? '')
            try {
              const c = await create.mutateAsync({ body: { name, ...values } })
              await navigate({ to: '/c/$handle', params: { handle: c.handle } })
            } catch (err) {
              return errorMessage(err, 'The community could not be created. Try again.')
            }
          }}
        >
          <div className="flex flex-col gap-2">
            <label htmlFor="name" className="font-sans text-sm font-semibold">
              Name
            </label>
            <Input
              id="name"
              name="name"
              required
              pattern="[a-z][a-z0-9_]{2,19}"
              title="3 to 20 characters: lowercase letters, digits and underscores, starting with a letter"
              autoComplete="off"
              aria-describedby="name-hint"
            />
            <p id="name-hint" className="font-sans text-xs text-ink-faint">
              Part of the community's address, and it never changes. 3 to 20 lowercase letters, digits or underscores.
            </p>
          </div>
        </SettingsForm>
      )}
    </Page>
  )
}

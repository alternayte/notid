import { useMutation, useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import { createFileRoute, Link, notFound } from '@tanstack/react-router'
import { errorMessage } from '~/api/errors'
import { communityOptions, communityQueryKey, updateCommunityMutation } from '~/api/gen/@tanstack/react-query.gen'
import { accentAttr } from '~/communities/accents'
import { SettingsForm } from '~/communities/SettingsForm'
import { Page } from './__root'

export const Route = createFileRoute('/c/$handle_/manage')({
  loader: async ({ context, params }) => {
    try {
      return await context.queryClient.ensureQueryData(communityOptions({ path: { handle: params.handle } }))
    } catch {
      throw notFound()
    }
  },
  head: ({ loaderData }) => ({
    meta: [{ title: `Manage ${loaderData?.title || loaderData?.name || 'community'} · Open Communities` }],
  }),
  component: Manage,
})

function Manage() {
  const { handle } = Route.useParams()
  const { data: c } = useSuspenseQuery(communityOptions({ path: { handle } }))
  const update = useMutation(updateCommunityMutation())
  const queryClient = useQueryClient()
  const allowed = c.local && (c.membership?.role === 'owner' || c.membership?.role === 'moderator')

  return (
    <div data-accent={accentAttr(c.theme)}>
      <Page>
        <div className="flex flex-col gap-2">
          <Link to="/c/$handle" params={{ handle }} className="w-fit font-sans text-sm text-ink-soft hover:text-ink">
            ← {c.title || c.name}
          </Link>
          <h1 className="font-display text-4xl tracking-tight">Manage the community</h1>
        </div>
        {!allowed ? (
          <p className="font-sans text-sm text-ink-soft">Only the community's owners and moderators can change it.</p>
        ) : (
          <SettingsForm
            initial={c}
            submitLabel="Save changes"
            busyLabel="Saving…"
            onSubmit={async (values) => {
              try {
                const d = await update.mutateAsync({ path: { handle }, body: values })
                queryClient.setQueryData(communityQueryKey({ path: { handle } }), d)
              } catch (err) {
                return errorMessage(err, 'The changes could not be saved. Try again.')
              }
            }}
          />
        )}
      </Page>
    </div>
  )
}

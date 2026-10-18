import { useMutation, useQuery } from '@tanstack/react-query'
import { createFileRoute, Link, useNavigate } from '@tanstack/react-router'
import { useId } from 'react'
import { errorMessage } from '~/api/errors'
import {
  createPostMutation,
  meOptions,
  myCommunitiesOptions,
  previewMarkdownMutation,
} from '~/api/gen/@tanstack/react-query.gen'
import { Composer } from '~/ui/Composer'
import { fromRenderer } from '~/ui/RenderedHtml'
import { Page } from './__root'

export const Route = createFileRoute('/submit')({
  validateSearch: (s: Record<string, unknown>): { community?: string } =>
    typeof s.community === 'string' && s.community ? { community: s.community } : {},
  head: () => ({ meta: [{ title: 'New post · Open Communities' }] }),
  loader: ({ context }) => context.queryClient.prefetchQuery({ ...myCommunitiesOptions(), retry: false }),
  component: Submit,
})

function Submit() {
  const { community } = Route.useSearch()
  const navigate = useNavigate({ from: Route.fullPath })
  const { data: me, isPending: mePending } = useQuery({ ...meOptions(), retry: false })
  const mine = useQuery({ ...myCommunitiesOptions(), retry: false, enabled: !!me })
  const create = useMutation(createPostMutation())
  const preview = useMutation(previewMarkdownMutation())
  const selectId = useId()

  if (!me && !mePending) {
    return (
      <Page>
        <h1 className="font-display text-4xl tracking-tight">New post</h1>
        <Link to="/login" className="w-fit font-sans text-sm font-semibold text-mark">
          Sign in to post
        </Link>
      </Page>
    )
  }
  const joined = (mine.data?.communities ?? []).filter((c) => c.membership?.state === 'accepted')
  const chosen = joined.find((c) => c.handle === community)

  return (
    <Page>
      <h1 className="font-display text-4xl tracking-tight">New post</h1>
      {mine.isError && (
        <p role="alert" className="font-sans text-sm text-danger">
          Your communities did not load. Reload the page to try again.
        </p>
      )}
      {mine.isSuccess && joined.length === 0 ? (
        <section className="flex max-w-(--container-measure) flex-col gap-2 rounded-sm bg-paper-sunk p-4">
          <h2 className="font-display text-lg tracking-tight">Join a community first</h2>
          <p className="font-serif text-sm text-ink-soft">You post in communities you are a member of.</p>
          <Link to="/discover" className="w-fit font-sans text-sm font-semibold text-mark">
            Discover communities
          </Link>
        </section>
      ) : (
        <div className="flex max-w-(--container-measure) flex-col gap-4">
          <div className="flex flex-col gap-1">
            <label htmlFor={selectId} className="font-sans text-xs font-bold uppercase tracking-kicker">
              Community
            </label>
            <select
              id={selectId}
              value={chosen?.handle ?? ''}
              onChange={(e) => navigate({ search: { community: e.target.value || undefined }, replace: true })}
              className="h-10 rounded-sm border border-rule bg-paper-raised px-3 font-sans text-sm focus-visible:outline-2 focus-visible:outline-mark"
            >
              <option value="">Choose a community</option>
              {joined.map((c) => (
                <option key={c.apId} value={c.handle}>
                  {c.title || c.name} ({c.handle})
                </option>
              ))}
            </select>
          </div>
          {chosen && (
            <Composer
              key={chosen.apId}
              community={chosen.title || chosen.name}
              kinds={['text', 'link']}
              startOpen
              preview={async (markdown) => fromRenderer((await preview.mutateAsync({ body: { markdown } })).html)}
              send={async (d) => {
                try {
                  const res = await create.mutateAsync({
                    body: { community: chosen.apId, title: d.title, body: d.body, url: d.url || undefined },
                  })
                  await navigate({ to: '/c/$handle/p/$ref', params: { handle: chosen.handle, ref: res.ref } })
                } catch (err) {
                  throw new Error(errorMessage(err, 'Post not sent.'))
                }
              }}
            />
          )}
        </div>
      )}
    </Page>
  )
}

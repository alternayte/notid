import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { createFileRoute, Link } from '@tanstack/react-router'
import { errorMessage } from '~/api/errors'
import { heldPostsOptions, heldPostsQueryKey, publishPostMutation } from '~/api/gen/@tanstack/react-query.gen'
import { Age } from '~/discussions/Age'
import { Button } from '~/ui/primitives/button'
import { Page } from './__root'

export const Route = createFileRoute('/c/$handle_/held')({
  loader: ({ context, params }) =>
    context.queryClient.prefetchQuery({ ...heldPostsOptions({ path: { handle: params.handle } }), retry: false }),
  head: ({ params }) => ({ meta: [{ title: `Held posts in ${params.handle} · Open Communities` }] }),
  component: Held,
})

function Held() {
  const { handle } = Route.useParams()
  const queryClient = useQueryClient()
  const { data, isError, isPending } = useQuery({
    ...heldPostsOptions({ path: { handle } }),
    retry: false,
  })
  const publish = useMutation(publishPostMutation())
  async function release(apId: string) {
    try {
      await publish.mutateAsync({ body: { object: apId } })
      await queryClient.invalidateQueries({ queryKey: heldPostsQueryKey({ path: { handle } }) })
    } catch (err) {
      alert(errorMessage(err, 'That post was not published. Try again.'))
    }
  }

  return (
    <Page>
      <div className="flex flex-col gap-2">
        <Link to="/c/$handle" params={{ handle }} className="w-fit font-sans text-sm text-ink-soft hover:text-ink">
          ← {handle}
        </Link>
        <h1 className="font-display text-4xl tracking-tight">Held posts</h1>
        <p className="max-w-(--container-measure) font-serif text-sm text-ink-soft">
          Posts from accounts that are not established yet. Nothing here is in the feeds, in search, or on other servers
          until you publish it.
        </p>
      </div>
      {isPending ? (
        <p className="font-sans text-sm text-ink-faint">Loading…</p>
      ) : isError || !data ? (
        <p role="alert" className="font-sans text-sm text-ink-soft">
          Only the community's owners and moderators can see held posts.
        </p>
      ) : data.posts.length === 0 ? (
        <section className="flex max-w-(--container-measure) flex-col gap-2 rounded-sm bg-paper-sunk p-4">
          <h2 className="font-display text-lg tracking-tight">Nothing is waiting</h2>
          <p className="font-serif text-sm text-ink-soft">
            A held post appears here as soon as a new person writes one.
          </p>
        </section>
      ) : (
        <ol className="flex max-w-(--container-measure) flex-col gap-6">
          {data.posts.map((p) => (
            <li key={p.apId} className="flex flex-col gap-2 border-t border-rule pt-4">
              <h2 className="font-display text-2xl tracking-tight text-balance">{p.title}</h2>
              <p className="font-sans text-xs text-ink-faint">
                {p.author.handle} · <Age iso={p.published} />
                {p.nsfw && ' · 18+'}
              </p>
              {p.excerpt && <p className="font-serif text-sm text-ink-soft">{p.excerpt}</p>}
              {p.url && (
                <a href={p.url} rel="nofollow noreferrer" className="font-sans text-sm text-mark break-all">
                  {p.url}
                </a>
              )}
              <Button className="w-fit" onClick={() => void release(p.apId)}>
                Publish this post
              </Button>
            </li>
          ))}
        </ol>
      )}
    </Page>
  )
}

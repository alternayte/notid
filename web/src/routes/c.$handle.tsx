import { useInfiniteQuery, useQuery, useSuspenseQuery } from '@tanstack/react-query'
import { createFileRoute, Link, notFound } from '@tanstack/react-router'
import { communityOptions, communityPostsInfiniteOptions, meOptions } from '~/api/gen/@tanstack/react-query.gen'
import type { Detail } from '~/api/gen/types.gen'
import { accentAttr } from '~/communities/accents'
import { membersLabel } from '~/communities/CommunityCard'
import { Join } from '~/communities/Join'
import { PostItem } from '~/discussions/PostItem'
import { Kicker } from '~/ui/Kicker'
import { Button } from '~/ui/primitives/button'
import { fromRenderer, Rendered } from '~/ui/RenderedHtml'
import { Page } from './__root'

export const Route = createFileRoute('/c/$handle')({
  loader: async ({ context, params }) => {
    let c: Detail
    try {
      c = await context.queryClient.ensureQueryData(communityOptions({ path: { handle: params.handle } }))
    } catch {
      throw notFound()
    }
    await context.queryClient.ensureInfiniteQueryData(postsOptions(params.handle))
    return c
  },
  head: ({ loaderData }) => ({
    meta: [{ title: `${loaderData?.title || loaderData?.name || 'Community'} · Open Communities` }],
  }),
  component: CommunityPage,
})

const postsOptions = (handle: string) => ({
  ...communityPostsInfiniteOptions({ path: { handle } }),
  initialPageParam: '',
  getNextPageParam: (last: { next: string }) => last.next || undefined,
})

function CommunityPage() {
  const { handle } = Route.useParams()
  const { data: c } = useSuspenseQuery(communityOptions({ path: { handle } }))
  const { data: me } = useQuery({ ...meOptions(), retry: false })
  const canManage = c.local && (c.membership?.role === 'owner' || c.membership?.role === 'moderator')

  return (
    <div data-accent={accentAttr(c.theme)}>
      <Page>
        {c.state !== 'active' && (
          <p role="status" className="rounded-sm bg-mark-wash px-4 py-3 font-sans text-sm">
            This community is moving to another site. It is read-only for now.
          </p>
        )}
        <header className="flex flex-col gap-3 border-b border-rule pb-6">
          <Kicker>{c.local ? 'Community' : `Community on ${c.domain}`}</Kicker>
          <h1 className="font-display text-4xl tracking-tight text-balance">{c.title || c.name}</h1>
          <p className="font-sans text-xs text-ink-faint">
            !{c.name}@{c.domain} · {membersLabel(c.members)}
            {!c.local && ' from this site'}
            {c.nsfw && ' · 18+'}
          </p>
          <div className="flex flex-wrap items-center gap-4 pt-1">
            <Join community={c} signedIn={!!me} />
            {canManage && (
              <Link
                to="/c/$handle/manage"
                params={{ handle }}
                className="font-sans text-sm font-semibold text-ink-soft hover:text-ink"
              >
                Manage
              </Link>
            )}
          </div>
        </header>
        <div className="grid gap-12 lg:grid-cols-[minmax(0,1fr)_18rem]">
          <section aria-labelledby="posts" className="flex flex-col gap-4">
            <h2
              id="posts"
              className="border-b border-rule-strong pb-1 font-sans text-xs font-bold uppercase tracking-kicker"
            >
              Posts · New
            </h2>
            {me && (c.state === 'active' || !c.local) && (
              <Link
                to="/submit"
                search={{ community: handle }}
                className="flex items-center gap-3 rounded-sm border border-rule bg-paper-raised px-4 py-3 font-serif text-lg text-ink-faint no-underline hover:text-ink-soft"
              >
                <span className="grid size-7 place-items-center rounded-pill bg-mark font-sans text-base text-mark-ink">
                  +
                </span>
                Start a post in {c.title || c.name}
              </Link>
            )}
            <Posts handle={handle} />
          </section>
          <About community={c} />
        </div>
      </Page>
    </div>
  )
}

function Posts({ handle }: { handle: string }) {
  // The loader fills this query, so data is present on the first render.
  const { data, isError, fetchNextPage, hasNextPage, isFetchingNextPage, isFetchNextPageError } = useInfiniteQuery(
    postsOptions(handle),
  )
  if (isError && !data) {
    return (
      <p role="alert" className="font-sans text-sm text-danger">
        Posts did not load. Reload the page to try again.
      </p>
    )
  }
  const posts = data?.pages.flatMap((p) => p.posts) ?? []
  if (data && posts.length === 0) {
    return (
      <div className="flex flex-col gap-2 rounded-sm bg-paper-sunk p-4">
        <h3 className="font-display text-lg tracking-tight">No posts yet</h3>
        <p className="font-serif text-sm text-ink-soft">The first post here starts the conversation.</p>
      </div>
    )
  }
  return (
    <div className="flex flex-col">
      {posts.map((p) => (
        <PostItem key={p.apId} post={p} showCommunity={false} />
      ))}
      {hasNextPage && (
        <div className="flex items-center gap-3 border-t border-rule pt-4">
          <Button variant="outline" disabled={isFetchingNextPage} onClick={() => fetchNextPage()}>
            {isFetchingNextPage ? 'Loading…' : 'Older posts'}
          </Button>
          {isFetchNextPageError && (
            <span role="alert" className="font-sans text-sm text-danger">
              Older posts did not load. Try again.
            </span>
          )}
        </div>
      )}
    </div>
  )
}

function About({ community: c }: { community: Detail }) {
  return (
    <aside aria-label={`About ${c.title || c.name}`} className="flex flex-col gap-6 lg:sticky lg:top-8 lg:self-start">
      <section className="flex flex-col gap-3 border-t-2 border-mark pt-3">
        <Kicker>About</Kicker>
        {c.descriptionHtml ? (
          <Rendered html={fromRenderer(c.descriptionHtml)} className="prose-news text-sm" />
        ) : (
          <p className="font-serif text-sm text-ink-faint">No description yet.</p>
        )}
      </section>
      {c.rules.length > 0 && (
        <section className="flex flex-col gap-3 border-t border-rule pt-3">
          <Kicker>Rules</Kicker>
          <ol className="flex list-decimal flex-col gap-2 pl-5 font-serif text-sm marker:font-sans marker:text-ink-faint">
            {c.rules.map((r) => (
              <li key={r}>{r}</li>
            ))}
          </ol>
        </section>
      )}
      {c.moderators.length > 0 && (
        <section className="flex flex-col gap-3 border-t border-rule pt-3">
          <Kicker>Moderators</Kicker>
          <ul className="flex flex-col gap-1 font-sans text-sm">
            {c.moderators.map((m) => (
              <li key={m.handle}>
                {m.displayName || m.handle}
                {m.role === 'owner' && <span className="text-ink-faint"> · owner</span>}
              </li>
            ))}
          </ul>
        </section>
      )}
      {c.appealsContact && (
        <section className="flex flex-col gap-2 border-t border-rule pt-3">
          <Kicker>Appeals</Kicker>
          <p className="font-sans text-sm text-ink-soft">
            To appeal a moderator's decision, contact <span className="text-ink">{c.appealsContact}</span>.
          </p>
        </section>
      )}
    </aside>
  )
}

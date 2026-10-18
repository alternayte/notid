import { useInfiniteQuery, useQuery, useSuspenseQuery } from '@tanstack/react-query'
import { createFileRoute, Link, notFound } from '@tanstack/react-router'
import { communityOptions, communityPostsInfiniteOptions, meOptions } from '~/api/gen/@tanstack/react-query.gen'
import type { Detail } from '~/api/gen/types.gen'
import { About } from '~/communities/About'
import { accentAttr } from '~/communities/accents'
import { membersLabel } from '~/communities/CommunityCard'
import { Join } from '~/communities/Join'
import { PostItem } from '~/discussions/PostItem'
import { Kicker } from '~/ui/Kicker'
import { Button } from '~/ui/primitives/button'
import { Page } from './__root'

const sorts = [
  { value: 'new', label: 'New' },
  { value: 'active', label: 'Active' },
  { value: 'top-day', label: 'Top today' },
  { value: 'top-week', label: 'Top this week' },
  { value: 'top-all', label: 'Top all time' },
] as const

type Sort = (typeof sorts)[number]['value']

export const Route = createFileRoute('/c/$handle')({
  validateSearch: (s: Record<string, unknown>): { sort?: Sort } =>
    sorts.some((o) => o.value === s.sort) && s.sort !== 'new' ? { sort: s.sort as Sort } : {},
  loaderDeps: ({ search }) => ({ sort: search.sort ?? 'new' }),
  loader: async ({ context, params, deps }) => {
    let c: Detail
    try {
      c = await context.queryClient.ensureQueryData(communityOptions({ path: { handle: params.handle } }))
    } catch {
      throw notFound()
    }
    await context.queryClient.ensureInfiniteQueryData(postsOptions(params.handle, deps.sort))
    return c
  },
  head: ({ loaderData }) => ({
    meta: [{ title: `${loaderData?.title || loaderData?.name || 'Community'} · Open Communities` }],
  }),
  component: CommunityPage,
})

const postsOptions = (handle: string, sort: Sort) => ({
  ...communityPostsInfiniteOptions({ path: { handle }, query: { sort } }),
  initialPageParam: '',
  getNextPageParam: (last: { next: string }) => last.next || undefined,
})

function CommunityPage() {
  const { handle } = Route.useParams()
  const sort = Route.useSearch().sort ?? 'new'
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
          {c.bannerUrl && (
            <img
              src={c.bannerUrl}
              alt=""
              className="mb-2 aspect-[4/1] w-full rounded-sm object-cover sm:aspect-[5/1]"
            />
          )}
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
              <>
                <Link
                  to="/c/$handle/manage"
                  params={{ handle }}
                  className="font-sans text-sm font-semibold text-ink-soft hover:text-ink"
                >
                  Manage
                </Link>
                <Link
                  to="/c/$handle/reports"
                  params={{ handle }}
                  className="font-sans text-sm font-semibold text-ink-soft hover:text-ink"
                >
                  Reports
                </Link>
                <Link
                  to="/c/$handle/held"
                  params={{ handle }}
                  className="font-sans text-sm font-semibold text-ink-soft hover:text-ink"
                >
                  Held posts
                </Link>
              </>
            )}
          </div>
        </header>
        <div className="grid gap-12 lg:grid-cols-[minmax(0,1fr)_18rem]">
          <section aria-labelledby="posts" className="flex flex-col gap-4">
            <div className="flex flex-wrap items-baseline justify-between gap-x-6 gap-y-2 border-b border-rule-strong pb-1">
              <h2 id="posts" className="font-sans text-xs font-bold uppercase tracking-kicker">
                Posts
              </h2>
              <nav aria-label="Sort posts" className="flex flex-wrap gap-x-4 gap-y-1">
                {sorts.map((o) => (
                  <Link
                    key={o.value}
                    to="/c/$handle"
                    params={{ handle }}
                    search={o.value === 'new' ? {} : { sort: o.value }}
                    activeOptions={{ exact: true }}
                    className="font-sans text-xs font-semibold text-ink-soft no-underline hover:text-ink data-[status=active]:text-ink data-[status=active]:underline data-[status=active]:decoration-mark data-[status=active]:decoration-2 data-[status=active]:underline-offset-[0.4em]"
                  >
                    {o.label}
                  </Link>
                ))}
              </nav>
            </div>
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
            <Posts handle={handle} sort={sort} />
          </section>
          <About community={c} />
        </div>
      </Page>
    </div>
  )
}

function Posts({ handle, sort }: { handle: string; sort: Sort }) {
  // The loader fills this query, so data is present on the first render.
  const { data, isError, fetchNextPage, hasNextPage, isFetchingNextPage, isFetchNextPageError } = useInfiniteQuery(
    postsOptions(handle, sort),
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

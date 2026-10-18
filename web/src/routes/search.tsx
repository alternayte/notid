import { useQuery, useSuspenseQuery } from '@tanstack/react-query'
import { createFileRoute, Link } from '@tanstack/react-router'
import {
  meOptions,
  searchCommentsOptions,
  searchCommunitiesOptions,
  searchPostsOptions,
} from '~/api/gen/@tanstack/react-query.gen'
import type { Summary } from '~/api/gen/types.gen'
import { CommunityCard } from '~/communities/CommunityCard'
import { CommentItem } from '~/discussions/CommentItem'
import { PostItem } from '~/discussions/PostItem'
import { Input } from '~/ui/primitives/input'
import { Page } from './__root'

export const Route = createFileRoute('/search')({
  validateSearch: (s: Record<string, unknown>): { q?: string } =>
    typeof s.q === 'string' && s.q.trim() ? { q: s.q.trim() } : {},
  loaderDeps: ({ search }) => ({ q: search.q ?? '' }),
  loader: async ({ context, deps }) => {
    if (!deps.q) return ''
    await Promise.all([
      context.queryClient.ensureQueryData(searchCommunitiesOptions({ query: { q: deps.q } })).catch(() => undefined),
      context.queryClient.ensureQueryData(searchPostsOptions({ query: { q: deps.q } })).catch(() => undefined),
      context.queryClient.ensureQueryData(searchCommentsOptions({ query: { q: deps.q } })).catch(() => undefined),
    ])
    return deps.q
  },
  head: ({ loaderData }) => ({
    meta: [{ title: loaderData ? `${loaderData} · Search` : 'Search · Open Communities' }],
  }),
  component: Search,
})

function Search() {
  const { q } = Route.useSearch()
  return (
    <Page>
      <h1 className="font-display text-4xl tracking-tight">Search</h1>
      <search className="max-w-(--container-measure)">
        <form action="/search" method="get" className="flex gap-2">
          <label htmlFor="q" className="sr-only">
            Search communities, posts and comments
          </label>
          <Input id="q" name="q" type="search" defaultValue={q ?? ''} placeholder="Words to find" maxLength={200} />
          <button
            type="submit"
            className="inline-flex h-9 shrink-0 items-center rounded-pill bg-mark px-4 font-sans text-sm font-semibold text-mark-ink"
          >
            Search
          </button>
        </form>
      </search>
      {q ? (
        <Results q={q} />
      ) : (
        <p className="max-w-(--container-measure) font-serif text-sm text-ink-soft">
          Find communities on this site, in the directory, or followed by someone here, and posts and comments in them.
          Put words in quotes to match a phrase, and a minus sign before a word to leave it out.
        </p>
      )}
    </Page>
  )
}

function Results({ q }: { q: string }) {
  // A search is cheap to repeat; the loader and these hooks share one cache entry.
  const { data: me } = useQuery({ ...meOptions(), retry: false })
  const communities = useSuspenseQuery(searchCommunitiesOptions({ query: { q } }))
  const posts = useQuery({ ...searchPostsOptions({ query: { q } }), retry: false })
  const comments = useQuery({ ...searchCommentsOptions({ query: { q } }), retry: false })
  const none =
    communities.data.communities.length === 0 &&
    (posts.data?.posts.length ?? 0) === 0 &&
    (comments.data?.comments.length ?? 0) === 0

  return (
    <div className="flex flex-col gap-10">
      <section aria-labelledby="found-communities" className="flex flex-col gap-4">
        <h2
          id="found-communities"
          className="border-b border-rule-strong pb-1 font-sans text-xs font-bold uppercase tracking-kicker"
        >
          Communities
        </h2>
        {communities.data.communities.length === 0 ? (
          <p className="font-serif text-sm text-ink-soft">No communities match.</p>
        ) : (
          <div className="grid gap-x-8 gap-y-10 sm:grid-cols-2 lg:grid-cols-3">
            {communities.data.communities.map((c: Summary) => (
              <CommunityCard key={c.apId} community={c} signedIn={!!me} />
            ))}
          </div>
        )}
      </section>
      <section aria-labelledby="found-posts" className="flex max-w-(--container-measure) flex-col gap-4">
        <h2
          id="found-posts"
          className="border-b border-rule-strong pb-1 font-sans text-xs font-bold uppercase tracking-kicker"
        >
          Posts
        </h2>
        <Result query={posts} empty="No posts match.">
          {(d) => d.posts.map((p: (typeof d.posts)[number]) => <PostItem key={p.apId} post={p} />)}
        </Result>
      </section>
      <section aria-labelledby="found-comments" className="flex max-w-(--container-measure) flex-col gap-4">
        <h2
          id="found-comments"
          className="border-b border-rule-strong pb-1 font-sans text-xs font-bold uppercase tracking-kicker"
        >
          Comments
        </h2>
        <Result query={comments} empty="No comments match.">
          {(d) =>
            d.comments.map((c: (typeof d.comments)[number]) => (
              <CommentItem
                key={c.comment.apId}
                comment={c.comment}
                post={{ ref: c.postRef, title: c.postTitle, community: c.community }}
              />
            ))
          }
        </Result>
      </section>
      {none && (
        <p className="font-serif text-sm text-ink-soft">
          Nothing matched. Try fewer words, or{' '}
          <Link to="/discover" className="font-sans font-semibold text-mark">
            browse Discover
          </Link>
          .
        </p>
      )}
    </div>
  )
}

// Result shows the three states of one list: failed, empty, and its items.
function Result<T>({
  query,
  empty,
  children,
}: {
  query: { data?: T; isError: boolean }
  empty: string
  children: (data: T) => React.ReactNode
}) {
  if (query.isError) {
    return (
      <p role="alert" className="font-sans text-sm text-danger">
        This part of the search did not run. Reload the page to try again.
      </p>
    )
  }
  if (!query.data) return <p className="font-sans text-sm text-ink-faint">Searching…</p>
  const rendered = children(query.data)
  if (Array.isArray(rendered) ? rendered.length === 0 : !rendered) {
    return <p className="font-serif text-sm text-ink-soft">{empty}</p>
  }
  return <div className="flex flex-col">{rendered}</div>
}

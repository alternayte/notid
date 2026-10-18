import { useInfiniteQuery, useQuery, useSuspenseQuery } from '@tanstack/react-query'
import { createFileRoute, Link, notFound } from '@tanstack/react-router'
import {
  meOptions,
  personCommentsInfiniteOptions,
  personOptions,
  personPostsInfiniteOptions,
} from '~/api/gen/@tanstack/react-query.gen'
import { Age } from '~/discussions/Age'
import { CommentItem } from '~/discussions/CommentItem'
import { PostItem } from '~/discussions/PostItem'
import { Kicker } from '~/ui/Kicker'
import { BotLabel } from '~/ui/PostCard'
import { Button } from '~/ui/primitives/button'
import { fromRenderer, Rendered } from '~/ui/RenderedHtml'
import { Page } from './__root'

type Tab = 'posts' | 'comments'

const paged = { initialPageParam: '', getNextPageParam: (last: { next: string }) => last.next || undefined }
const postsOptions = (handle: string) => ({ ...personPostsInfiniteOptions({ path: { handle } }), ...paged })
const commentsOptions = (handle: string) => ({ ...personCommentsInfiniteOptions({ path: { handle } }), ...paged })

export const Route = createFileRoute('/u/$handle')({
  validateSearch: (s: Record<string, unknown>): { tab?: Tab } => (s.tab === 'comments' ? { tab: 'comments' } : {}),
  loaderDeps: ({ search }) => ({ tab: search.tab ?? 'posts' }),
  loader: async ({ context, params, deps }) => {
    const p = await context.queryClient
      .ensureQueryData(personOptions({ path: { handle: params.handle } }))
      .catch(() => {
        throw notFound()
      })
    if (deps.tab === 'comments') {
      await context.queryClient.prefetchInfiniteQuery(commentsOptions(params.handle))
    } else {
      await context.queryClient.prefetchInfiniteQuery(postsOptions(params.handle))
    }
    return p.displayName || p.name
  },
  head: ({ loaderData }) => ({ meta: [{ title: `${loaderData ?? 'Person'} · Open Communities` }] }),
  component: Profile,
})

const tabLink =
  'font-sans text-xs font-semibold text-ink-soft no-underline hover:text-ink data-[status=active]:text-ink data-[status=active]:underline data-[status=active]:decoration-mark data-[status=active]:decoration-2 data-[status=active]:underline-offset-[0.4em]'

function Profile() {
  const { handle } = Route.useParams()
  const tab = Route.useSearch().tab ?? 'posts'
  const { data: p } = useSuspenseQuery(personOptions({ path: { handle } }))
  const { data: me } = useQuery({ ...meOptions(), retry: false })

  return (
    <Page>
      <header className="flex max-w-(--container-measure) flex-col gap-3 border-b border-rule pb-6">
        <Kicker>{p.local ? 'Person' : `Person on ${p.domain}`}</Kicker>
        <h1 className="font-display text-4xl tracking-tight text-balance">
          {p.displayName || p.name}
          {p.bot && <BotLabel />}
        </h1>
        <p className="font-sans text-xs text-ink-faint">
          @{p.name}@{p.domain} · {p.local ? 'joined' : 'first seen'} <Age iso={p.joined} />
        </p>
        {p.bot && (
          <p className="font-sans text-sm text-ink-soft">
            This is an automated account. Its posts and comments carry a Bot label.
          </p>
        )}
        {p.bioHtml && <Rendered html={fromRenderer(p.bioHtml)} className="prose-news" />}
        {me?.handle === p.handle && (
          <Link to="/settings" className="w-fit font-sans text-sm font-semibold text-mark">
            Edit your profile
          </Link>
        )}
      </header>
      <section aria-labelledby="activity" className="flex max-w-(--container-measure) flex-col gap-4">
        <div className="flex items-baseline justify-between gap-4 border-b border-rule-strong pb-1">
          <h2 id="activity" className="font-sans text-xs font-bold uppercase tracking-kicker">
            Activity
          </h2>
          <nav aria-label="Activity" className="flex gap-4">
            <Link to="/u/$handle" params={{ handle }} search={{}} activeOptions={{ exact: true }} className={tabLink}>
              Posts
            </Link>
            <Link
              to="/u/$handle"
              params={{ handle }}
              search={{ tab: 'comments' }}
              activeOptions={{ exact: true }}
              className={tabLink}
            >
              Comments
            </Link>
          </nav>
        </div>
        {tab === 'comments' ? <Comments handle={handle} /> : <Posts handle={handle} />}
      </section>
    </Page>
  )
}

function Posts({ handle }: { handle: string }) {
  const q = useInfiniteQuery(postsOptions(handle))
  const posts = q.data?.pages.flatMap((p) => p.posts) ?? []
  return (
    <List
      empty="No posts yet."
      count={posts.length}
      loaded={!!q.data}
      failed={q.isError && !q.data}
      more={
        q.hasNextPage
          ? { busy: q.isFetchingNextPage, failed: q.isFetchNextPageError, load: () => q.fetchNextPage() }
          : null
      }
    >
      {posts.map((p) => (
        <PostItem key={p.apId} post={p} />
      ))}
    </List>
  )
}

function Comments({ handle }: { handle: string }) {
  const q = useInfiniteQuery(commentsOptions(handle))
  const comments = q.data?.pages.flatMap((p) => p.comments) ?? []
  return (
    <List
      empty="No comments yet."
      count={comments.length}
      loaded={!!q.data}
      failed={q.isError && !q.data}
      more={
        q.hasNextPage
          ? { busy: q.isFetchingNextPage, failed: q.isFetchNextPageError, load: () => q.fetchNextPage() }
          : null
      }
    >
      {comments.map((c) => (
        <CommentItem
          key={c.comment.apId}
          comment={c.comment}
          post={{ ref: c.postRef, title: c.postTitle, community: c.community }}
        />
      ))}
    </List>
  )
}

function List({
  empty,
  count,
  loaded,
  failed,
  more,
  children,
}: {
  empty: string
  count: number
  loaded: boolean
  failed: boolean
  more: { busy: boolean; failed: boolean; load: () => void } | null
  children: React.ReactNode
}) {
  if (failed) {
    return (
      <p role="alert" className="font-sans text-sm text-danger">
        This did not load. Reload the page to try again.
      </p>
    )
  }
  if (loaded && count === 0) return <p className="font-serif text-sm text-ink-soft">{empty}</p>
  return (
    <div className="flex flex-col">
      {children}
      {more && (
        <div className="flex items-center gap-3 border-t border-rule pt-4">
          <Button variant="outline" disabled={more.busy} onClick={more.load}>
            {more.busy ? 'Loading…' : 'Older'}
          </Button>
          {more.failed && (
            <span role="alert" className="font-sans text-sm text-danger">
              Older items did not load. Try again.
            </span>
          )}
        </div>
      )}
    </div>
  )
}

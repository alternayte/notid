import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { createFileRoute, Link } from '@tanstack/react-router'
import { homeFeedInfiniteOptions, meOptions, myCommunitiesOptions } from '~/api/gen/@tanstack/react-query.gen'
import type { Summary } from '~/api/gen/types.gen'
import { PostItem } from '~/discussions/PostItem'
import { Kicker } from '~/ui/Kicker'
import { Button } from '~/ui/primitives/button'
import { Page } from './__root'

export const Route = createFileRoute('/')({
  // A signed-out visitor gets 401 from both; the page then shows the sign-up call to action.
  loader: ({ context }) =>
    Promise.all([
      context.queryClient.prefetchQuery({ ...myCommunitiesOptions(), retry: false }),
      context.queryClient.prefetchInfiniteQuery(feedOptions()),
    ]),
  component: Home,
})

const feedOptions = () => ({
  ...homeFeedInfiniteOptions(),
  initialPageParam: '',
  getNextPageParam: (last: { next: string }) => last.next || undefined,
  retry: false,
})

function Home() {
  const { data: me } = useQuery({ ...meOptions(), retry: false })
  const { data: mine } = useQuery({ ...myCommunitiesOptions(), retry: false, enabled: !!me })

  if (!me) {
    return (
      <Page>
        <section className="flex max-w-(--container-measure) flex-col gap-4 py-8">
          <h1 className="font-display text-4xl tracking-tight text-balance">
            Communities you can join from one place, and that can leave with their members.
          </h1>
          <p className="font-serif text-lg text-ink-soft">
            Follow communities here, on Lemmy and on PieFed, and read them in one feed, newest first.
          </p>
          <div className="flex flex-wrap items-center gap-5">
            <Link
              to="/login"
              className="inline-flex h-9 items-center rounded-pill bg-mark px-4 font-sans text-sm font-semibold text-mark-ink no-underline"
            >
              Sign in or create an account
            </Link>
            <Link to="/discover" className="font-sans text-sm font-semibold text-mark">
              Browse communities
            </Link>
          </div>
        </section>
      </Page>
    )
  }

  const communities = (mine?.communities ?? []).filter((c) => c.membership?.state === 'accepted')
  return (
    <Page>
      <div className="grid grid-cols-[minmax(0,1fr)] gap-12 lg:grid-cols-[minmax(0,1fr)_18rem]">
        <section aria-labelledby="feed" className="flex flex-col gap-4">
          <h1
            id="feed"
            className="border-b border-rule-strong pb-1 font-sans text-xs font-bold uppercase tracking-kicker"
          >
            Home · New
          </h1>
          <Feed hasCommunities={communities.length > 0} />
        </section>
        <MyCommunities communities={communities} />
      </div>
    </Page>
  )
}

function Feed({ hasCommunities }: { hasCommunities: boolean }) {
  const feed = useInfiniteQuery(feedOptions())
  if (feed.isError && !feed.data) {
    return (
      <p role="alert" className="font-sans text-sm text-danger">
        Your feed did not load. Reload the page to try again.
      </p>
    )
  }
  const posts = feed.data?.pages.flatMap((p) => p.posts) ?? []
  if (feed.data && posts.length === 0) {
    return (
      <section className="flex max-w-(--container-measure) flex-col gap-2 rounded-sm bg-paper-sunk p-4">
        <h2 className="font-display text-lg tracking-tight">Nothing here yet</h2>
        <p className="font-serif text-sm text-ink-soft">
          {hasCommunities
            ? 'No one has posted in your communities yet. Start the first conversation.'
            : 'Your home feed fills with posts from the communities you join, newest first.'}
        </p>
        <Link to={hasCommunities ? '/submit' : '/discover'} className="w-fit font-sans text-sm font-semibold text-mark">
          {hasCommunities ? 'Write a post' : 'Find communities'}
        </Link>
      </section>
    )
  }
  return (
    <div className="flex flex-col">
      {posts.map((p) => (
        <PostItem key={p.apId} post={p} />
      ))}
      {feed.hasNextPage && (
        <div className="flex items-center gap-3 border-t border-rule pt-4">
          <Button variant="outline" disabled={feed.isFetchingNextPage} onClick={() => feed.fetchNextPage()}>
            {feed.isFetchingNextPage ? 'Loading…' : 'Older posts'}
          </Button>
          {feed.isFetchNextPageError && (
            <span role="alert" className="font-sans text-sm text-danger">
              Older posts did not load. Try again.
            </span>
          )}
        </div>
      )}
    </div>
  )
}

function MyCommunities({ communities }: { communities: Summary[] }) {
  return (
    <aside aria-label="Your communities" className="flex flex-col gap-3 border-t-2 border-mark pt-3 lg:self-start">
      <Kicker>Your communities</Kicker>
      {communities.length === 0 ? (
        <p className="font-serif text-sm text-ink-faint">You have not joined a community yet.</p>
      ) : (
        <ul className="flex flex-col gap-2 font-sans text-sm">
          {communities.map((c) => (
            <li key={c.apId}>
              <Link
                to="/c/$handle"
                params={{ handle: c.handle }}
                className="font-semibold text-ink-soft hover:text-mark"
              >
                {c.title || c.name}
              </Link>
              {!c.local && <span className="text-ink-faint"> · {c.domain}</span>}
            </li>
          ))}
        </ul>
      )}
      <Link to="/discover" className="font-sans text-sm font-semibold text-mark">
        Discover more
      </Link>
    </aside>
  )
}

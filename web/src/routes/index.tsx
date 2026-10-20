import { useInfiniteQuery, useQuery, useQueryClient } from '@tanstack/react-query'
import { createFileRoute, Link } from '@tanstack/react-router'
import { useEffect, useRef } from 'react'
import {
  allFeedInfiniteOptions,
  homeFeedInfiniteOptions,
  homeFeedInfiniteQueryKey,
  meOptions,
  myCommunitiesOptions,
  popularFeedInfiniteOptions,
} from '~/api/gen/@tanstack/react-query.gen'
import type { Summary } from '~/api/gen/types.gen'
import { PostItem } from '~/discussions/PostItem'
import { Kicker } from '~/ui/Kicker'
import { Button } from '~/ui/primitives/button'
import { Page } from './__root'

type FeedName = 'home' | 'popular' | 'all'

const paged = {
  initialPageParam: '',
  getNextPageParam: (last: { next: string }) => last.next || undefined,
  retry: false,
}
const feeds = {
  home: () => ({ ...homeFeedInfiniteOptions(), ...paged }),
  popular: () => ({ ...popularFeedInfiniteOptions(), ...paged }),
  all: () => ({ ...allFeedInfiniteOptions(), ...paged }),
}

// The front page opens on Home for a person with an accepted join, and on Popular for everyone else.
function shownFeed(asked: FeedName | undefined, mine: Summary[] | undefined): FeedName {
  const joined = (mine ?? []).some((c) => c.membership?.state === 'accepted')
  if (asked === 'home') return mine ? 'home' : 'popular'
  return asked ?? (joined ? 'home' : 'popular')
}

export const Route = createFileRoute('/')({
  validateSearch: (s: Record<string, unknown>): { feed?: FeedName } =>
    s.feed === 'home' || s.feed === 'popular' || s.feed === 'all' ? { feed: s.feed } : {},
  loaderDeps: ({ search }) => ({ feed: search.feed }),
  loader: async ({ context, deps }) => {
    // A signed-out visitor gets 401 here and sees Popular.
    const mine = await context.queryClient
      .fetchQuery({ ...myCommunitiesOptions(), retry: false })
      .catch(() => undefined)
    await context.queryClient.prefetchInfiniteQuery(feeds[shownFeed(deps.feed, mine?.communities)]())
  },
  component: Home,
})

const tabLink =
  'font-sans text-xs font-bold uppercase tracking-kicker no-underline transition-colors duration-(--dur-press) hover:text-ink'

function Home() {
  const asked = Route.useSearch().feed
  const { data: me } = useQuery({ ...meOptions(), retry: false })
  const { data: mine } = useQuery({
    ...myCommunitiesOptions(),
    retry: false,
    enabled: !!me,
    // A join that another server has not accepted yet is read again until it is.
    refetchInterval: (q) => (q.state.data?.communities.some((c) => c.membership?.state === 'pending') ? 4000 : false),
  })
  const feed = shownFeed(asked, me ? mine?.communities : undefined)
  const joined = (mine?.communities ?? []).filter((c) => !!c.membership)
  const communities = joined.filter((c) => c.membership?.state === 'accepted')
  const tabs: [FeedName, string][] = me
    ? [
        ['home', 'Home'],
        ['popular', 'Popular'],
        ['all', 'All'],
      ]
    : [
        ['popular', 'Popular'],
        ['all', 'All'],
      ]

  return (
    <Page>
      {!me && <Welcome />}
      <div className="grid grid-cols-[minmax(0,1fr)] gap-12 lg:grid-cols-[minmax(0,1fr)_18rem]">
        <section aria-labelledby="feed" className="flex flex-col gap-4">
          <div className="flex items-baseline justify-between gap-4 border-b border-rule-strong pb-1">
            <h2 id="feed" className="sr-only">
              {tabs.find(([n]) => n === feed)?.[1]}
            </h2>
            <nav aria-label="Feeds" className="flex gap-5">
              {tabs.map(([name, label]) => (
                <Link
                  key={name}
                  to="/"
                  search={{ feed: name }}
                  aria-current={name === feed ? 'page' : undefined}
                  className={`${tabLink} ${name === feed ? 'text-ink underline decoration-mark decoration-2 underline-offset-[0.4em]' : 'text-ink-faint'}`}
                >
                  {label}
                </Link>
              ))}
            </nav>
            <Link to="/about/feeds" className="font-sans text-xs text-ink-faint no-underline hover:text-ink">
              How feeds work
            </Link>
          </div>
          {feed === 'home' ? <HomeFeed joined={communities.length} /> : <Feed name={feed} />}
        </section>
        {me ? <MyCommunities communities={joined} /> : <JoinIn />}
      </div>
    </Page>
  )
}

function Welcome() {
  return (
    <section className="flex max-w-(--container-measure) flex-col gap-3 pb-2">
      <h1 className="font-display text-3xl tracking-tight text-balance">
        Communities you can join from one place, and that can leave with their members.
      </h1>
      <p className="font-serif text-base text-ink-soft">
        Read communities here, on Lemmy and on PieFed. Sign in to join them and get your own home feed.
      </p>
    </section>
  )
}

function JoinIn() {
  return (
    <aside aria-label="Join in" className="flex flex-col gap-3 border-t-2 border-mark pt-3 lg:self-start">
      <Kicker>Join in</Kicker>
      <p className="font-serif text-sm text-ink-soft">
        An account lets you join communities, vote, comment and post. Your home feed then shows the communities you
        join, newest first.
      </p>
      <Link
        to="/login"
        className="inline-flex h-9 w-fit items-center rounded-pill bg-mark px-4 font-sans text-sm font-semibold text-mark-ink no-underline"
      >
        Sign in or create an account
      </Link>
      <Link to="/discover" className="font-sans text-sm font-semibold text-mark">
        Browse communities
      </Link>
    </aside>
  )
}

function HomeFeed({ joined }: { joined: number }) {
  const queryClient = useQueryClient()
  const seen = useRef(joined)
  useEffect(() => {
    // A join that was just accepted adds a community, so the feed is read again.
    if (joined > seen.current) void queryClient.invalidateQueries({ queryKey: homeFeedInfiniteQueryKey() })
    seen.current = joined
  }, [joined, queryClient])
  return <Feed name="home" joined={joined} />
}

const empty: Record<FeedName, string> = {
  home: 'Your home feed fills with posts from the communities you join, newest first.',
  popular: 'No posts from the last week yet. Popular fills as communities here and in the directory post.',
  all: 'No posts yet. All fills as communities here and in the directory post.',
}

function Feed({ name, joined = 0 }: { name: FeedName; joined?: number }) {
  const feed = useInfiniteQuery(feeds[name]())
  const hasCommunities = joined > 0
  if (feed.isError && !feed.data) {
    return (
      <p role="alert" className="font-sans text-sm text-danger">
        This feed did not load. Reload the page to try again.
      </p>
    )
  }
  const posts = feed.data?.pages.flatMap((p) => p.posts) ?? []
  if (feed.data && posts.length === 0) {
    return (
      <section className="flex max-w-(--container-measure) flex-col gap-2 rounded-sm bg-paper-sunk p-4">
        <h2 className="font-display text-lg tracking-tight">Nothing here yet</h2>
        <p className="font-serif text-sm text-ink-soft">
          {name === 'home' && hasCommunities
            ? 'No one has posted in your communities yet. Start the first conversation.'
            : empty[name]}
        </p>
        {name === 'home' && (
          <Link
            to={hasCommunities ? '/submit' : '/discover'}
            className="w-fit font-sans text-sm font-semibold text-mark"
          >
            {hasCommunities ? 'Write a post' : 'Find communities'}
          </Link>
        )}
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
              {c.membership?.state === 'pending' && <span className="text-ink-faint"> · waiting to be accepted</span>}
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

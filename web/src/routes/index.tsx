import { useQuery } from '@tanstack/react-query'
import { createFileRoute, Link } from '@tanstack/react-router'
import { meOptions, myCommunitiesOptions } from '~/api/gen/@tanstack/react-query.gen'
import { CommunityCard } from '~/communities/CommunityCard'
import { Page } from './__root'

export const Route = createFileRoute('/')({
  // A signed-out visitor gets 401 from both; the page then shows the sign-up call to action.
  loader: ({ context }) => context.queryClient.prefetchQuery({ ...myCommunitiesOptions(), retry: false }),
  component: Home,
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

  const communities = mine?.communities ?? []
  return (
    <Page>
      <h1 className="font-display text-4xl tracking-tight">Your communities</h1>
      {communities.length === 0 ? (
        <section className="flex max-w-(--container-measure) flex-col gap-2 rounded-sm bg-paper-sunk p-4">
          <h2 className="font-display text-lg tracking-tight">Nothing here yet</h2>
          <p className="font-serif text-sm text-ink-soft">
            Your home feed fills with posts from the communities you join, newest first.
          </p>
          <Link to="/discover" className="w-fit font-sans text-sm font-semibold text-mark">
            Find communities
          </Link>
        </section>
      ) : (
        <div className="grid gap-x-8 gap-y-10 sm:grid-cols-2 lg:grid-cols-3">
          {communities.map((c) => (
            <CommunityCard key={c.apId} community={c} signedIn />
          ))}
        </div>
      )}
    </Page>
  )
}

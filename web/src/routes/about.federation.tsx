import { useQuery } from '@tanstack/react-query'
import { createFileRoute, Link } from '@tanstack/react-router'
import { aboutOptions } from '~/api/gen/@tanstack/react-query.gen'
import { Page } from './__root'

export const Route = createFileRoute('/about/federation')({
  loader: ({ context }) => context.queryClient.prefetchQuery({ ...aboutOptions(), retry: false }),
  head: () => ({ meta: [{ title: 'How this site federates · Open Communities' }] }),
  component: Federation,
})

const term = 'font-display text-lg tracking-tight'
const body = 'font-serif text-base text-ink-soft'

function Federation() {
  const limits = useQuery({ ...aboutOptions(), retry: false }).data?.limits
  return (
    <Page>
      <div className="flex max-w-(--container-measure) flex-col gap-2">
        <p className="font-sans text-xs font-bold uppercase tracking-kicker text-ink-faint">About</p>
        <h1 className="font-display text-4xl tracking-tight">How this site federates</h1>
        <p className="font-serif text-base text-ink-soft">
          Open Communities speaks ActivityPub. People on other servers take part here, and people here take part on
          other servers.
        </p>
      </div>
      <div className="flex max-w-(--container-measure) flex-col gap-6">
        <section className="flex flex-col gap-1 border-t border-rule pt-4">
          <h2 className={term}>Open, with a blocklist</h2>
          <p className={body}>
            Any server can talk to this one, unless an admin blocks it. A block stops its activities at the inbox and
            stops deliveries to it.
          </p>
        </section>
        <section className="flex flex-col gap-1 border-t border-rule pt-4">
          <h2 className={term}>Discover and the directory</h2>
          <p className={body}>
            Discover shows the communities on this site, and a reviewed list of communities elsewhere that an admin
            added. You can still reach any other community by pasting its address or handle. Content from a server that
            no one here follows stays out of Discover and out of search.
          </p>
        </section>
        <section className="flex flex-col gap-1 border-t border-rule pt-4">
          <h2 className={term}>Media stays at its source</h2>
          <p className={body}>
            Images from other servers are not stored here. This site fetches them through its own proxy, which refuses
            private addresses, so your browser never talks to that server directly.
          </p>
        </section>
        <section className="flex flex-col gap-1 border-t border-rule pt-4">
          <h2 className={term}>Who sees what you do</h2>
          <p className={body}>
            Your posts, comments and votes go to the server that hosts the community. Its admins can see who voted and
            who reported, as this site's admins can for content written here. Moderation decisions and the public log
            travel with the content.
          </p>
        </section>
        {limits && (
          <section className="flex flex-col gap-1 border-t border-rule pt-4">
            <h2 className={term}>Limits for new accounts</h2>
            <p className={body}>
              A new account can write {limits.writesPerDay} posts and comments a day, and only{' '}
              {limits.remoteWritesPerDay} of them into communities on other servers. Its votes stay on this site and do
              not count yet. An account becomes trusted after {Math.round(limits.trustAfterHours / 24)} days and{' '}
              {limits.trustAfterWrites} posts or comments that no moderator removed, with no upheld report against them.
            </p>
          </section>
        )}
        <section className="flex flex-col gap-1 border-t border-rule pt-4">
          <h2 className={term}>Communities can move</h2>
          <p className={body}>
            A community that owns its domain can move to another installation and keep its address and its members. The
            old installation marks itself departed and stops answering for the community.
          </p>
        </section>
        <p className="font-sans text-sm text-ink-faint">
          <Link to="/about/feeds" className="font-semibold text-mark">
            How the feeds work
          </Link>{' '}
          ·{' '}
          <Link to="/about/moderation" className="font-semibold text-mark">
            How moderation works
          </Link>
        </p>
      </div>
    </Page>
  )
}

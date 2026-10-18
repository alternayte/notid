import { useQuery } from '@tanstack/react-query'
import { createFileRoute, Link } from '@tanstack/react-router'
import { aboutOptions } from '~/api/gen/@tanstack/react-query.gen'
import { Page } from './__root'

export const Route = createFileRoute('/about/moderation')({
  loader: ({ context }) => context.queryClient.prefetchQuery({ ...aboutOptions(), retry: false }),
  head: () => ({ meta: [{ title: 'How moderation works · Open Communities' }] }),
  component: Moderation,
})

const term = 'font-display text-lg tracking-tight'
const body = 'font-serif text-base text-ink-soft'

function Moderation() {
  const { data } = useQuery({ ...aboutOptions(), retry: false })
  return (
    <Page>
      <div className="flex max-w-(--container-measure) flex-col gap-2">
        <p className="font-sans text-xs font-bold uppercase tracking-kicker text-ink-faint">About</p>
        <h1 className="font-display text-4xl tracking-tight">How moderation works</h1>
        <p className="font-serif text-base text-ink-soft">
          Each community has its own owners and moderators. Every action they take appears in its public moderation log.
        </p>
      </div>
      <div className="flex max-w-(--container-measure) flex-col gap-6">
        <section className="flex flex-col gap-1 border-t border-rule pt-4">
          <h2 className={term}>What moderators can do</h2>
          <p className={body}>
            Remove a post or a comment and restore it. Lock a post so no one replies. Ban a person from the community,
            for a number of days or until a moderator lifts it. A removed post shows a notice to its author, with the
            community's appeals contact, instead of its body.
          </p>
        </section>
        <section className="flex flex-col gap-1 border-t border-rule pt-4">
          <h2 className={term}>Reports</h2>
          <p className={body}>
            Anyone signed in can report a post or a comment. Reports go to the community's moderators and stay private;
            the reporter is named only to them. A site admin sees reports on content written on this site.
          </p>
        </section>
        <section className="flex flex-col gap-1 border-t border-rule pt-4">
          <h2 className={term}>The moderation log</h2>
          <p className={body}>
            Every community page shows its public log: each removal, restoration, lock, ban and report decision, with
            the reason and the moderator. A community's site-wide bans are listed here as counts, not in its log.
          </p>
        </section>
        <section className="flex flex-col gap-2 border-t border-rule pt-4">
          <h2 className={term}>Counts</h2>
          {data ? (
            <table className="w-full border-collapse font-sans text-sm">
              <thead>
                <tr className="border-b border-rule text-left text-xs uppercase tracking-kicker text-ink-faint">
                  <th className="py-2 font-bold">Action</th>
                  <th className="py-2 text-right font-bold">All time</th>
                  <th className="py-2 text-right font-bold">Last 30 days</th>
                </tr>
              </thead>
              <tbody>
                {[
                  ['Posts removed', data.allTime.postsRemoved, data.recent.postsRemoved],
                  ['Comments removed', data.allTime.commentsRemoved, data.recent.commentsRemoved],
                  ['Posts locked', data.allTime.postsLocked, data.recent.postsLocked],
                  ['Bans', data.allTime.bans, data.recent.bans],
                  ['Reports filed', data.allTime.reportsFiled, data.recent.reportsFiled],
                  ['Reports upheld', data.allTime.reportsUpheld, data.recent.reportsUpheld],
                  ['Reports dismissed', data.allTime.reportsDismissed, data.recent.reportsDismissed],
                ].map(([name, all, recent]) => (
                  <tr key={String(name)} className="border-b border-rule">
                    <td className="py-2">{name}</td>
                    <td className="py-2 text-right tabular-nums">{all}</td>
                    <td className="py-2 text-right tabular-nums">{recent}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          ) : (
            <p className="font-sans text-sm text-ink-faint">The counts did not load. Reload the page to try again.</p>
          )}
        </section>
        <p className="font-sans text-sm text-ink-faint">
          <Link to="/about/feeds" className="font-semibold text-mark">
            How the feeds work
          </Link>{' '}
          ·{' '}
          <Link to="/about/federation" className="font-semibold text-mark">
            How this site federates
          </Link>
        </p>
      </div>
    </Page>
  )
}

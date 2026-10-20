import { createFileRoute, Link } from '@tanstack/react-router'
import { Page } from './__root'

export const Route = createFileRoute('/about/feeds')({
  head: () => ({ meta: [{ title: 'How the feeds work · Open Communities' }] }),
  component: Feeds,
})

const term = 'font-display text-lg tracking-tight'
const body = 'font-serif text-base text-ink-soft'

function Feeds() {
  return (
    <Page>
      <div className="flex max-w-(--container-measure) flex-col gap-2">
        <p className="font-sans text-xs font-bold uppercase tracking-kicker text-ink-faint">About</p>
        <h1 className="font-display text-4xl tracking-tight">How the feeds work</h1>
        <p className="font-serif text-base text-ink-soft">
          There is no hidden ranking. Every list uses one of the formulas below, word for word.
        </p>
      </div>
      <div className="flex max-w-(--container-measure) flex-col gap-6">
        <section className="flex flex-col gap-1 border-t border-rule pt-4">
          <h2 className={term}>Home</h2>
          <p className={body}>
            Posts from the communities you follow, newest first. Nothing else appears in it, and it has no second sort.
          </p>
        </section>
        <section className="flex flex-col gap-1 border-t border-rule pt-4">
          <h2 className={term}>Popular</h2>
          <p className={body}>
            Posts from the last week in every community this site receives: communities that live here, communities in
            the site's directory, and communities someone here joined. Each post gets a rank, log10(max(1, 3 + score))
            divided by (hours since it arrived + 2) to the power 1.8, and the highest rank comes first. The ranks are
            fixed when you open the first page, so the order does not shift while you read. NSFW posts and posts in NSFW
            communities stay out.
          </p>
        </section>
        <section className="flex flex-col gap-1 border-t border-rule pt-4">
          <h2 className={term}>All</h2>
          <p className={body}>
            The same posts as Popular, from any time, newest first by the time the post arrived on this site. NSFW posts
            and posts in NSFW communities stay out.
          </p>
        </section>
        <section className="flex flex-col gap-1 border-t border-rule pt-4">
          <h2 className={term}>New, in a community</h2>
          <p className={body}>Newest first, by the time the post arrived on this site.</p>
        </section>
        <section className="flex flex-col gap-1 border-t border-rule pt-4">
          <h2 className={term}>Active, in a community</h2>
          <p className={body}>
            Newest first by the latest of the post and its last reply. A reply counts no later than the moment it
            arrived, so a post dated in the future cannot push itself up.
          </p>
        </section>
        <section className="flex flex-col gap-1 border-t border-rule pt-4">
          <h2 className={term}>Top, in a community</h2>
          <p className={body}>
            Highest score first, among posts from the last day, the last week, or all time. A vote from a new account is
            kept but changes no score, and it never reaches another server.
          </p>
        </section>
        <p className="font-sans text-sm text-ink-faint">
          <Link to="/about/moderation" className="font-semibold text-mark">
            How moderation works
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

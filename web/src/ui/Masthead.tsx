import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { meOptions } from '~/api/gen/@tanstack/react-query.gen'

const navLink =
  'font-sans text-sm font-semibold text-ink-soft no-underline transition-colors duration-(--dur-press) hover:text-ink data-[status=active]:text-ink data-[status=active]:underline data-[status=active]:decoration-mark data-[status=active]:decoration-2 data-[status=active]:underline-offset-[0.4em]'

// The nameplate and the section links, set above a heavy rule like a front page.
export function Masthead() {
  const { data: me } = useQuery({ ...meOptions(), retry: false })
  return (
    <header className="border-b-2 border-rule-strong">
      <div className="mx-auto flex max-w-(--container-page) flex-wrap items-baseline gap-x-8 gap-y-2 px-5 pt-5 pb-3 sm:px-8">
        <Link to="/" className="font-display text-xl tracking-tight no-underline">
          Open Communities
        </Link>
        <nav aria-label="Sections" className="flex items-baseline gap-5">
          <Link to="/" className={navLink} activeOptions={{ exact: true }}>
            Home
          </Link>
          <Link to="/discover" className={navLink}>
            Discover
          </Link>
        </nav>
        <div className="ml-auto flex items-baseline gap-5">
          {me ? (
            <>
              <Link to="/communities/new" className={navLink}>
                Start a community
              </Link>
              <span className="font-sans text-sm text-ink-faint">{me.displayName || me.name}</span>
            </>
          ) : (
            <Link to="/login" className={navLink}>
              Sign in
            </Link>
          )}
        </div>
      </div>
    </header>
  )
}

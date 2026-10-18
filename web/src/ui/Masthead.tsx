import { useQuery } from '@tanstack/react-query'
import { Link, useRouterState } from '@tanstack/react-router'
import { Menu, X } from 'lucide-react'
import { useEffect, useId, useState } from 'react'
import { meOptions } from '~/api/gen/@tanstack/react-query.gen'
import type { Me } from '~/api/gen/types.gen'

const navLink =
  'font-sans text-sm font-semibold whitespace-nowrap text-ink-soft no-underline transition-colors duration-(--dur-press) hover:text-ink data-[status=active]:text-ink data-[status=active]:underline data-[status=active]:decoration-mark data-[status=active]:decoration-2 data-[status=active]:underline-offset-[0.4em]'

const menuLink =
  'block border-b border-rule py-3 font-sans text-base font-semibold text-ink-soft no-underline last:border-b-0 hover:text-ink data-[status=active]:text-ink data-[status=active]:underline data-[status=active]:decoration-mark data-[status=active]:decoration-2 data-[status=active]:underline-offset-[0.4em]'

// The nameplate and the section links, set above a heavy rule like a front page. On a phone the account links fold
// into a panel under the rule.
export function Masthead() {
  const { data: me } = useQuery({ ...meOptions(), retry: false })
  const [open, setOpen] = useState(false)
  const panelId = useId()
  const pathname = useRouterState({ select: (s) => s.location.pathname })

  const [openedAt, setOpenedAt] = useState(pathname)
  if (open && openedAt !== pathname) setOpen(false)

  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(false)
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [open])

  return (
    <header className="border-b-2 border-rule-strong">
      <div className="mx-auto flex max-w-(--container-page) flex-wrap items-baseline gap-x-8 gap-y-2 px-5 pt-5 pb-3 sm:px-8">
        <Link to="/" className="font-display text-xl tracking-tight no-underline">
          Open Communities
        </Link>
        <nav aria-label="Sections" className="order-3 flex w-full items-baseline gap-5 sm:order-none sm:w-auto">
          <Link to="/" className={navLink} activeOptions={{ exact: true }}>
            Home
          </Link>
          <Link to="/discover" className={navLink}>
            Discover
          </Link>
          <Link to="/search" className={navLink}>
            Search
          </Link>
        </nav>
        <div className="ml-auto flex items-baseline gap-5">
          {me ? (
            <>
              <div className="hidden items-baseline gap-5 sm:flex">
                <AccountLinks me={me} className={navLink} />
              </div>
              <button
                type="button"
                aria-expanded={open}
                aria-controls={panelId}
                onClick={() => {
                  setOpenedAt(pathname)
                  setOpen(!open)
                }}
                className="-my-2 inline-flex items-center gap-1.5 self-center rounded-pill px-2 py-2 font-sans text-sm font-semibold text-ink-soft hover:text-ink sm:hidden"
              >
                {open ? <X className="size-4" aria-hidden /> : <Menu className="size-4" aria-hidden />}
                Menu
              </button>
            </>
          ) : (
            <Link to="/login" className={navLink}>
              Sign in
            </Link>
          )}
        </div>
      </div>
      {me && open && (
        <nav id={panelId} aria-label="Your account" className="border-t border-rule px-5 pb-2 sm:hidden">
          <AccountLinks me={me} className={menuLink} />
        </nav>
      )}
    </header>
  )
}

function AccountLinks({ me, className }: { me: Me; className: string }) {
  return (
    <>
      <Link to="/submit" className={className}>
        New post
      </Link>
      <Link to="/saved" className={className}>
        Saved
      </Link>
      <Link to="/communities/new" className={className}>
        Start a community
      </Link>
      {me.admin && (
        <Link to="/admin" className={className}>
          Admin
        </Link>
      )}
      <Link to="/u/$handle" params={{ handle: me.handle }} className={className}>
        {me.displayName || me.name}
      </Link>
    </>
  )
}

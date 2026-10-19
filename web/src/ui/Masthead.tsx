import { useMutation, useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { ChevronDown, Menu } from 'lucide-react'
import { DropdownMenu } from 'radix-ui'
import type { ReactNode } from 'react'
import { meOptions, signOutMutation } from '~/api/gen/@tanstack/react-query.gen'
import type { Me } from '~/api/gen/types.gen'

const navLink =
  'font-sans text-sm font-semibold whitespace-nowrap text-ink-soft no-underline transition-colors duration-(--dur-press) hover:text-ink data-[status=active]:text-ink data-[status=active]:underline data-[status=active]:decoration-mark data-[status=active]:decoration-2 data-[status=active]:underline-offset-[0.4em]'

const menuItem =
  'flex w-full cursor-pointer items-center rounded-sm px-3 py-2.5 font-sans text-sm font-semibold text-ink-soft no-underline outline-none select-none data-highlighted:bg-paper-sunk data-highlighted:text-ink data-[status=active]:text-ink'

// The nameplate and the section links, set above a heavy rule like a front page. The account links sit in one menu at
// every width, so the row never wraps between a phone and a desktop.
export function Masthead() {
  const { data: me } = useQuery({ ...meOptions(), retry: false })

  return (
    <header className="border-b-2 border-rule-strong">
      <div className="mx-auto flex max-w-(--container-page) flex-wrap items-baseline gap-x-4 gap-y-2 px-5 pt-5 pb-3 sm:gap-x-8 sm:px-8">
        <Link to="/" className="font-display text-lg tracking-tight whitespace-nowrap no-underline sm:text-xl">
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
              <Link to="/submit" className={`${navLink} hidden sm:inline`}>
                New post
              </Link>
              <AccountMenu me={me} />
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

function AccountMenu({ me }: { me: Me }) {
  const signOut = useMutation(signOutMutation())
  const name = me.displayName || me.name

  return (
    <DropdownMenu.Root modal={false}>
      <DropdownMenu.Trigger className="-my-2 inline-flex max-w-48 items-center gap-1.5 self-center rounded-pill px-2 py-2 font-sans text-sm font-semibold text-ink-soft outline-none hover:text-ink focus-visible:ring-2 focus-visible:ring-mark data-[state=open]:text-ink">
        <Menu className="size-4 sm:hidden" aria-hidden />
        <span className="sm:hidden">Menu</span>
        <span className="hidden truncate sm:inline">{name}</span>
        <ChevronDown className="hidden size-4 sm:block" aria-hidden />
      </DropdownMenu.Trigger>
      <DropdownMenu.Portal>
        <DropdownMenu.Content
          align="end"
          sideOffset={8}
          collisionPadding={16}
          className="z-50 w-72 max-w-[calc(100vw-2rem)] rounded-md border border-rule-strong bg-paper-raised p-1 shadow-lg"
        >
          <DropdownMenu.Label className="border-b border-rule px-3 pt-2 pb-3">
            <span className="block truncate font-sans text-sm font-semibold text-ink">{name}</span>
            <span className="block truncate font-sans text-xs text-ink-faint">{me.email}</span>
          </DropdownMenu.Label>
          <DropdownMenu.Group className="py-1">
            <Item>
              <Link to="/u/$handle" params={{ handle: me.handle }} className={menuItem}>
                Your profile
              </Link>
            </Item>
            <Item>
              <Link to="/submit" className={menuItem}>
                New post
              </Link>
            </Item>
            <Item>
              <Link to="/saved" className={menuItem}>
                Saved
              </Link>
            </Item>
            <Item>
              <Link to="/communities/new" className={menuItem}>
                Start a community
              </Link>
            </Item>
            {me.admin && (
              <Item>
                <Link to="/admin" className={menuItem}>
                  Admin
                </Link>
              </Item>
            )}
            <Item>
              <Link to="/settings" className={menuItem}>
                Settings
              </Link>
            </Item>
          </DropdownMenu.Group>
          <DropdownMenu.Separator className="h-px bg-rule" />
          <DropdownMenu.Item
            className={`${menuItem} mt-1`}
            disabled={signOut.isPending}
            onSelect={(e) => {
              e.preventDefault()
              // A full load drops every cached query of the signed-in user.
              signOut.mutate({}, { onSettled: () => window.location.assign('/') })
            }}
          >
            {signOut.isPending ? 'Signing out…' : 'Sign out'}
          </DropdownMenu.Item>
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
  )
}

// Item makes a link a menu item, so the arrow keys reach it and choosing it closes the menu.
function Item({ children }: { children: ReactNode }) {
  return <DropdownMenu.Item asChild>{children}</DropdownMenu.Item>
}

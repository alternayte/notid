/// <reference types="vite/client" />
import type { QueryClient } from '@tanstack/react-query'
import { createRootRouteWithContext, HeadContent, Link, Outlet, ScriptOnce, Scripts } from '@tanstack/react-router'
import type { ReactNode } from 'react'
import { meOptions } from '~/api/gen/@tanstack/react-query.gen'
import appCss from '~/styles/app.css?url'
import { themeScript } from '~/theme'
import { Masthead } from '~/ui/Masthead'
import { ThemeSwitch } from '~/ui/ThemeSwitch'

export const Route = createRootRouteWithContext<{ queryClient: QueryClient }>()({
  head: () => ({
    meta: [
      { charSet: 'utf-8' },
      { name: 'viewport', content: 'width=device-width, initial-scale=1' },
      { title: 'Open Communities' },
    ],
    links: [{ rel: 'stylesheet', href: appCss }],
  }),
  // A signed-out visitor gets 401; the masthead then shows the sign-in link.
  loader: ({ context }) => context.queryClient.prefetchQuery({ ...meOptions(), retry: false }),
  component: Layout,
  shellComponent: RootDocument,
  notFoundComponent: () => (
    <Page>
      <h1 className="font-display text-3xl tracking-tight">Page not found</h1>
      <Link to="/" className="font-sans text-sm font-semibold text-mark">
        Go to the home feed
      </Link>
    </Page>
  ),
})

// Page is the column every route sits in.
export function Page({ children }: { children: ReactNode }) {
  return <main className="mx-auto flex max-w-(--container-page) flex-col gap-6 px-5 py-8 sm:px-8">{children}</main>
}

function Layout() {
  return (
    <div className="flex min-h-dvh flex-col">
      <Masthead />
      <div className="flex-1">
        <Outlet />
      </div>
      <footer className="border-t border-rule">
        <div className="mx-auto flex max-w-(--container-page) flex-wrap items-center justify-between gap-4 px-5 py-6 font-sans text-xs text-ink-faint sm:px-8">
          <span>Open Communities is free software under the AGPL-3.0.</span>
          <ThemeSwitch />
        </div>
      </footer>
    </div>
  )
}

function RootDocument({ children }: { children: ReactNode }) {
  return (
    <html lang="en" suppressHydrationWarning>
      <head>
        <ScriptOnce>{themeScript}</ScriptOnce>
        <HeadContent />
      </head>
      <body>
        {children}
        <Scripts />
      </body>
    </html>
  )
}

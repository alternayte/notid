/// <reference types="vite/client" />
import type { QueryClient } from '@tanstack/react-query'
import { createRootRouteWithContext, HeadContent, Link, Outlet, Scripts } from '@tanstack/react-router'
import type { ReactNode } from 'react'
import appCss from '~/styles/app.css?url'
import { themeScript } from '~/theme'

export const Route = createRootRouteWithContext<{ queryClient: QueryClient }>()({
  head: () => ({
    meta: [
      { charSet: 'utf-8' },
      { name: 'viewport', content: 'width=device-width, initial-scale=1' },
      { title: 'Open Communities' },
    ],
    links: [{ rel: 'stylesheet', href: appCss }],
  }),
  component: () => <Outlet />,
  shellComponent: RootDocument,
  notFoundComponent: () => (
    <main>
      <h1>Page not found</h1>
      <Link to="/">Go to the home feed</Link>
    </main>
  ),
})

function RootDocument({ children }: { children: ReactNode }) {
  return (
    <html lang="en" suppressHydrationWarning>
      <head>
        {/* biome-ignore lint/security/noDangerouslySetInnerHtml: a fixed inline script that sets the theme before paint */}
        <script dangerouslySetInnerHTML={{ __html: themeScript }} />
        <HeadContent />
      </head>
      <body>
        {children}
        <Scripts />
      </body>
    </html>
  )
}

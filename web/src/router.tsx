import { QueryClient } from '@tanstack/react-query'
import { createRouter } from '@tanstack/react-router'
import { setupRouterSsrQueryIntegration } from '@tanstack/react-router-ssr-query'
import { createIsomorphicFn } from '@tanstack/react-start'
import { getRequestHeader } from '@tanstack/react-start/server'
import { routeTree } from './routeTree.gen'

// The Go edge sends each page's CSP nonce; every script the server writes carries it.
const scriptNonce = createIsomorphicFn()
  .server(() => getRequestHeader('x-notid-nonce'))
  .client(() => undefined)

// Called once per server request and once in the browser, so no cache is shared between visitors.
export function getRouter() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { staleTime: 30_000 } },
  })
  const router = createRouter({
    routeTree,
    context: { queryClient },
    defaultPreload: 'intent',
    defaultPreloadStaleTime: 0,
    scrollRestoration: true,
    ssr: { nonce: scriptNonce() },
  })
  setupRouterSsrQueryIntegration({ router, queryClient })
  return router
}

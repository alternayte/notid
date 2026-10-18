import { createIsomorphicFn } from '@tanstack/react-start'
import { getRequestHeader } from '@tanstack/react-start/server'
import type { CreateClientConfig } from './gen/client.gen'

// The generated query keys contain the client's baseUrl, so the server and the
// browser must use the same one, or no query fetched during SSR is found again
// on hydration. Both use this placeholder origin, and each side's fetch
// rewrites it: the server to the internal listener, the browser to its own origin.
export const apiOrigin = 'https://api.invalid'

const internalApi = () => process.env.INTERNAL_API ?? 'http://localhost:8081'

// retarget sends a generated-client request to origin. Only paths under the
// placeholder origin pass, so the API client can never reach another host. The
// body is buffered: browsers refuse to copy a streamed body into a new Request.
async function retarget(request: Request, origin: string): Promise<Request> {
  const prefix = `${apiOrigin}/api/`
  if (!request.url.startsWith(prefix)) throw new Error(`API client request outside /api: ${request.url}`)
  const body = request.method === 'GET' || request.method === 'HEAD' ? undefined : await request.arrayBuffer()
  return new Request(origin + request.url.slice(apiOrigin.length), {
    method: request.method,
    headers: request.headers,
    body,
    signal: request.signal,
    redirect: request.redirect,
  })
}

const fetchWithVisitor = createIsomorphicFn()
  // On the server, loaders call the internal listener and forward the visitor's cookie for this request only.
  .server((): typeof fetch =>
    forFetch(async (input, init) => {
      const request = await retarget(new Request(input, init), internalApi())
      const cookie = getRequestHeader('cookie')
      if (cookie) request.headers.set('cookie', cookie)
      const forwardedFor = getRequestHeader('x-forwarded-for')
      if (forwardedFor) request.headers.set('x-forwarded-for', forwardedFor)
      return fetch(request)
    }),
  )
  .client((): typeof fetch =>
    forFetch(async (input, init) =>
      fetch(await retarget(new Request(input, init), window.location.origin), { credentials: 'same-origin' }),
    ),
  )

// Bun's fetch type carries a preconnect method; the generated client only calls it as a function.
function forFetch(fn: (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>): typeof fetch {
  return Object.assign(fn, { preconnect: fetch.preconnect })
}

export const createClientConfig: CreateClientConfig = (config) => ({
  ...config,
  baseUrl: apiOrigin,
  fetch: fetchWithVisitor(),
})

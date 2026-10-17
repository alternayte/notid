import { createIsomorphicFn } from '@tanstack/react-start'
import { getRequestHeader } from '@tanstack/react-start/server'
import type { CreateClientConfig } from './gen/client.gen'

// On the server, loaders call the internal listener and forward the visitor's cookie for this request only.
// In the browser, calls go to the same origin.
const baseUrl = createIsomorphicFn()
  .server(() => process.env.INTERNAL_API ?? 'http://localhost:8081')
  .client(() => '')

const fetchWithVisitor = createIsomorphicFn()
  .server((): typeof fetch =>
    forFetch((input, init) => {
      const request = new Request(input, init)
      const cookie = getRequestHeader('cookie')
      if (cookie) request.headers.set('cookie', cookie)
      const forwardedFor = getRequestHeader('x-forwarded-for')
      if (forwardedFor) request.headers.set('x-forwarded-for', forwardedFor)
      return fetch(request)
    }),
  )
  .client((): typeof fetch => forFetch((input, init) => fetch(input, { ...init, credentials: 'same-origin' })))

// Bun's fetch type carries a preconnect method; the generated client only calls it as a function.
function forFetch(fn: (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>): typeof fetch {
  return Object.assign(fn, { preconnect: fetch.preconnect })
}

export const createClientConfig: CreateClientConfig = (config) => ({
  ...config,
  baseUrl: baseUrl(),
  fetch: fetchWithVisitor(),
})

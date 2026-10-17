import { createIsomorphicFn } from '@tanstack/react-start'
import { getRequestHeader } from '@tanstack/react-start/server'
import createClient from 'openapi-fetch'
import type { paths } from './schema'

// On the server, page loaders call the API on the internal listener and forward the visitor's cookie.
// In the browser, calls go to the same origin.
export const api = createIsomorphicFn()
  .server(() =>
    createClient<paths>({
      baseUrl: process.env.INTERNAL_API ?? 'http://localhost:8081',
      headers: forwardedHeaders(),
    }),
  )
  .client(() => createClient<paths>({ baseUrl: '', credentials: 'same-origin' }))

function forwardedHeaders(): Record<string, string> {
  const headers: Record<string, string> = {}
  const cookie = getRequestHeader('cookie')
  if (cookie) headers.cookie = cookie
  const forwardedFor = getRequestHeader('x-forwarded-for')
  if (forwardedFor) headers['x-forwarded-for'] = forwardedFor
  return headers
}

export type ApiError = { title?: string; detail?: string; message?: string }

export function errorMessage(error: unknown, fallback: string): string {
  if (error && typeof error === 'object') {
    const e = error as ApiError
    return e.detail ?? e.message ?? e.title ?? fallback
  }
  return fallback
}

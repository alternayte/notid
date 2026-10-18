import { describe, expect, it } from 'vitest'
import { errorMessage } from './errors'

describe('errorMessage', () => {
  it('reads Huma and auth-all error bodies', () => {
    expect(errorMessage({ title: 'Conflict', detail: 'That name is taken.' }, 'x')).toBe('That name is taken.')
    expect(errorMessage({ error: { code: 'INVALID_TOKEN', message: 'The token is invalid or expired.' } }, 'x')).toBe(
      'The token is invalid or expired.',
    )
  })
  it('names the first invalid field of a Huma validation error', () => {
    const body = {
      detail: 'validation failed',
      errors: [{ message: 'expected length <= 100', location: 'body.title' }],
    }
    expect(errorMessage(body, 'x')).toBe('Check title: expected length <= 100.')
  })
  it('falls back for unknown shapes', () => {
    expect(errorMessage(undefined, 'fallback')).toBe('fallback')
    expect(errorMessage({ error: {} }, 'fallback')).toBe('fallback')
  })
})

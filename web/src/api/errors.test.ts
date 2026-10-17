import { describe, expect, it } from 'vitest'
import { errorMessage } from './errors'

describe('errorMessage', () => {
  it('reads Huma and auth-all error bodies', () => {
    expect(errorMessage({ title: 'Conflict', detail: 'That name is taken.' }, 'x')).toBe('That name is taken.')
    expect(errorMessage({ error: { code: 'INVALID_TOKEN', message: 'The token is invalid or expired.' } }, 'x')).toBe(
      'The token is invalid or expired.',
    )
  })
  it('falls back for unknown shapes', () => {
    expect(errorMessage(undefined, 'fallback')).toBe('fallback')
    expect(errorMessage({ error: {} }, 'fallback')).toBe('fallback')
  })
})

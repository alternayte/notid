// Huma errors carry detail at the top level, and field errors in errors[]; auth-all errors nest a message under error.
export function errorMessage(error: unknown, fallback: string): string {
  if (!error || typeof error !== 'object') return fallback
  const e = error as {
    detail?: unknown
    message?: unknown
    title?: unknown
    error?: { message?: unknown }
    errors?: { message?: unknown; location?: unknown }[]
  }
  const field = Array.isArray(e.errors) ? e.errors[0] : undefined
  if (e.detail === 'validation failed' && field && typeof field.message === 'string') {
    const where = typeof field.location === 'string' ? field.location.replace(/^body\./, '') : ''
    return where ? `Check ${where}: ${field.message}.` : `${field.message}.`
  }
  for (const v of [e.detail, e.error?.message, e.message, e.title]) {
    if (typeof v === 'string' && v !== '') return v
  }
  return fallback
}

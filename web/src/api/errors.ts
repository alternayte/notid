// Huma errors carry detail at the top level; auth-all errors nest a message under error.
export function errorMessage(error: unknown, fallback: string): string {
  if (!error || typeof error !== 'object') return fallback
  const e = error as { detail?: unknown; message?: unknown; title?: unknown; error?: { message?: unknown } }
  for (const v of [e.detail, e.error?.message, e.message, e.title]) {
    if (typeof v === 'string' && v !== '') return v
  }
  return fallback
}

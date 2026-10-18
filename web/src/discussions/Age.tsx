const units: [Intl.RelativeTimeFormatUnit, number][] = [
  ['year', 365 * 24 * 3600],
  ['month', 30 * 24 * 3600],
  ['week', 7 * 24 * 3600],
  ['day', 24 * 3600],
  ['hour', 3600],
  ['minute', 60],
]

const format = new Intl.RelativeTimeFormat('en', { numeric: 'auto', style: 'short' })

// relative says how long ago iso was, in the largest whole unit.
export function relative(iso: string, now = Date.now()): string {
  const seconds = Math.round((now - Date.parse(iso)) / 1000)
  for (const [unit, size] of units) {
    if (Math.abs(seconds) >= size) return format.format(-Math.trunc(seconds / size), unit)
  }
  return 'just now'
}

// Age shows a relative time. Server and browser can disagree by a minute, so
// hydration warnings on the text are suppressed.
export function Age({ iso }: { iso: string }) {
  return (
    <time dateTime={iso} title={new Date(iso).toUTCString()} suppressHydrationWarning>
      {relative(iso)}
    </time>
  )
}

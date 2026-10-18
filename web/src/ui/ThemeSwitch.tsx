import { useEffect, useState } from 'react'
import { applyTheme, readTheme, type Theme } from '~/theme'
import { cx } from './cx'

const options: { value: Theme; label: string }[] = [
  { value: 'light', label: 'Day' },
  { value: 'dark', label: 'Night' },
  { value: 'system', label: 'Auto' },
]

export function ThemeSwitch() {
  const [theme, setTheme] = useState<Theme>('system')
  useEffect(() => setTheme(readTheme()), [])
  return (
    <fieldset className="m-0 flex rounded-pill border border-rule p-0.5 font-sans text-xs">
      <legend className="sr-only">Theme</legend>
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          aria-pressed={theme === o.value}
          onClick={() => {
            applyTheme(o.value)
            setTheme(o.value)
          }}
          className={cx(
            'rounded-pill px-3 py-1 font-semibold transition-colors duration-(--dur-press)',
            theme === o.value ? 'bg-ink text-paper' : 'text-ink-soft hover:text-ink',
          )}
        >
          {o.label}
        </button>
      ))}
    </fieldset>
  )
}

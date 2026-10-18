import { useRef } from 'react'
import { cx } from './cx'

type Props<T extends string> = {
  label: string
  value: T
  options: { value: T; label: string }[]
  onChange: (value: T) => void
}

// A tab row whose vermilion underline slides to the chosen tab. Arrow keys move between tabs.
export function Tabs<T extends string>({ label, value, options, onChange }: Props<T>) {
  const refs = useRef<(HTMLButtonElement | null)[]>([])
  const index = options.findIndex((o) => o.value === value)

  function onKey(e: React.KeyboardEvent, i: number) {
    const step = e.key === 'ArrowRight' ? 1 : e.key === 'ArrowLeft' ? -1 : 0
    if (!step) return
    e.preventDefault()
    const next = (i + step + options.length) % options.length
    const option = options[next]
    if (!option) return
    onChange(option.value)
    refs.current[next]?.focus()
  }

  return (
    <div role="tablist" aria-label={label} className="relative flex gap-6 border-b border-rule font-sans text-sm">
      {options.map((o, i) => (
        <button
          key={o.value}
          ref={(el) => {
            refs.current[i] = el
          }}
          type="button"
          role="tab"
          aria-selected={o.value === value}
          tabIndex={o.value === value ? 0 : -1}
          onClick={() => onChange(o.value)}
          onKeyDown={(e) => onKey(e, i)}
          className={cx(
            'pb-2 font-semibold transition-colors duration-(--dur-press)',
            o.value === value ? 'text-ink' : 'text-ink-faint hover:text-ink-soft',
          )}
        >
          {o.label}
        </button>
      ))}
      <span
        aria-hidden="true"
        className="absolute bottom-[-1px] left-0 h-0.5 bg-mark transition-[transform,width] duration-(--dur-move) ease-(--ease-out)"
        style={{
          width: refs.current[index]?.offsetWidth ?? 0,
          transform: `translateX(${refs.current[index]?.offsetLeft ?? 0}px)`,
        }}
      />
    </div>
  )
}

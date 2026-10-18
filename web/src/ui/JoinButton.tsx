import { useState } from 'react'
import { cx } from './cx'

type State = 'out' | 'pending' | 'in'

type Props = {
  community: string
  initial?: State
  // Resolves with the new state: "pending" while a remote community has not answered yet.
  send: (join: boolean) => Promise<State>
}

export function JoinButton({ community, initial = 'out', send }: Props) {
  const [state, setState] = useState<State>(initial)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState(false)

  async function toggle() {
    const previous = state
    const join = state === 'out'
    setState(join ? 'in' : 'out')
    setBusy(true)
    setError(false)
    try {
      setState(await send(join))
    } catch {
      setState(previous)
      setError(true)
    } finally {
      setBusy(false)
    }
  }

  const label = state === 'in' ? 'Joined' : state === 'pending' ? 'Requested' : 'Join'
  return (
    <span className="inline-flex items-center gap-3 font-sans">
      <button
        type="button"
        aria-pressed={state !== 'out'}
        aria-label={`${label}: ${community}`}
        aria-busy={busy}
        onClick={toggle}
        className={cx(
          'group inline-flex h-9 items-center gap-2 rounded-pill px-4 text-sm font-semibold',
          'transition-[background-color,color,box-shadow,transform] duration-(--dur-move) ease-(--ease-out) active:scale-95',
          state === 'out'
            ? 'bg-mark text-mark-ink hover:brightness-110'
            : 'bg-transparent text-ink shadow-[inset_0_0_0_1px_var(--rule-strong)] hover:bg-paper-sunk',
        )}
      >
        {state !== 'out' && (
          <svg viewBox="0 0 16 16" aria-hidden="true" className="size-4">
            <path
              d={state === 'in' ? 'M3 8.5 6.5 12 13 4.5' : 'M8 4v4l2.5 2.5'}
              fill="none"
              stroke="currentColor"
              strokeWidth="1.8"
              strokeLinecap="round"
              strokeLinejoin="round"
              pathLength={1}
              className="[stroke-dasharray:1] motion-safe:animate-[draw_var(--dur-open)_var(--ease-out)_both]"
            />
          </svg>
        )}
        {label}
      </button>
      {error && (
        <span role="alert" className="text-xs text-danger">
          Could not {state === 'out' ? 'join' : 'leave'}. Check your connection and try again.
        </span>
      )}
    </span>
  )
}

import { useState } from 'react'
import { cx } from './cx'

type Vote = -1 | 0 | 1

type Props = {
  score: number
  initial?: Vote
  label: string
  // Sends the vote; a rejected promise shows the failure and restores the previous vote.
  send: (value: Vote) => Promise<void>
}

export function VoteControl({ score, initial = 0, label, send }: Props) {
  const [vote, setVote] = useState<Vote>(initial)
  const [failed, setFailed] = useState<Vote | null>(null)
  const [bump, setBump] = useState(0)
  const shown = score - initial + vote

  async function cast(next: Vote) {
    const previous = vote
    const value = previous === next ? 0 : next
    setVote(value)
    setFailed(null)
    setBump((b) => b + 1)
    try {
      await send(value)
    } catch {
      setVote(previous)
      setFailed(value)
    }
  }

  return (
    <div className="flex items-center gap-1 font-sans">
      <fieldset aria-label={`Vote on ${label}`} className="m-0 flex items-center rounded-pill border border-rule p-0">
        <VoteButton direction="up" active={vote === 1} onClick={() => cast(1)} />
        <output
          key={bump}
          aria-live="polite"
          className={cx(
            'min-w-8 text-center text-sm font-semibold tabular-nums',
            'motion-safe:animate-[tick_var(--dur-move)_var(--ease-spring)]',
            vote === 1 && 'text-mark',
            vote === -1 && 'text-cool',
          )}
        >
          {shown}
        </output>
        <VoteButton direction="down" active={vote === -1} onClick={() => cast(-1)} />
      </fieldset>
      {failed !== null && (
        <span role="alert" className="flex items-center gap-2 text-xs text-danger">
          Vote not saved.
          <button
            type="button"
            className="font-semibold underline underline-offset-2"
            onClick={() => cast(failed === 0 ? vote : (failed as Vote))}
          >
            Try again
          </button>
        </span>
      )}
    </div>
  )
}

function VoteButton({
  direction,
  active,
  onClick,
}: {
  direction: 'up' | 'down'
  active: boolean
  onClick: () => void
}) {
  const up = direction === 'up'
  return (
    <button
      type="button"
      aria-pressed={active}
      aria-label={up ? 'Upvote' : 'Downvote'}
      onClick={onClick}
      className={cx(
        'grid size-8 place-items-center rounded-pill text-ink-faint',
        'transition-[color,background-color,transform] duration-(--dur-press) ease-(--ease-out)',
        'hover:bg-paper-sunk hover:text-ink active:scale-90',
        active && up && 'bg-mark-wash text-mark hover:text-mark',
        active && !up && 'bg-cool-wash text-cool hover:text-cool',
      )}
    >
      <svg viewBox="0 0 16 16" aria-hidden="true" className={cx('size-4', !up && 'rotate-180')}>
        <path
          d="M8 3 13.5 9.5H10V13H6V9.5H2.5Z"
          fill={active ? 'currentColor' : 'none'}
          stroke="currentColor"
          strokeWidth="1.4"
          strokeLinejoin="round"
        />
      </svg>
    </button>
  )
}

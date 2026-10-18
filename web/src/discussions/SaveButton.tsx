import { useMutation, useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { meOptions, saveMutation } from '~/api/gen/@tanstack/react-query.gen'
import { cn } from '~/ui/cn'

// SaveButton saves at once and undoes on a second press. A failure restores
// the previous state and offers a retry.
export function SaveButton({ apId, initial, label }: { apId: string; initial: boolean; label: string }) {
  const { data: me } = useQuery({ ...meOptions(), retry: false })
  const save = useMutation(saveMutation())
  const [saved, setSaved] = useState(initial)
  const [failed, setFailed] = useState<boolean | null>(null)
  if (!me) return null

  async function set(next: boolean) {
    setSaved(next)
    setFailed(null)
    try {
      await save.mutateAsync({ body: { object: apId, saved: next } })
    } catch {
      setSaved(!next)
      setFailed(next)
    }
  }

  return (
    <span className="flex items-center gap-2 font-sans text-xs">
      <button
        type="button"
        aria-pressed={saved}
        aria-label={saved ? `Unsave ${label}` : `Save ${label}`}
        onClick={() => set(!saved)}
        className={cn(
          'font-semibold transition-colors duration-(--dur-press) hover:text-mark',
          saved ? 'text-mark' : 'text-ink-soft',
        )}
      >
        {saved ? 'Saved' : 'Save'}
      </button>
      {failed !== null && (
        <span role="alert" className="flex items-center gap-2 text-danger">
          {failed ? 'Not saved.' : 'Still saved.'}
          <button type="button" className="font-semibold underline underline-offset-2" onClick={() => set(failed)}>
            Try again
          </button>
        </span>
      )}
    </span>
  )
}

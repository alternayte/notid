import { useMutation, useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { meOptions, voteMutation } from '~/api/gen/@tanstack/react-query.gen'
import type { Viewer } from '~/api/gen/types.gen'
import { VoteControl } from '~/ui/VoteControl'

// Votes wires VoteControl to the API. A signed-out visitor sees the score and a sign-in link.
export function Votes({ apId, score, viewer, label }: { apId: string; score: number; viewer: Viewer; label: string }) {
  const { data: me } = useQuery({ ...meOptions(), retry: false })
  const vote = useMutation(voteMutation())
  if (!me) {
    return (
      <Link
        to="/login"
        className="rounded-pill border border-rule px-3 py-1 font-sans text-xs text-ink-soft hover:text-mark"
        aria-label={`Score ${score}. Sign in to vote`}
      >
        <span className="font-semibold tabular-nums text-ink">{score}</span> · Sign in to vote
      </Link>
    )
  }
  // A changed vote keeps the counted flag it was cast with; a new vote counts once the account is trusted.
  const counts = viewer.vote !== 0 ? viewer.voteCounted : me.trust === 'trusted'
  return (
    <VoteControl
      score={score}
      initial={viewer.vote as -1 | 0 | 1}
      counts={counts}
      label={label}
      send={async (value) => {
        await vote.mutateAsync({ body: { object: apId, value } })
      }}
    />
  )
}

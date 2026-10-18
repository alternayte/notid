import { useMutation, useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { useState } from 'react'
import { meOptions, voteMutation } from '~/api/gen/@tanstack/react-query.gen'
import type { Viewer } from '~/api/gen/types.gen'
import { VoteControl } from '~/ui/VoteControl'
import { splitHandle } from './AuthorLink'

// Votes wires VoteControl to the API. A signed-out visitor sees the score and a sign-in link.
// The first vote a session sends to a community on another site says who can see it.
export function Votes({
  apId,
  score,
  viewer,
  label,
  community,
}: {
  apId: string
  score: number
  viewer: Viewer
  label: string
  community?: string
}) {
  const { data: me } = useQuery({ ...meOptions(), retry: false })
  const vote = useMutation(voteMutation())
  const host = community ? splitHandle(community).host : undefined
  const [noted, setNoted] = useState(false)
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
  function noteRemoteVote() {
    if (!host) return
    const key = `notid:voted:${host}`
    try {
      if (sessionStorage.getItem(key)) return
      sessionStorage.setItem(key, '1')
    } catch {
      // Storage can be unavailable; the note is still worth showing.
    }
    setNoted(true)
  }
  return (
    <span className="flex flex-col gap-1">
      <VoteControl
        score={score}
        initial={viewer.vote as -1 | 0 | 1}
        counts={counts}
        label={label}
        send={async (value) => {
          await vote.mutateAsync({ body: { object: apId, value } })
          noteRemoteVote()
        }}
      />
      {noted && host && (
        <span role="status" className="font-sans text-xs text-ink-faint">
          Your vote went to {host}. Admins there can see who voted.
        </span>
      )}
    </span>
  )
}

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { useEffect } from 'react'
import {
  communityOptions,
  communityQueryKey,
  discoverCommunitiesQueryKey,
  followCommunityMutation,
  homeFeedInfiniteQueryKey,
  leaveCommunityMutation,
  myCommunitiesQueryKey,
} from '~/api/gen/@tanstack/react-query.gen'
import type { Summary } from '~/api/gen/types.gen'
import { JoinButton } from '~/ui/JoinButton'

// A join to a community on another server waits for that server to accept it. Its state is read again this often.
const pendingPoll = 4000

// Join wires JoinButton to the API. A signed-out visitor gets a sign-in link instead.
export function Join({ community, signedIn }: { community: Summary; signedIn: boolean }) {
  const queryClient = useQueryClient()
  const follow = useMutation(followCommunityMutation())
  const leave = useMutation(leaveCommunityMutation())
  const waiting = signedIn && community.membership?.state === 'pending'
  const fresh = useQuery({
    ...communityOptions({ path: { handle: community.handle } }),
    enabled: waiting,
    refetchInterval: (q) => (q.state.data?.membership?.state === 'pending' ? pendingPoll : false),
  })
  const accepted = waiting && fresh.data?.membership?.state === 'accepted'

  useEffect(() => {
    if (!accepted) return
    for (const key of [myCommunitiesQueryKey(), discoverCommunitiesQueryKey(), homeFeedInfiniteQueryKey()]) {
      void queryClient.invalidateQueries({ queryKey: key })
    }
  }, [accepted, queryClient])

  if (!signedIn) {
    return (
      <Link to="/login" className="font-sans text-sm font-semibold text-mark">
        Sign in to join
      </Link>
    )
  }
  if (community.membership?.role === 'owner') {
    return <span className="font-sans text-sm text-ink-faint">You own this community</span>
  }

  const m = accepted ? fresh.data?.membership : community.membership
  const initial = !m ? 'out' : m.state === 'accepted' ? 'in' : 'pending'
  return (
    <>
      <JoinButton
        // A new server state remounts the button, so it shows the answer without a reload.
        key={initial}
        community={community.title || community.name}
        initial={initial}
        send={async (join) => {
          const body = { community: community.apId }
          const res = join ? await follow.mutateAsync({ body }) : await leave.mutateAsync({ body })
          for (const key of [
            communityQueryKey({ path: { handle: community.handle } }),
            myCommunitiesQueryKey(),
            discoverCommunitiesQueryKey(),
          ]) {
            void queryClient.invalidateQueries({ queryKey: key })
          }
          return res.state === 'accepted' ? 'in' : res.state === 'pending' ? 'pending' : 'out'
        }}
      />
      {initial === 'pending' && (
        <p role="status" className="basis-full font-sans text-xs text-ink-faint">
          Waiting for {community.domain} to accept. This usually takes a few seconds and can take a few minutes. New
          posts reach your home feed once it does.
        </p>
      )}
    </>
  )
}

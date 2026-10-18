import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import {
  communityQueryKey,
  discoverCommunitiesQueryKey,
  followCommunityMutation,
  leaveCommunityMutation,
  myCommunitiesQueryKey,
} from '~/api/gen/@tanstack/react-query.gen'
import type { Summary } from '~/api/gen/types.gen'
import { JoinButton } from '~/ui/JoinButton'

// Join wires JoinButton to the API. A signed-out visitor gets a sign-in link instead.
export function Join({ community, signedIn }: { community: Summary; signedIn: boolean }) {
  const queryClient = useQueryClient()
  const follow = useMutation(followCommunityMutation())
  const leave = useMutation(leaveCommunityMutation())

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

  const m = community.membership
  const initial = !m ? 'out' : m.state === 'accepted' ? 'in' : 'pending'
  return (
    <JoinButton
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
  )
}

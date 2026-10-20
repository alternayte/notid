import { useQuery, useSuspenseQuery } from '@tanstack/react-query'
import { createFileRoute, Link } from '@tanstack/react-router'
import type { FormEvent } from 'react'
import { discoverCommunitiesOptions, meOptions } from '~/api/gen/@tanstack/react-query.gen'
import { CommunityCard } from '~/communities/CommunityCard'
import { AddressHint, useOpenCommunity } from '~/communities/OpenCommunity'
import { Button } from '~/ui/primitives/button'
import { Input } from '~/ui/primitives/input'
import { Page } from './__root'

export const Route = createFileRoute('/discover')({
  head: () => ({ meta: [{ title: 'Discover · Open Communities' }] }),
  loader: ({ context }) => context.queryClient.ensureQueryData(discoverCommunitiesOptions()),
  component: Discover,
})

function Discover() {
  const { data } = useSuspenseQuery(discoverCommunitiesOptions())
  const { data: me } = useQuery({ ...meOptions(), retry: false })
  return (
    <Page>
      <div className="flex flex-col gap-2">
        <h1 className="font-display text-4xl tracking-tight">Discover</h1>
        <p className="max-w-(--container-measure) font-serif text-lg text-ink-soft">
          Communities on this site, and communities elsewhere that our admins have reviewed. Largest first. Search finds
          more on Lemmy and PieFed servers.
        </p>
      </div>
      <div className="flex flex-col gap-3">
        <AddressHint />
        <FindByAddress signedIn={!!me} />
      </div>
      {data.communities.length === 0 ? (
        <section className="flex max-w-(--container-measure) flex-col gap-2 rounded-sm bg-paper-sunk p-4">
          <h2 className="font-display text-lg tracking-tight">No communities yet</h2>
          <p className="font-serif text-sm text-ink-soft">Be the first to start one.</p>
          <Link to="/communities/new" className="w-fit font-sans text-sm font-semibold text-mark">
            Start a community
          </Link>
        </section>
      ) : (
        <div className="grid grid-cols-[minmax(0,1fr)] gap-x-8 gap-y-10 sm:grid-cols-2 lg:grid-cols-3">
          {data.communities.map((c) => (
            <CommunityCard key={c.apId} community={c} signedIn={!!me} />
          ))}
        </div>
      )}
    </Page>
  )
}

// FindByAddress opens any community by its URL or handle, on this site or elsewhere.
function FindByAddress({ signedIn }: { signedIn: boolean }) {
  const { open, error, busy } = useOpenCommunity()

  function onSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    void open(String(new FormData(e.currentTarget).get('address') ?? '').trim())
  }

  if (!signedIn) {
    return (
      <p className="font-sans text-sm text-ink-soft">
        Know a community elsewhere?{' '}
        <Link to="/login" className="font-semibold text-mark">
          Sign in
        </Link>{' '}
        to open it by its address.
      </p>
    )
  }
  return (
    <form onSubmit={onSubmit} className="flex max-w-(--container-measure) flex-col gap-2">
      <label htmlFor="address" className="font-sans text-sm font-semibold">
        Open a community by its address
      </label>
      <div className="flex gap-2">
        <Input
          id="address"
          name="address"
          required
          minLength={3}
          placeholder="woodworking@lemmy.world or https://…"
          aria-invalid={error !== '' || undefined}
          aria-describedby={error ? 'address-error' : undefined}
        />
        <Button type="submit" variant="outline" className="h-auto" disabled={busy}>
          {busy ? 'Looking…' : 'Open'}
        </Button>
      </div>
      {error && (
        <p id="address-error" role="alert" className="font-sans text-xs text-danger">
          {error}
        </p>
      )}
    </form>
  )
}

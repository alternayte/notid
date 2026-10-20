import { useMutation } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import { useState } from 'react'
import { errorMessage } from '~/api/errors'
import { lookupCommunityMutation } from '~/api/gen/@tanstack/react-query.gen'
import type { IndexEntry } from '~/api/gen/types.gen'
import { Kicker } from '~/ui/Kicker'
import { Button } from '~/ui/primitives/button'

// communityAddress returns the community a search names by its address: name@server, !name@server, or a URL.
export function communityAddress(q: string): string | null {
  const s = q.trim()
  if (/^https?:\/\/\S+$/i.test(s)) return s
  const m = /^[!@]?([a-z0-9_]+)@([a-z0-9.-]+\.[a-z]{2,}(?::\d+)?)$/i.exec(s)
  return m ? `${m[1]}@${m[2]}` : null
}

// useOpenCommunity looks a community up on its server, which fetches it and its newest posts, then opens its page.
export function useOpenCommunity() {
  const navigate = useNavigate()
  const lookup = useMutation(lookupCommunityMutation())
  const [error, setError] = useState('')
  async function open(address: string) {
    setError('')
    try {
      const c = await lookup.mutateAsync({ body: { community: address } })
      await navigate({ to: '/c/$handle', params: { handle: c.handle } })
    } catch (err) {
      setError(errorMessage(err, 'That community could not be reached. Check the address and try again.'))
    }
  }
  return { open, error, busy: lookup.isPending }
}

// AddressHint tells people that any community on another server opens by its address.
export function AddressHint() {
  return (
    <p className="max-w-(--container-measure) font-serif text-sm text-ink-soft">
      Every community on Lemmy, PieFed and other sites like this one has an address, such as{' '}
      <span className="font-sans font-semibold text-ink">technology@lemmy.world</span>. Type one into Search to open it.
    </p>
  )
}

export function activeLabel(n: number) {
  return `${n.toLocaleString('en')} active this month`
}

const software: Record<IndexEntry['software'], string> = { lemmy: 'Lemmy', piefed: 'PieFed' }

// IndexCard is one community from the community index. One this site has not fetched opens through a lookup,
// which only a signed-in person can start.
export function IndexCard({ entry: e, signedIn }: { entry: IndexEntry; signedIn: boolean }) {
  const { open, error, busy } = useOpenCommunity()
  return (
    <article className="flex flex-col gap-2 border-t-2 border-rule-strong pt-3" aria-labelledby={`index-${e.apId}`}>
      <Kicker>{`${software[e.software]} · ${e.domain}`}</Kicker>
      <h3 id={`index-${e.apId}`} className="font-display text-2xl tracking-tight text-balance">
        {e.title || e.name}
      </h3>
      <p className="font-sans text-xs font-semibold text-ink-soft">{e.handle}</p>
      {e.description && <p className="line-clamp-3 font-serif text-sm text-ink-soft">{e.description}</p>}
      <div className="mt-auto flex flex-wrap items-center justify-between gap-3 pt-2">
        <span className="font-sans text-xs text-ink-faint">
          {activeLabel(e.activeMonth)}
          {e.nsfw && ' · 18+'}
        </span>
        {e.known ? (
          <Link to="/c/$handle" params={{ handle: e.handle }} className="font-sans text-sm font-semibold text-mark">
            Open
          </Link>
        ) : signedIn ? (
          <Button variant="outline" size="sm" disabled={busy} onClick={() => void open(e.apId)}>
            {busy ? 'Opening…' : 'Open'}
          </Button>
        ) : (
          <Link to="/login" className="font-sans text-sm font-semibold text-mark">
            Sign in to open
          </Link>
        )}
      </div>
      {error && (
        <p role="alert" className="font-sans text-xs text-danger">
          {error}
        </p>
      )}
    </article>
  )
}

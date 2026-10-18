import { Link } from '@tanstack/react-router'
import type { Summary } from '~/api/gen/types.gen'
import { Kicker } from '~/ui/Kicker'
import { fromRenderer, Rendered } from '~/ui/RenderedHtml'
import { accentAttr } from './accents'
import { Join } from './Join'

export function membersLabel(n: number) {
  return `${n.toLocaleString('en')} ${n === 1 ? 'member' : 'members'}`
}

// One community in a list: kicker, name as the headline, the start of its description, and Join.
export function CommunityCard({ community: c, signedIn }: { community: Summary; signedIn: boolean }) {
  return (
    <article
      data-accent={accentAttr(c.theme)}
      className="flex flex-col gap-2 border-t-2 border-mark pt-3"
      aria-labelledby={`community-${c.apId}`}
    >
      <Kicker>{c.local ? 'Community' : `Community on ${c.domain}`}</Kicker>
      <h2 id={`community-${c.apId}`} className="font-display text-2xl tracking-tight text-balance">
        <Link
          to="/c/$handle"
          params={{ handle: c.handle }}
          className="no-underline decoration-mark decoration-2 hover:underline"
        >
          {c.title || c.name}
        </Link>
      </h2>
      {c.descriptionHtml && (
        <Rendered html={fromRenderer(c.descriptionHtml)} className="line-clamp-3 font-serif text-sm text-ink-soft" />
      )}
      <div className="mt-auto flex flex-wrap items-center justify-between gap-3 pt-2">
        <span className="font-sans text-xs text-ink-faint">
          {c.local ? membersLabel(c.members) : `${membersLabel(c.members)} from this site`}
          {c.nsfw && ' · 18+'}
        </span>
        <Join community={c} signedIn={signedIn} />
      </div>
    </article>
  )
}

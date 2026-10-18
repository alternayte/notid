import { Link } from '@tanstack/react-router'
import type { ReactNode } from 'react'
import type { PostView } from '~/api/gen/types.gen'
import { PostCard } from '~/ui/PostCard'
import { Age } from './Age'
import { AuthorLink } from './AuthorLink'
import { SaveButton } from './SaveButton'
import { Votes } from './Votes'

export function threadPath(p: { community: string; ref: string }) {
  return `/c/${p.community}/p/${p.ref}`
}

// PostItem is one post in a list, linked to its thread.
export function PostItem({ post: p, showCommunity = true }: { post: PostView; showCommunity?: boolean }) {
  return (
    <PostCard
      community={showCommunity ? p.communityTitle || p.community : ''}
      title={p.title}
      href={threadPath(p)}
      linkTo={({ className, hash, children }: { className: string; hash?: string; children: ReactNode }) => (
        <Link to="/c/$handle/p/$ref" params={{ handle: p.community, ref: p.ref }} hash={hash} className={className}>
          {children}
        </Link>
      )}
      author={<AuthorLink author={p.author} />}
      bot={p.author.bot}
      age={<Age iso={p.published} />}
      comments={p.comments}
      standfirst={p.excerpt || undefined}
      link={p.url && !p.image ? p.url : undefined}
      image={
        p.image ? (
          <img src={p.url} alt="" loading="lazy" className="aspect-square w-full rounded-sm object-cover" />
        ) : undefined
      }
      votes={<Votes apId={p.apId} score={p.score} viewer={p.viewer} label={p.title} />}
      actions={<SaveButton apId={p.apId} initial={p.viewer.saved} label={p.title} />}
    />
  )
}

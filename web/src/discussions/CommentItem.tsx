import { Link } from '@tanstack/react-router'
import type { CommentView } from '~/api/gen/types.gen'
import { Kicker } from '~/ui/Kicker'
import { BotLabel } from '~/ui/PostCard'
import { fromRenderer, Rendered } from '~/ui/RenderedHtml'
import { Age } from './Age'
import { AuthorLink } from './AuthorLink'
import { SaveButton } from './SaveButton'
import { Votes } from './Votes'

// CommentItem is one comment listed outside its thread, linked to its place there.
export function CommentItem({
  comment: c,
  post,
  kicker,
}: {
  comment: CommentView
  post: { ref: string; title: string; community: string }
  kicker?: string
}) {
  const label = `${c.author.displayName || c.author.handle}'s comment`
  return (
    <article className="flex flex-col gap-2 border-t border-rule py-6 first:border-t-0">
      {kicker && <Kicker>{kicker}</Kicker>}
      <p className="font-sans text-xs text-ink-faint">
        <AuthorLink author={c.author} />
        {c.author.bot && <BotLabel />} on{' '}
        <Link
          to="/c/$handle/p/$ref"
          params={{ handle: post.community, ref: post.ref }}
          hash={`c-${c.ref}`}
          className="font-semibold text-ink-soft hover:text-mark"
        >
          {post.title}
        </Link>{' '}
        · <Age iso={c.published} />
      </p>
      <Rendered html={fromRenderer(c.bodyHtml)} className="prose-news text-base" />
      <div className="flex flex-wrap items-center gap-4">
        <Votes apId={c.apId} score={c.score} viewer={c.viewer} label={label} />
        <SaveButton apId={c.apId} initial={c.viewer.saved} label={label} />
      </div>
    </article>
  )
}

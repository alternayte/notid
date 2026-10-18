import type { ReactNode } from 'react'
import { Kicker } from './Kicker'

type Props = {
  community: string
  title: string
  href: string
  author: string
  authorHost?: string
  bot?: boolean
  age: string
  comments: number
  standfirst?: string
  link?: string
  image?: ReactNode
  votes: ReactNode
}

// One entry in a feed, set like an article in a news column: kicker,
// headline, standfirst, byline. Rules between entries replace card boxes.
export function PostCard({
  community,
  title,
  href,
  author,
  authorHost,
  bot,
  age,
  comments,
  standfirst,
  link,
  image,
  votes,
}: Props) {
  return (
    <article className="grid grid-cols-[1fr_auto] gap-x-4 gap-y-2 sm:gap-x-6 border-t border-rule py-6 first:border-t-0">
      <div className="flex flex-col gap-2">
        <Kicker>{community}</Kicker>
        <h2 className="font-display text-xl tracking-tight text-balance">
          <a href={href} className="no-underline decoration-mark decoration-2 hover:underline">
            {title}
          </a>
        </h2>
        {link && (
          <a href={link} className="w-fit font-sans text-xs text-ink-soft hover:text-mark" rel="nofollow noopener">
            {linkHost(link)} ↗
          </a>
        )}
        {standfirst && <p className="line-clamp-3 font-serif text-base text-ink-soft">{standfirst}</p>}
      </div>
      {image && <div className="row-span-2 w-20 sm:w-40">{image}</div>}
      <footer className="col-start-1 flex flex-wrap items-center gap-x-4 gap-y-2 pt-1 font-sans text-xs text-ink-faint">
        {votes}
        <span>
          by <span className="font-semibold text-ink-soft">{author}</span>
          {authorHost && <span className="text-ink-faint">@{authorHost}</span>}
          {bot && <BotLabel />}
        </span>
        <span aria-hidden="true" className="hidden sm:inline">
          ·
        </span>
        <span>{age}</span>
        <span aria-hidden="true" className="hidden sm:inline">
          ·
        </span>
        <a href={`${href}#comments`} className="hover:text-mark">
          {comments} {comments === 1 ? 'reply' : 'replies'}
        </a>
      </footer>
    </article>
  )
}

export function BotLabel() {
  return (
    <span className="ml-2 rounded-xs border border-rule px-1 py-px text-2xs font-semibold uppercase tracking-kicker text-ink-soft">
      Bot
    </span>
  )
}

// A remote post's link can be any string a peer sent.
function linkHost(link: string): string {
  try {
    return new URL(link).host
  } catch {
    return link
  }
}

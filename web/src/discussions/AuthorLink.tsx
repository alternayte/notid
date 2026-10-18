import { Link } from '@tanstack/react-router'
import type { Author } from '~/api/gen/types.gen'

export function splitHandle(handle: string): { name: string; host?: string } {
  const [name = handle, host] = handle.split('@')
  return { name, host }
}

// AuthorLink names a person and links to their profile; people from other sites show their host.
export function AuthorLink({ author }: { author: Author }) {
  const { name, host } = splitHandle(author.handle)
  return (
    <Link to="/u/$handle" params={{ handle: author.handle }} className="font-semibold text-ink-soft hover:text-mark">
      {author.displayName || name}
      {host && <span className="font-normal text-ink-faint">@{host}</span>}
    </Link>
  )
}

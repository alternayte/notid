import { Link } from '@tanstack/react-router'

// MarkedNotice stands in for an NSFW community or post while the viewer's NSFW setting is off.
export function MarkedNotice({ signedIn }: { signedIn: boolean }) {
  return (
    <section
      aria-label="Marked 18+"
      className="flex max-w-(--container-measure) flex-col gap-2 rounded-sm bg-paper-sunk p-4"
    >
      <p className="font-display text-lg tracking-tight">Marked 18+.</p>
      <p className="font-serif text-sm text-ink-soft">
        NSFW content stays hidden until a signed-in person turns it on and confirms they are 18 or older.
      </p>
      {signedIn ? (
        <Link to="/settings" className="w-fit font-sans text-sm font-semibold text-mark">
          Turn on NSFW in Settings
        </Link>
      ) : (
        <Link to="/login" className="w-fit font-sans text-sm font-semibold text-mark">
          Sign in to view
        </Link>
      )}
    </section>
  )
}

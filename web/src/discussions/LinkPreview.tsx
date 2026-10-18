import type { Preview } from '~/api/gen/types.gen'

function hostOf(url: string) {
  try {
    return new URL(url).host
  } catch {
    return url
  }
}

// LinkPreview is the link of a link post, with what the page says about itself when it was read.
export function LinkPreview({ url, preview }: { url: string; preview?: Preview }) {
  if (!preview?.title && !preview?.description && !preview?.imageSrc) {
    return (
      <a
        href={url}
        rel="nofollow noopener ugc"
        className="w-fit break-all font-sans text-sm text-ink-soft hover:text-mark"
      >
        {url} ↗
      </a>
    )
  }
  return (
    <a
      href={url}
      rel="nofollow noopener ugc"
      className="group flex max-w-xl gap-4 rounded-sm border border-rule p-3 no-underline hover:border-mark"
    >
      {preview.imageSrc && (
        <img
          src={preview.imageSrc}
          alt=""
          loading="lazy"
          className="size-20 shrink-0 rounded-sm object-cover sm:size-24"
        />
      )}
      <span className="flex min-w-0 flex-col gap-1">
        <span className="font-sans text-xs text-ink-faint">{hostOf(url)} ↗</span>
        {preview.title ? (
          <span className="font-display text-lg leading-snug text-ink group-hover:text-mark">{preview.title}</span>
        ) : (
          <span className="break-all font-sans text-sm text-ink-soft group-hover:text-mark">{url}</span>
        )}
        {preview.description && (
          <span className="line-clamp-3 font-serif text-sm text-ink-soft">{preview.description}</span>
        )}
      </span>
    </a>
  )
}

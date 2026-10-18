import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { createFileRoute, Link } from '@tanstack/react-router'
import { meOptions, savedInfiniteOptions } from '~/api/gen/@tanstack/react-query.gen'
import type { SavedItem } from '~/api/gen/types.gen'
import { Age } from '~/discussions/Age'
import { PostItem, splitAuthor } from '~/discussions/PostItem'
import { SaveButton } from '~/discussions/SaveButton'
import { Votes } from '~/discussions/Votes'
import { Kicker } from '~/ui/Kicker'
import { Button } from '~/ui/primitives/button'
import { fromRenderer, Rendered } from '~/ui/RenderedHtml'
import { Page } from './__root'

const savedOptions = () => ({
  ...savedInfiniteOptions(),
  initialPageParam: '',
  getNextPageParam: (last: { next: string }) => last.next || undefined,
  retry: false,
})

export const Route = createFileRoute('/saved')({
  head: () => ({ meta: [{ title: 'Saved · Open Communities' }] }),
  loader: ({ context }) => context.queryClient.prefetchInfiniteQuery(savedOptions()),
  component: Saved,
})

function Saved() {
  const { data: me, isPending: mePending } = useQuery({ ...meOptions(), retry: false })
  const saved = useInfiniteQuery({ ...savedOptions(), enabled: !!me })

  return (
    <Page>
      <h1 className="font-display text-4xl tracking-tight">Saved</h1>
      {!me && !mePending ? (
        <Link to="/login" className="w-fit font-sans text-sm font-semibold text-mark">
          Sign in to see what you saved
        </Link>
      ) : saved.isError ? (
        <p role="alert" className="font-sans text-sm text-danger">
          Your saved items did not load. Reload the page to try again.
        </p>
      ) : saved.data && saved.data.pages[0]?.items.length === 0 ? (
        <section className="flex max-w-(--container-measure) flex-col gap-2 rounded-sm bg-paper-sunk p-4">
          <h2 className="font-display text-lg tracking-tight">Nothing saved yet</h2>
          <p className="font-serif text-sm text-ink-soft">Press Save on a post or a reply to keep it here.</p>
        </section>
      ) : (
        <div className="flex max-w-(--container-measure) flex-col">
          {saved.data?.pages
            .flatMap((p) => p.items)
            .map((item) => (
              <Item key={item.comment?.apId ?? item.post.apId} item={item} />
            ))}
          {saved.hasNextPage && (
            <div className="flex items-center gap-3 border-t border-rule pt-4">
              <Button variant="outline" disabled={saved.isFetchingNextPage} onClick={() => saved.fetchNextPage()}>
                {saved.isFetchingNextPage ? 'Loading…' : 'Older saves'}
              </Button>
              {saved.isFetchNextPageError && (
                <span role="alert" className="font-sans text-sm text-danger">
                  Older saves did not load. Try again.
                </span>
              )}
            </div>
          )}
        </div>
      )}
    </Page>
  )
}

function Item({ item }: { item: SavedItem }) {
  const c = item.comment
  if (!c) return <PostItem post={item.post} />
  const a = splitAuthor(c.author.handle)
  const label = `${c.author.displayName || a.name}'s comment`
  return (
    <article className="flex flex-col gap-2 border-t border-rule py-6 first:border-t-0">
      <Kicker>Reply in {item.post.communityTitle || item.post.community}</Kicker>
      <p className="font-sans text-xs text-ink-faint">
        <span className="font-semibold text-ink-soft">{c.author.displayName || a.name}</span>
        {a.host && <span>@{a.host}</span>} on{' '}
        <Link
          to="/c/$handle/p/$ref"
          params={{ handle: item.post.community, ref: item.post.ref }}
          hash={`c-${c.ref}`}
          className="font-semibold text-ink-soft hover:text-mark"
        >
          {item.post.title}
        </Link>{' '}
        · <Age iso={c.published} />
      </p>
      <Rendered html={fromRenderer(c.bodyHtml)} className="prose-news text-base" />
      <div className="flex flex-wrap items-center gap-4">
        <Votes apId={c.apId} score={c.score} viewer={c.viewer} label={label} />
        <SaveButton apId={c.apId} initial label={label} />
      </div>
    </article>
  )
}

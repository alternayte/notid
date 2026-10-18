import { useMutation, useQuery, useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import { createFileRoute, Link, notFound, redirect } from '@tanstack/react-router'
import { useMemo } from 'react'
import { errorMessage } from '~/api/errors'
import {
  communityOptions,
  createCommentMutation,
  meOptions,
  threadOptions,
  threadQueryKey,
} from '~/api/gen/@tanstack/react-query.gen'
import { About } from '~/communities/About'
import { accentAttr } from '~/communities/accents'
import { Age } from '~/discussions/Age'
import { AuthorLink, splitHandle } from '~/discussions/AuthorLink'
import { SaveButton } from '~/discussions/SaveButton'
import { type Node, tree } from '~/discussions/tree'
import { Votes } from '~/discussions/Votes'
import { ModActions } from '~/moderation/ModActions'
import { Kicker } from '~/ui/Kicker'
import { BotLabel } from '~/ui/PostCard'
import { fromRenderer, Rendered } from '~/ui/RenderedHtml'
import { type CommentNode, ReplyBox, Thread } from '~/ui/Thread'
import { Page } from './__root'

export const Route = createFileRoute('/c/$handle_/p/$ref')({
  loader: async ({ context, params }) => {
    const t = await context.queryClient.ensureQueryData(threadOptions({ path: { ref: params.ref } })).catch(() => {
      throw notFound()
    })
    // A ref is global, so a link with another community's handle still finds the post; send it to its own address.
    if (t.post.community !== params.handle) {
      throw redirect({ to: '/c/$handle/p/$ref', params: { handle: t.post.community, ref: params.ref } })
    }
    await context.queryClient.ensureQueryData(communityOptions({ path: { handle: params.handle } }))
    return t.post.title
  },
  head: ({ loaderData }) => ({ meta: [{ title: `${loaderData ?? 'Post'} · Open Communities` }] }),
  component: ThreadPage,
})

function ThreadPage() {
  const { handle, ref } = Route.useParams()
  const { data: t } = useSuspenseQuery(threadOptions({ path: { ref } }))
  const { data: c } = useSuspenseQuery(communityOptions({ path: { handle } }))
  const { data: me } = useQuery({ ...meOptions(), retry: false })
  const queryClient = useQueryClient()
  const comment = useMutation(createCommentMutation())
  const comments = useMemo(() => tree(t.comments), [t.comments])
  const p = t.post
  const open = c.state === 'active' || !c.local
  const canModerate =
    c.local && (c.membership?.role === 'owner' || c.membership?.role === 'moderator') && c.state === 'active'
  const signInToReport = me !== undefined && me.handle !== p.author.handle

  async function reply(inReplyTo: string, text: string) {
    try {
      await comment.mutateAsync({ body: { inReplyTo, body: text } })
    } catch (err) {
      throw new Error(errorMessage(err, 'Reply not sent.'))
    }
    await queryClient.invalidateQueries({ queryKey: threadQueryKey({ path: { ref } }) })
  }

  return (
    <div data-accent={accentAttr(c.theme)}>
      <Page>
        {!open && (
          <p role="status" className="rounded-sm bg-mark-wash px-4 py-3 font-sans text-sm">
            This community is moving to another site. It is read-only for now.
          </p>
        )}
        {p.removed && (
          <div role="status" className="flex flex-col gap-2 rounded-sm bg-mark-wash px-4 py-3 font-sans text-sm">
            <p className="font-semibold">A moderator removed this post.</p>
            <p>
              Only you and the moderators can see it now.
              {c.appealsContact && (
                <>
                  {' '}
                  To appeal, contact <span className="font-semibold">{c.appealsContact}</span>.
                </>
              )}
            </p>
          </div>
        )}
        <div className="grid gap-12 lg:grid-cols-[minmax(0,1fr)_18rem]">
          <div className="flex flex-col gap-6">
            <article className="flex max-w-(--container-measure) flex-col gap-4 border-b border-rule pb-6">
              <Kicker>
                <Link to="/c/$handle" params={{ handle }} className="no-underline hover:text-mark">
                  {c.title || c.name}
                </Link>
              </Kicker>
              <h1 className="font-display text-3xl tracking-tight text-balance sm:text-4xl">{p.title}</h1>
              <p className="font-sans text-xs text-ink-faint">
                by <AuthorLink author={p.author} />
                {p.author.bot && <BotLabel />}
                <span aria-hidden="true"> · </span>
                <Age iso={p.published} />
                {p.nsfw && ' · 18+'}
              </p>
              {p.image && (
                <img src={p.url} alt="" className="max-h-[80vh] w-auto self-start rounded-sm border border-rule" />
              )}
              {p.url && !p.image && (
                <a
                  href={p.url}
                  rel="nofollow noopener ugc"
                  className="w-fit break-all font-sans text-sm text-ink-soft hover:text-mark"
                >
                  {p.url} ↗
                </a>
              )}
              {p.bodyHtml && <Rendered html={fromRenderer(p.bodyHtml)} className="prose-news" />}
              <div className="flex flex-wrap items-center gap-4 pt-2">
                <Votes apId={p.apId} score={p.score} viewer={p.viewer} label={p.title} />
                <SaveButton apId={p.apId} initial={p.viewer.saved} label={p.title} />
                <ModActions
                  apId={p.apId}
                  kind="post"
                  ref={p.ref}
                  community={p.community}
                  author={p.author.handle}
                  removed={p.removed}
                  locked={p.locked}
                  canModerate={canModerate}
                  canReport={signInToReport}
                />
              </div>
            </article>
            <section
              id="comments"
              aria-labelledby="comments-title"
              className="flex max-w-(--container-measure) scroll-mt-8 flex-col gap-6"
            >
              <h2
                id="comments-title"
                className="border-b border-rule-strong pb-1 font-sans text-xs font-bold uppercase tracking-kicker"
              >
                {p.comments} {p.comments === 1 ? 'reply' : 'replies'}
              </h2>
              {open &&
                (me ? (
                  <ReplyBox label="Reply to the post" autoFocus={false} send={(text) => reply(p.apId, text)} />
                ) : (
                  <Link to="/login" className="w-fit font-sans text-sm font-semibold text-mark">
                    Sign in to reply
                  </Link>
                ))}
              {comments.length === 0 ? (
                <p className="font-serif text-sm text-ink-soft">No replies yet.</p>
              ) : (
                <Thread
                  comments={comments.map(node)}
                  renderActions={(n) => (
                    <>
                      <Votes apId={n.apId} score={n.score} viewer={n.viewer} label={`${n.author}'s comment`} />
                      <SaveButton apId={n.apId} initial={n.viewer.saved} label={`${n.author}'s comment`} />
                    </>
                  )}
                  onReply={open && !p.locked && !p.removed && me ? (n, text) => reply(n.apId, text) : undefined}
                />
              )}
              {t.truncated && (
                <p className="font-sans text-sm text-ink-faint">
                  This thread is longer than one page. Later replies are not shown.
                </p>
              )}
            </section>
          </div>
          <About community={c} />
        </div>
      </Page>
    </div>
  )
}

type ThreadNode = CommentNode & Pick<Node, 'apId' | 'score' | 'viewer'> & { replies: ThreadNode[] }

function node(n: Node): ThreadNode {
  const a = splitHandle(n.author.handle)
  return {
    apId: n.apId,
    score: n.score,
    viewer: n.viewer,
    gone: n.gone,
    id: n.ref,
    author: n.author.displayName || a.name,
    authorLink: <AuthorLink author={n.author} />,
    authorHost: a.host,
    bot: n.author.bot,
    age: <Age iso={n.published} />,
    body: <Rendered html={fromRenderer(n.bodyHtml)} className="prose-news text-base" />,
    replies: n.replies.map(node),
  }
}

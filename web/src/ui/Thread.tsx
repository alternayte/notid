import { type ReactNode, useId, useState } from 'react'
import { cn } from './cn'
import { BotLabel } from './PostCard'
import { Button } from './primitives/button'
import { Textarea } from './primitives/textarea'

export type CommentNode = {
  id: string
  author: string
  authorHost?: string
  bot?: boolean
  age: ReactNode
  body: ReactNode
  gone?: boolean
  replies: CommentNode[]
}

type Actions<T extends CommentNode> = {
  renderActions: (c: T) => ReactNode
  // Sends a reply; a rejected promise keeps the text and shows the failure.
  // Without it, comments offer no Reply button.
  onReply?: (c: T, text: string) => Promise<void>
}

// A threaded discussion. Each level hangs off a hairline rule that also
// collapses the branch, like marginal notes against a column.
export function Thread<T extends CommentNode>({ comments, ...actions }: { comments: T[] } & Actions<T>) {
  return (
    <ol className="flex flex-col gap-5">
      {comments.map((c) => (
        <Comment key={c.id} comment={c} {...actions} />
      ))}
    </ol>
  )
}

function Comment<T extends CommentNode>({ comment, ...actions }: { comment: T } & Actions<T>) {
  const [open, setOpen] = useState(true)
  const [replying, setReplying] = useState(false)
  const branch = useId()
  const onReply = actions.onReply
  const who = comment.gone ? 'a deleted' : `${comment.author}'s`
  return (
    <li id={`c-${comment.id}`} className="relative flex scroll-mt-8 flex-col gap-2">
      <header className="flex flex-wrap items-center gap-2 font-sans text-xs text-ink-faint">
        <button
          type="button"
          aria-expanded={open}
          aria-controls={branch}
          aria-label={open ? `Collapse ${who} comment` : `Expand ${who} comment`}
          onClick={() => setOpen((o) => !o)}
          className="grid size-5 place-items-center rounded-xs border border-rule text-ink-soft hover:border-rule-strong"
        >
          <span aria-hidden="true">{open ? '−' : '+'}</span>
        </button>
        {comment.gone ? (
          <span className="italic">Deleted</span>
        ) : (
          <span>
            <span className="font-semibold text-ink-soft">{comment.author}</span>
            {comment.authorHost && <span>@{comment.authorHost}</span>}
            {comment.bot && <BotLabel />}
          </span>
        )}
        <span aria-hidden="true">·</span>
        <span>{comment.age}</span>
        {!open && comment.replies.length > 0 && <span>· {count(comment)} hidden</span>}
      </header>
      <div
        id={branch}
        className={cn(
          'grid transition-[grid-template-rows,opacity] duration-(--dur-open) ease-(--ease-out)',
          open ? 'grid-rows-[1fr] opacity-100' : 'grid-rows-[0fr] opacity-0',
        )}
      >
        <div className="overflow-hidden">
          <div className="ml-2.5 flex flex-col gap-3 border-l border-rule pl-5">
            {comment.gone ? (
              <p className="font-serif text-base text-ink-faint">This comment was deleted.</p>
            ) : (
              <>
                <div className="font-serif text-base">{comment.body}</div>
                <div className="flex flex-wrap items-center gap-4 font-sans text-xs">
                  {actions.renderActions(comment)}
                  {onReply && (
                    <button
                      type="button"
                      onClick={() => setReplying((r) => !r)}
                      className="font-semibold text-ink-soft hover:text-mark"
                      aria-expanded={replying}
                    >
                      Reply
                    </button>
                  )}
                </div>
                {onReply && replying && (
                  <ReplyBox
                    label={`Reply to ${comment.author}`}
                    send={(text) => onReply(comment, text)}
                    onClose={() => setReplying(false)}
                  />
                )}
              </>
            )}
            {comment.replies.length > 0 && <Thread comments={comment.replies as T[]} {...actions} />}
          </div>
        </div>
      </div>
    </li>
  )
}

// ReplyBox writes a reply.
export function ReplyBox({
  label,
  send,
  onClose,
  autoFocus = true,
}: {
  label: string
  send: (text: string) => Promise<void>
  onClose?: () => void
  autoFocus?: boolean
}) {
  const id = useId()
  const [text, setText] = useState('')
  const [busy, setBusy] = useState(false)
  const [failed, setFailed] = useState<string | null>(null)
  return (
    <form
      className="flex flex-col gap-2 motion-safe:animate-[unfold_var(--dur-open)_var(--ease-out)]"
      onSubmit={async (e) => {
        e.preventDefault()
        if (!text.trim()) return
        setBusy(true)
        setFailed(null)
        try {
          await send(text)
          setText('')
          onClose?.()
        } catch (err) {
          setFailed(err instanceof Error && err.message ? err.message : 'Reply not sent.')
        } finally {
          setBusy(false)
        }
      }}
    >
      <label htmlFor={id} className="sr-only">
        {label}
      </label>
      <Textarea
        id={id}
        autoFocus={autoFocus}
        rows={3}
        placeholder="Write a reply"
        value={text}
        maxLength={20000}
        onChange={(e) => setText(e.target.value)}
      />
      {failed && (
        <p role="alert" className="font-sans text-xs text-danger">
          {failed} Your text is kept; try again.
        </p>
      )}
      <div className="flex gap-2">
        <Button type="submit" disabled={busy}>
          {busy ? 'Sending…' : 'Reply'}
        </Button>
        {onClose && (
          <Button type="button" variant="quiet" onClick={onClose}>
            Cancel
          </Button>
        )}
      </div>
    </form>
  )
}

function count(c: CommentNode): number {
  return c.replies.reduce((n, r) => n + 1 + count(r), 0)
}

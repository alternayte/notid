import { useId, useState } from 'react'
import { cx } from './cx'

export type CommentNode = {
  id: string
  author: string
  authorHost?: string
  age: string
  body: string
  score: number
  replies: CommentNode[]
}

// A threaded discussion. Each level hangs off a hairline rule that also
// collapses the branch, like marginal notes against a column.
export function Thread({
  comments,
  renderVotes,
}: {
  comments: CommentNode[]
  renderVotes: (c: CommentNode) => React.ReactNode
}) {
  return (
    <ol className="flex flex-col gap-5">
      {comments.map((c) => (
        <Comment key={c.id} comment={c} renderVotes={renderVotes} />
      ))}
    </ol>
  )
}

function Comment({ comment, renderVotes }: { comment: CommentNode; renderVotes: (c: CommentNode) => React.ReactNode }) {
  const [open, setOpen] = useState(true)
  const [replying, setReplying] = useState(false)
  const branch = useId()
  return (
    <li className="relative flex flex-col gap-2">
      <header className="flex items-center gap-2 font-sans text-xs text-ink-faint">
        <button
          type="button"
          aria-expanded={open}
          aria-controls={branch}
          aria-label={open ? `Collapse ${comment.author}'s comment` : `Expand ${comment.author}'s comment`}
          onClick={() => setOpen((o) => !o)}
          className="grid size-5 place-items-center rounded-xs border border-rule text-ink-soft hover:border-rule-strong"
        >
          <span aria-hidden="true">{open ? '−' : '+'}</span>
        </button>
        <span className="font-semibold text-ink-soft">{comment.author}</span>
        {comment.authorHost && <span>@{comment.authorHost}</span>}
        <span aria-hidden="true">·</span>
        <span>{comment.age}</span>
        {!open && comment.replies.length > 0 && <span>· {count(comment)} hidden</span>}
      </header>
      <div
        id={branch}
        className={cx(
          'grid transition-[grid-template-rows,opacity] duration-(--dur-open) ease-(--ease-out)',
          open ? 'grid-rows-[1fr] opacity-100' : 'grid-rows-[0fr] opacity-0',
        )}
      >
        <div className="overflow-hidden">
          <div className="ml-2.5 flex flex-col gap-3 border-l border-rule pl-5">
            <p className="font-serif text-base">{comment.body}</p>
            <div className="flex items-center gap-4 font-sans text-xs">
              {renderVotes(comment)}
              <button
                type="button"
                onClick={() => setReplying((r) => !r)}
                className="font-semibold text-ink-soft hover:text-mark"
                aria-expanded={replying}
              >
                Reply
              </button>
            </div>
            {replying && <ReplyBox onClose={() => setReplying(false)} />}
            {comment.replies.length > 0 && <Thread comments={comment.replies} renderVotes={renderVotes} />}
          </div>
        </div>
      </div>
    </li>
  )
}

function ReplyBox({ onClose }: { onClose: () => void }) {
  const id = useId()
  return (
    <form
      className="flex flex-col gap-2 motion-safe:animate-[unfold_var(--dur-open)_var(--ease-out)]"
      onSubmit={(e) => {
        e.preventDefault()
        onClose()
      }}
    >
      <label htmlFor={id} className="sr-only">
        Your reply
      </label>
      {/* biome-ignore lint/a11y/noAutofocus: the reply box opens on the reader's request */}
      <textarea id={id} autoFocus rows={3} className="field font-serif" placeholder="Write a reply" />
      <div className="flex gap-2">
        <button type="submit" className="btn-primary">
          Reply
        </button>
        <button type="button" onClick={onClose} className="btn-quiet">
          Cancel
        </button>
      </div>
    </form>
  )
}

function count(c: CommentNode): number {
  return c.replies.reduce((n, r) => n + 1 + count(r), 0)
}

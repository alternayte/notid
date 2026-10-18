import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { type FormEvent, useId, useState } from 'react'
import { errorMessage } from '~/api/errors'
import {
  deleteCommentMutation,
  deletePostMutation,
  editCommentMutation,
  editPostMutation,
  threadQueryKey,
} from '~/api/gen/@tanstack/react-query.gen'
import type { PostView } from '~/api/gen/types.gen'
import { Button } from '~/ui/primitives/button'
import { Checkbox } from '~/ui/primitives/checkbox'
import { Input } from '~/ui/primitives/input'
import { Textarea } from '~/ui/primitives/textarea'

const field = 'font-sans text-xs font-semibold'

// PostAuthorActions edits or deletes a post, for its author only.
export function PostAuthorActions({ post, handle }: { post: PostView; handle: string }) {
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const edit = useMutation(editPostMutation())
  const del = useMutation(deletePostMutation())
  const ids = { title: useId(), body: useId(), url: useId() }
  const [editing, setEditing] = useState(false)
  const [confirming, setConfirming] = useState(false)
  const [title, setTitle] = useState(post.title)
  const [body, setBody] = useState(post.body)
  const [url, setUrl] = useState(post.url)
  const [nsfw, setNsfw] = useState(post.nsfw)
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState<string | null>(null)

  async function save(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    setBusy(true)
    setMessage(null)
    try {
      // The edit form starts from the source text; a post's excerpt is what the list shows.
      await edit.mutateAsync({ path: { ref: post.ref }, body: { title: title.trim(), body, url, nsfw } })
      await queryClient.invalidateQueries({ queryKey: threadQueryKey({ path: { ref: post.ref } }) })
      setEditing(false)
    } catch (err) {
      setMessage(errorMessage(err, 'Your changes were not saved. Try again.'))
    } finally {
      setBusy(false)
    }
  }

  async function remove() {
    setBusy(true)
    setMessage(null)
    try {
      await del.mutateAsync({ path: { ref: post.ref } })
      await queryClient.invalidateQueries({ queryKey: ['thread'] })
      await navigate({ to: '/c/$handle', params: { handle } })
    } catch (err) {
      setMessage(errorMessage(err, 'The post was not deleted. Try again.'))
      setBusy(false)
    }
  }

  if (editing) {
    return (
      <form onSubmit={save} className="flex w-full flex-col gap-3 border-t border-rule pt-4">
        <div className="flex flex-col gap-1">
          <label htmlFor={ids.title} className={field}>
            Title
          </label>
          <Input id={ids.title} required maxLength={200} value={title} onChange={(e) => setTitle(e.target.value)} />
        </div>
        <div className="flex flex-col gap-1">
          <label htmlFor={ids.body} className={field}>
            Text
          </label>
          <Textarea id={ids.body} rows={8} maxLength={40000} value={body} onChange={(e) => setBody(e.target.value)} />
        </div>
        <div className="flex flex-col gap-1">
          <label htmlFor={ids.url} className={field}>
            Link
          </label>
          <Input id={ids.url} maxLength={2000} value={url} onChange={(e) => setUrl(e.target.value)} />
        </div>
        <div className="flex items-center gap-2">
          <Checkbox id={ids.body + '-nsfw'} checked={nsfw} onCheckedChange={(v) => setNsfw(v === true)} />
          <label htmlFor={ids.body + '-nsfw'} className="font-sans text-sm">
            Adult content (18+)
          </label>
        </div>
        <div className="flex flex-wrap items-center gap-3">
          <Button type="submit" disabled={busy}>
            {busy ? 'Saving…' : 'Save changes'}
          </Button>
          <Button type="button" variant="quiet" disabled={busy} onClick={() => setEditing(false)}>
            Cancel
          </Button>
          {message && (
            <p role="alert" className="font-sans text-sm text-danger">
              {message}
            </p>
          )}
        </div>
      </form>
    )
  }

  return (
    <div className="flex flex-wrap items-center gap-4 font-sans text-xs">
      <button type="button" onClick={() => setEditing(true)} className="font-semibold text-ink-soft hover:text-mark">
        Edit
      </button>
      {confirming ? (
        <>
          <button
            type="button"
            disabled={busy}
            onClick={() => void remove()}
            className="font-semibold text-danger hover:underline"
          >
            {busy ? 'Deleting…' : 'Yes, delete this post'}
          </button>
          <button type="button" disabled={busy} onClick={() => setConfirming(false)} className="text-ink-soft">
            Keep it
          </button>
        </>
      ) : (
        <button
          type="button"
          onClick={() => setConfirming(true)}
          className="font-semibold text-ink-soft hover:text-mark"
        >
          Delete
        </button>
      )}
      {message && (
        <p role="alert" className="font-sans text-sm text-danger">
          {message}
        </p>
      )}
    </div>
  )
}

// CommentAuthorActions edits or deletes a comment, for its author only.
export function CommentAuthorActions({
  commentRef,
  body,
  onChanged,
}: {
  commentRef: string
  body: string
  onChanged: () => Promise<void>
}) {
  const edit = useMutation(editCommentMutation())
  const del = useMutation(deleteCommentMutation())
  const id = useId()
  const [editing, setEditing] = useState(false)
  const [confirming, setConfirming] = useState(false)
  const [text, setText] = useState(body)
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState<string | null>(null)

  async function save(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    setBusy(true)
    setMessage(null)
    try {
      await edit.mutateAsync({ path: { ref: commentRef }, body: { body: text } })
      await onChanged()
      setEditing(false)
    } catch (err) {
      setMessage(errorMessage(err, 'Your changes were not saved. Try again.'))
    } finally {
      setBusy(false)
    }
  }

  async function remove() {
    setBusy(true)
    setMessage(null)
    try {
      await del.mutateAsync({ path: { ref: commentRef } })
      await onChanged()
    } catch (err) {
      setMessage(errorMessage(err, 'The comment was not deleted. Try again.'))
      setBusy(false)
    }
  }

  if (editing) {
    return (
      <form onSubmit={save} className="flex w-full flex-col gap-2">
        <label htmlFor={id} className="sr-only">
          Edit your comment
        </label>
        <Textarea id={id} rows={4} maxLength={20000} value={text} onChange={(e) => setText(e.target.value)} />
        <div className="flex flex-wrap items-center gap-3 font-sans text-xs">
          <Button type="submit" size="sm" disabled={busy}>
            {busy ? 'Saving…' : 'Save'}
          </Button>
          <Button type="button" size="sm" variant="quiet" disabled={busy} onClick={() => setEditing(false)}>
            Cancel
          </Button>
          {message && (
            <p role="alert" className="font-sans text-sm text-danger">
              {message}
            </p>
          )}
        </div>
      </form>
    )
  }

  return (
    <>
      <button type="button" onClick={() => setEditing(true)} className="font-semibold text-ink-soft hover:text-mark">
        Edit
      </button>
      {confirming ? (
        <>
          <button
            type="button"
            disabled={busy}
            onClick={() => void remove()}
            className="font-semibold text-danger hover:underline"
          >
            {busy ? 'Deleting…' : 'Yes, delete'}
          </button>
          <button type="button" disabled={busy} onClick={() => setConfirming(false)} className="text-ink-soft">
            Keep it
          </button>
        </>
      ) : (
        <button
          type="button"
          onClick={() => setConfirming(true)}
          className="font-semibold text-ink-soft hover:text-mark"
        >
          Delete
        </button>
      )}
      {message && (
        <p role="alert" className="font-sans text-sm text-danger">
          {message}
        </p>
      )}
    </>
  )
}

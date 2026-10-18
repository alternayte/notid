import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { errorMessage } from '~/api/errors'
import {
  banActorMutation,
  communityPostsQueryKey,
  lockPostMutation,
  removeObjectMutation,
  reportObjectMutation,
  restoreObjectMutation,
  threadQueryKey,
} from '~/api/gen/@tanstack/react-query.gen'
import { Button } from '~/ui/primitives/button'
import { Input } from '~/ui/primitives/input'
import { Textarea } from '~/ui/primitives/textarea'

type Props = {
  apId: string
  kind: 'post' | 'comment'
  ref: string
  community: string
  author: string
  removed: boolean
  locked?: boolean
  canModerate: boolean
  canReport: boolean
}

// ModActions gives moderators Remove, Restore, Lock and Ban, and everyone else Report.
// Each action reports its failure in place and refreshes the thread.
export function ModActions(p: Props) {
  const queryClient = useQueryClient()
  const remove = useMutation(removeObjectMutation())
  const restore = useMutation(restoreObjectMutation())
  const lock = useMutation(lockPostMutation())
  const report = useMutation(reportObjectMutation())
  const ban = useMutation(banActorMutation())
  const [open, setOpen] = useState<'report' | 'ban' | null>(null)
  const [reason, setReason] = useState('')
  const [days, setDays] = useState('')
  const [failed, setFailed] = useState<string | null>(null)

  async function run(action: () => Promise<unknown>) {
    setFailed(null)
    try {
      await action()
      setOpen(null)
      setReason('')
      for (const key of [
        threadQueryKey({ path: { ref: p.ref } }),
        communityPostsQueryKey({ path: { handle: p.community }, query: { sort: 'new' } }),
      ]) {
        void queryClient.invalidateQueries({ queryKey: key })
      }
      await queryClient.invalidateQueries({ queryKey: communityPostsQueryKey({ path: { handle: p.community } }) })
    } catch (err) {
      setFailed(errorMessage(err, 'That did not work. Try again.'))
    }
  }

  const link = 'font-semibold text-ink-soft hover:text-mark'
  return (
    <span className="inline-flex flex-wrap items-center gap-3 font-sans text-xs">
      {p.canModerate && (
        <>
          {p.removed ? (
            <button
              type="button"
              className={link}
              onClick={() => run(() => restore.mutateAsync({ body: { object: p.apId } }))}
            >
              Restore
            </button>
          ) : (
            <button
              type="button"
              className={link}
              onClick={() => run(() => remove.mutateAsync({ body: { object: p.apId, reason } }))}
            >
              Remove
            </button>
          )}
          {p.kind === 'post' && (
            <button
              type="button"
              className={link}
              onClick={() => run(() => lock.mutateAsync({ body: { object: p.apId, locked: !p.locked } }))}
            >
              {p.locked ? 'Unlock' : 'Lock'}
            </button>
          )}
          <button type="button" className={link} onClick={() => setOpen(open === 'ban' ? null : 'ban')}>
            Ban author
          </button>
        </>
      )}
      {p.canReport && (
        <button type="button" className={link} onClick={() => setOpen(open === 'report' ? null : 'report')}>
          Report
        </button>
      )}
      {failed && (
        <span role="alert" className="text-danger">
          {failed}
        </span>
      )}
      {open === 'report' && (
        <form
          className="flex w-full flex-col gap-2 pt-2"
          onSubmit={(e) => {
            e.preventDefault()
            void run(() => report.mutateAsync({ body: { object: p.apId, reason } }))
          }}
        >
          <label className="sr-only" htmlFor={`report-${p.ref}`}>
            Why are you reporting this?
          </label>
          <Textarea
            id={`report-${p.ref}`}
            rows={2}
            maxLength={1000}
            required
            autoFocus
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            placeholder="What is wrong with it? The moderators of this community see this."
          />
          <div className="flex gap-2">
            <Button type="submit">Send report</Button>
            <Button type="button" variant="quiet" onClick={() => setOpen(null)}>
              Cancel
            </Button>
          </div>
        </form>
      )}
      {open === 'ban' && (
        <form
          className="flex w-full flex-col gap-2 pt-2"
          onSubmit={(e) => {
            e.preventDefault()
            void run(() =>
              ban.mutateAsync({
                body: { community: p.community, actor: p.author, reason, days: Number(days) || 0 },
              }),
            )
          }}
        >
          <p className="text-ink-soft">
            Ban {p.author} from {p.community}. This appears in the public moderation log.
          </p>
          <label className="sr-only" htmlFor={`ban-reason-${p.ref}`}>
            Reason
          </label>
          <Textarea
            id={`ban-reason-${p.ref}`}
            rows={2}
            maxLength={500}
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            placeholder="Reason (public)"
          />
          <div className="flex items-center gap-2">
            <label htmlFor={`ban-days-${p.ref}`} className="text-ink-soft">
              Days (empty bans until lifted)
            </label>
            <Input
              id={`ban-days-${p.ref}`}
              inputMode="numeric"
              className="w-20"
              value={days}
              onChange={(e) => setDays(e.target.value.replace(/\D/g, ''))}
            />
          </div>
          <div className="flex gap-2">
            <Button type="submit">Ban</Button>
            <Button type="button" variant="quiet" onClick={() => setOpen(null)}>
              Cancel
            </Button>
          </div>
        </form>
      )}
    </span>
  )
}

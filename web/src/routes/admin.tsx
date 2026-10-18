import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { createFileRoute, Link } from '@tanstack/react-router'
import { type FormEvent, useId, useState } from 'react'
import { errorMessage } from '~/api/errors'
import {
  meOptions,
  removeObjectMutation,
  resolveReportMutation,
  siteBanMutation,
  siteBansOptions,
  siteBansQueryKey,
  siteReportsOptions,
  siteReportsQueryKey,
  siteUnbanMutation,
} from '~/api/gen/@tanstack/react-query.gen'
import type { Report, SiteBanView } from '~/api/gen/types.gen'
import { Age } from '~/discussions/Age'
import { Button } from '~/ui/primitives/button'
import { Input } from '~/ui/primitives/input'
import { Textarea } from '~/ui/primitives/textarea'
import { Page } from './__root'

export const Route = createFileRoute('/admin')({
  loader: async ({ context }) => {
    await context.queryClient.prefetchQuery({ ...siteReportsOptions(), retry: false })
    await context.queryClient.prefetchQuery({ ...siteBansOptions(), retry: false })
  },
  head: () => ({ meta: [{ title: 'Site admin · Open Communities' }] }),
  component: Admin,
})

const label = 'font-sans text-sm font-semibold'
const hint = 'font-sans text-xs text-ink-faint'

function Admin() {
  const { data: me } = useQuery({ ...meOptions(), retry: false })
  if (!me) {
    return (
      <Page>
        <h1 className="font-display text-4xl tracking-tight">Site admin</h1>
        <Link to="/login" className="w-fit font-sans text-sm font-semibold text-mark">
          Sign in to use it
        </Link>
      </Page>
    )
  }
  if (!me.admin) {
    return (
      <Page>
        <h1 className="font-display text-4xl tracking-tight">Site admin</h1>
        <p className="max-w-(--container-measure) font-serif text-base text-ink-soft">
          Only site admins see this page. An admin runs <code className="font-mono text-sm">notid admin create</code> to
          add the first one.
        </p>
        <Link to="/" className="w-fit font-sans text-sm font-semibold text-mark">
          Go to the home feed
        </Link>
      </Page>
    )
  }
  return <AdminBody />
}

function AdminBody() {
  const queryClient = useQueryClient()
  const reports = useQuery({ ...siteReportsOptions(), retry: false })
  const bans = useQuery({ ...siteBansOptions(), retry: false })
  const remove = useMutation(removeObjectMutation())
  const resolve = useMutation(resolveReportMutation())
  const ban = useMutation(siteBanMutation())
  const unban = useMutation(siteUnbanMutation())
  const [status, setStatus] = useState<{ ok: boolean; message: string } | null>(null)

  async function act(fn: () => Promise<unknown>, message: string) {
    try {
      await fn()
      setStatus({ ok: true, message })
    } catch (err) {
      setStatus({ ok: false, message: errorMessage(err, 'That did not work. Try again.') })
    }
    for (const key of [siteReportsQueryKey(), siteBansQueryKey()]) {
      void queryClient.invalidateQueries({ queryKey: key })
    }
  }

  return (
    <Page>
      <div className="flex flex-col gap-2">
        <h1 className="font-display text-4xl tracking-tight">Site admin</h1>
        <p className="max-w-(--container-measure) font-serif text-sm text-ink-soft">
          Site bans stop a person acting anywhere on this installation. Reports here are the open reports on content
          written on this site, in any community.
        </p>
      </div>
      {status && (
        <p role="status" className={`font-sans text-sm ${status.ok ? 'text-ink-soft' : 'text-danger'}`}>
          {status.message}
        </p>
      )}
      <section className="flex flex-col gap-4">
        <h2 className="font-display text-2xl tracking-tight">Reports on local content</h2>
        {reports.isPending ? (
          <p className="font-sans text-sm text-ink-faint">Loading…</p>
        ) : reports.isError ? (
          <p role="alert" className="font-sans text-sm text-danger">
            The report queue did not load. Reload the page to try again.
          </p>
        ) : reports.data.reports.length === 0 ? (
          <p className="font-serif text-sm text-ink-soft">No open reports.</p>
        ) : (
          <ol className="flex max-w-(--container-measure) flex-col gap-6">
            {reports.data.reports.map((r) => (
              <ReportRow
                key={r.id}
                report={r}
                onAct={act}
                remove={() => remove.mutateAsync({ body: { object: r.postApId ?? '' } })}
                resolve={(upheld) => resolve.mutateAsync({ path: { handle: r.community, id: r.id }, body: { upheld } })}
              />
            ))}
          </ol>
        )}
      </section>
      <section className="flex flex-col gap-4">
        <h2 className="font-display text-2xl tracking-tight">Site bans</h2>
        {bans.isPending ? (
          <p className="font-sans text-sm text-ink-faint">Loading…</p>
        ) : (
          <ul className="flex max-w-(--container-measure) flex-col gap-4">
            {(bans.data?.bans ?? []).map((b) => (
              <BanRow
                key={b.person}
                ban={b}
                onLift={() =>
                  act(() => unban.mutateAsync({ body: { actor: b.person } }), `Lifted the ban on ${b.person}.`)
                }
              />
            ))}
            {(bans.data?.bans ?? []).length === 0 && (
              <li className="font-serif text-sm text-ink-soft">No site bans.</li>
            )}
          </ul>
        )}
        <BanForm
          onBan={(actor, reason, days) =>
            act(() => ban.mutateAsync({ body: { actor, reason, days } }), `Banned ${actor}.`)
          }
        />
      </section>
    </Page>
  )
}

function ReportRow({
  report: r,
  onAct,
  remove,
  resolve,
}: {
  report: Report
  onAct: (fn: () => Promise<unknown>, message: string) => Promise<void>
  remove: () => Promise<unknown>
  resolve: (upheld: boolean) => Promise<unknown>
}) {
  return (
    <li className="flex flex-col gap-3 border-t border-rule pt-4">
      <p className="font-sans text-xs text-ink-faint">
        Reported by <span className="font-semibold text-ink-soft">{r.reporter}</span> · <Age iso={r.createdAt} /> ·{' '}
        <Link to="/c/$handle" params={{ handle: r.community }} className="font-semibold text-mark">
          {r.community}
        </Link>
      </p>
      <p className="font-serif text-base">{r.reason}</p>
      <p className="font-serif text-sm text-ink-soft">
        On:{' '}
        <Link
          to="/c/$handle/p/$ref"
          params={{ handle: r.community, ref: r.postRef ?? '' }}
          className="font-sans font-semibold text-mark"
        >
          {r.postTitle ?? r.commentBody ?? 'a comment'}
        </Link>
        {r.commentBody && r.postTitle && (
          <span className="block border-l border-rule pl-3 italic">“{r.commentBody}”</span>
        )}
      </p>
      <div className="flex flex-wrap gap-2">
        {r.postApId && (
          <Button variant="outline" onClick={() => onAct(remove, 'Removed the content.')}>
            Remove the content
          </Button>
        )}
        <Button onClick={() => onAct(() => resolve(true), 'Report upheld.')}>Uphold</Button>
        <Button variant="quiet" onClick={() => onAct(() => resolve(false), 'Report dismissed.')}>
          Dismiss
        </Button>
      </div>
    </li>
  )
}

function BanRow({ ban, onLift }: { ban: SiteBanView; onLift: () => Promise<void> }) {
  return (
    <li className="flex flex-wrap items-baseline justify-between gap-3 border-t border-rule pt-3">
      <div className="flex flex-col gap-1">
        <span className="font-sans text-sm font-semibold">{ban.person}</span>
        <span className="font-serif text-sm text-ink-soft">
          {ban.reason || 'No reason recorded'} · <Age iso={ban.createdAt} />
          {ban.expiresAt ? ` · until ${new Date(ban.expiresAt).toLocaleDateString()}` : ' · until lifted'}
        </span>
      </div>
      <Button variant="quiet" onClick={() => void onLift()}>
        Lift
      </Button>
    </li>
  )
}

function BanForm({ onBan }: { onBan: (actor: string, reason: string, days: number) => Promise<void> }) {
  const ids = { actor: useId(), reason: useId(), days: useId() }
  const [actor, setActor] = useState('')
  const [reason, setReason] = useState('')
  const [days, setDays] = useState('0')
  const [busy, setBusy] = useState(false)

  async function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    setBusy(true)
    await onBan(actor.trim().replace(/^[@!]/, ''), reason.trim(), Number(days) || 0)
    setActor('')
    setReason('')
    setDays('0')
    setBusy(false)
  }

  return (
    <form onSubmit={submit} className="flex max-w-(--container-measure) flex-col gap-4 border-t border-rule pt-4">
      <h3 className="font-display text-lg tracking-tight">Ban someone from the site</h3>
      <div className="flex flex-col gap-2">
        <label htmlFor={ids.actor} className={label}>
          Their handle
        </label>
        <Input
          id={ids.actor}
          required
          value={actor}
          onChange={(e) => setActor(e.target.value)}
          placeholder="ada or sam@lemmy.example"
          aria-describedby={`${ids.actor}-hint`}
        />
        <p id={`${ids.actor}-hint`} className={hint}>
          A name on this site, or name@domain for someone elsewhere.
        </p>
      </div>
      <div className="flex flex-col gap-2">
        <label htmlFor={ids.reason} className={label}>
          Reason
        </label>
        <Textarea
          id={ids.reason}
          rows={2}
          maxLength={500}
          value={reason}
          onChange={(e) => setReason(e.target.value)}
          aria-describedby={`${ids.reason}-hint`}
        />
        <p id={`${ids.reason}-hint`} className={hint}>
          The public moderation log records it.
        </p>
      </div>
      <div className="flex flex-col gap-2">
        <label htmlFor={ids.days} className={label}>
          Days
        </label>
        <Input
          id={ids.days}
          type="number"
          min={0}
          max={3650}
          value={days}
          onChange={(e) => setDays(e.target.value)}
          className="w-28"
          aria-describedby={`${ids.days}-hint`}
        />
        <p id={`${ids.days}-hint`} className={hint}>
          0 bans until a site admin lifts it.
        </p>
      </div>
      <Button type="submit" className="w-fit" disabled={busy || actor.trim() === ''}>
        Ban from the site
      </Button>
    </form>
  )
}

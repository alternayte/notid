import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { createFileRoute, Link } from '@tanstack/react-router'
import { type FormEvent, useId, useState } from 'react'
import { errorMessage } from '~/api/errors'
import {
  addToDirectoryMutation,
  blockPeerMutation,
  directoryOptions,
  directoryQueryKey,
  meOptions,
  removeFromDirectoryMutation,
  removeObjectMutation,
  resolveReportMutation,
  setTrustMutation,
  siteBanMutation,
  siteBansOptions,
  siteBansQueryKey,
  sitePeersOptions,
  sitePeersQueryKey,
  siteReportsOptions,
  siteReportsQueryKey,
  siteUnbanMutation,
} from '~/api/gen/@tanstack/react-query.gen'
import type { DirectoryView, PeerView, Report, SiteBanView } from '~/api/gen/types.gen'
import { Age, relative } from '~/discussions/Age'
import { Button } from '~/ui/primitives/button'
import { Input } from '~/ui/primitives/input'
import { Textarea } from '~/ui/primitives/textarea'
import { Page } from './__root'

export const Route = createFileRoute('/admin')({
  loader: async ({ context }) => {
    await context.queryClient.prefetchQuery({ ...siteReportsOptions(), retry: false })
    await context.queryClient.prefetchQuery({ ...siteBansOptions(), retry: false })
    await context.queryClient.prefetchQuery({ ...sitePeersOptions(), retry: false })
    await context.queryClient.prefetchQuery({ ...directoryOptions(), retry: false })
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
  const peers = useQuery({ ...sitePeersOptions(), retry: false })
  const dir = useQuery({ ...directoryOptions(), retry: false })
  const remove = useMutation(removeObjectMutation())
  const resolve = useMutation(resolveReportMutation())
  const ban = useMutation(siteBanMutation())
  const unban = useMutation(siteUnbanMutation())
  const block = useMutation(blockPeerMutation())
  const addDirectory = useMutation(addToDirectoryMutation())
  const removeDirectory = useMutation(removeFromDirectoryMutation())
  const trust = useMutation(setTrustMutation())
  const [status, setStatus] = useState<{ ok: boolean; message: string } | null>(null)

  async function act(fn: () => Promise<unknown>, message: string) {
    try {
      await fn()
      setStatus({ ok: true, message })
    } catch (err) {
      setStatus({ ok: false, message: errorMessage(err, 'That did not work. Try again.') })
    }
    for (const key of [siteReportsQueryKey(), siteBansQueryKey(), sitePeersQueryKey(), directoryQueryKey()]) {
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
        <h2 className="font-display text-2xl tracking-tight">Peers</h2>
        <p className="max-w-(--container-measure) font-serif text-sm text-ink-soft">
          A blocked peer's activities are refused, nothing more is sent to it, and its communities leave Discover and
          search. Its content stays where it already is.
        </p>
        {peers.isPending ? (
          <p className="font-sans text-sm text-ink-faint">Loading…</p>
        ) : peers.isError ? (
          <p role="alert" className="font-sans text-sm text-danger">
            The peer list did not load. Reload the page to try again.
          </p>
        ) : (
          <ul className="flex max-w-(--container-measure) flex-col gap-4">
            {peers.data.peers.map((p) => (
              <PeerRow
                key={p.domain}
                peer={p}
                onBlock={(reason) =>
                  act(
                    () => block.mutateAsync({ body: { domain: p.domain, blocked: true, reason } }),
                    `Blocked ${p.domain}.`,
                  )
                }
                onUnblock={() =>
                  act(() => block.mutateAsync({ body: { domain: p.domain, blocked: false } }), `Unblocked ${p.domain}.`)
                }
              />
            ))}
            {peers.data.peers.length === 0 && (
              <li className="font-serif text-sm text-ink-soft">No server has contacted this installation yet.</li>
            )}
          </ul>
        )}
        <BlockPeerForm
          onBlock={(domain, reason) =>
            act(() => block.mutateAsync({ body: { domain, blocked: true, reason } }), `Blocked ${domain}.`)
          }
        />
      </section>
      <section className="flex flex-col gap-4">
        <h2 className="font-display text-2xl tracking-tight">Directory</h2>
        <p className="max-w-(--container-measure) font-serif text-sm text-ink-soft">
          Discover shows the communities on this site and the ones listed here. This site follows each listed community,
          so its posts reach Popular and All before anyone here joins it. Anything else a person reaches by pasting its
          address.
        </p>
        {dir.isPending ? (
          <p className="font-sans text-sm text-ink-faint">Loading…</p>
        ) : dir.isError ? (
          <p role="alert" className="font-sans text-sm text-danger">
            The directory did not load. Reload the page to try again.
          </p>
        ) : (
          <ul className="flex max-w-(--container-measure) flex-col gap-3">
            {dir.data.communities.map((c) => (
              <DirectoryRow
                key={c.handle}
                entry={c}
                onRemove={() =>
                  act(
                    () => removeDirectory.mutateAsync({ body: { community: c.handle } }),
                    `Took ${c.handle} out of the directory.`,
                  )
                }
              />
            ))}
            {dir.data.communities.length === 0 && (
              <li className="font-serif text-sm text-ink-soft">The directory is empty.</li>
            )}
          </ul>
        )}
        <AddDirectoryForm
          onAdd={(community, note) =>
            act(() => addDirectory.mutateAsync({ body: { community, note } }), `Listed ${community}.`)
          }
        />
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
      <section className="flex flex-col gap-4">
        <h2 className="font-display text-2xl tracking-tight">Trust levels</h2>
        <p className="max-w-(--container-measure) font-serif text-sm text-ink-soft">
          A new account becomes established on its own after a few days and a few posts that no moderator removed.
          Promote one early, or demote one that went wrong.
        </p>
        <TrustForm
          onSet={(handle, trusted) =>
            act(
              () => trust.mutateAsync({ path: { handle }, body: { trusted } }),
              trusted ? `${handle} is established.` : `${handle} is new again.`,
            )
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

const siteFollow: Record<DirectoryView['siteFollow'], string> = {
  accepted: 'This site follows it. New posts arrive here.',
  pending: 'Waiting for its server to accept the site follow.',
  none: 'Not followed. Add it again to follow it.',
}

function DirectoryRow({ entry, onRemove }: { entry: DirectoryView; onRemove: () => Promise<void> }) {
  return (
    <li className="flex flex-wrap items-baseline justify-between gap-3 border-t border-rule pt-3">
      <div className="flex flex-col gap-1">
        <span className="font-sans text-sm font-semibold">{entry.handle}</span>
        <span className="font-serif text-sm text-ink-soft">
          {entry.title || entry.handle} · {entry.members} {entry.members === 1 ? 'member' : 'members'} · listed{' '}
          <Age iso={entry.addedAt} />
          {entry.note && ` · ${entry.note}`}
        </span>
        <span className="font-sans text-xs text-ink-faint">{siteFollow[entry.siteFollow]}</span>
      </div>
      <Button variant="quiet" onClick={() => void onRemove()}>
        Remove
      </Button>
    </li>
  )
}

function AddDirectoryForm({ onAdd }: { onAdd: (community: string, note: string) => Promise<void> }) {
  const ids = { community: useId(), note: useId() }
  const [community, setCommunity] = useState('')
  const [note, setNote] = useState('')
  return (
    <form
      className="flex max-w-(--container-measure) flex-col gap-3 border-t border-rule pt-4"
      onSubmit={(e) => {
        e.preventDefault()
        void onAdd(community.trim(), note.trim()).then(() => {
          setCommunity('')
          setNote('')
        })
      }}
    >
      <h3 className="font-display text-lg tracking-tight">List a community</h3>
      <div className="flex flex-col gap-2">
        <label htmlFor={ids.community} className={label}>
          Its address
        </label>
        <Input
          id={ids.community}
          value={community}
          required
          maxLength={300}
          onChange={(e) => setCommunity(e.target.value)}
          placeholder="woodworking@lemmy.example"
        />
        <p className={hint}>A name@domain handle or a full community URL.</p>
      </div>
      <div className="flex flex-col gap-2">
        <label htmlFor={ids.note} className={label}>
          Note
        </label>
        <Input id={ids.note} value={note} maxLength={500} onChange={(e) => setNote(e.target.value)} />
      </div>
      <Button type="submit" className="w-fit">
        List it
      </Button>
    </form>
  )
}

function PeerRow({
  peer,
  onBlock,
  onUnblock,
}: {
  peer: PeerView
  onBlock: (reason: string) => Promise<void>
  onUnblock: () => Promise<void>
}) {
  const ids = { reason: useId() }
  const [open, setOpen] = useState(false)
  const [reason, setReason] = useState('')
  const health = peer.failingSince
    ? `No answer for ${relative(peer.failingSince).replace(/ ago$/, '')}`
    : peer.lastOkAt
      ? `Last answered ${relative(peer.lastOkAt)}`
      : 'Never answered'
  return (
    <li className="flex flex-col gap-3 border-t border-rule pt-3">
      <div className="flex flex-wrap items-baseline justify-between gap-3">
        <div className="flex flex-col gap-1">
          <span className="font-sans text-sm font-semibold">{peer.domain}</span>
          <span className="font-serif text-sm text-ink-soft">
            {health}
            {peer.software && ` · ${peer.software} ${peer.version}`}
            {peer.blocked && ` · blocked${peer.reason ? `: ${peer.reason}` : ''}`}
          </span>
        </div>
        {peer.blocked ? (
          <Button variant="quiet" onClick={() => void onUnblock()}>
            Unblock
          </Button>
        ) : (
          <Button variant="quiet" onClick={() => setOpen(!open)}>
            Block
          </Button>
        )}
      </div>
      {open && !peer.blocked && (
        <form
          className="flex flex-wrap items-end gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            void onBlock(reason.trim()).then(() => {
              setOpen(false)
              setReason('')
            })
          }}
        >
          <label htmlFor={ids.reason} className="sr-only">
            Why block {peer.domain}?
          </label>
          <Input
            id={ids.reason}
            value={reason}
            maxLength={500}
            autoFocus
            onChange={(e) => setReason(e.target.value)}
            placeholder="Why (shown to admins)"
            className="w-64"
          />
          <Button type="submit">Block</Button>
        </form>
      )}
    </li>
  )
}

function BlockPeerForm({ onBlock }: { onBlock: (domain: string, reason: string) => Promise<void> }) {
  const ids = { domain: useId(), reason: useId() }
  const [domain, setDomain] = useState('')
  const [reason, setReason] = useState('')
  return (
    <form
      className="flex max-w-(--container-measure) flex-col gap-3 border-t border-rule pt-4"
      onSubmit={(e) => {
        e.preventDefault()
        void onBlock(domain.trim(), reason.trim()).then(() => {
          setDomain('')
          setReason('')
        })
      }}
    >
      <h3 className="font-display text-lg tracking-tight">Block a server</h3>
      <div className="flex flex-col gap-2">
        <label htmlFor={ids.domain} className={label}>
          Its domain
        </label>
        <Input
          id={ids.domain}
          value={domain}
          required
          maxLength={300}
          onChange={(e) => setDomain(e.target.value)}
          placeholder="example.org"
        />
        <p className={hint}>The domain only, with no scheme or path.</p>
      </div>
      <div className="flex flex-col gap-2">
        <label htmlFor={ids.reason} className={label}>
          Why
        </label>
        <Input id={ids.reason} value={reason} maxLength={500} onChange={(e) => setReason(e.target.value)} />
      </div>
      <Button type="submit" className="w-fit">
        Block it
      </Button>
    </form>
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

function TrustForm({ onSet }: { onSet: (handle: string, trusted: boolean) => Promise<void> }) {
  const id = useId()
  const [handle, setHandle] = useState('')
  const [busy, setBusy] = useState(false)

  async function run(trusted: boolean) {
    setBusy(true)
    await onSet(handle.trim().replace(/^[@!]/, ''), trusted)
    setBusy(false)
    setHandle('')
  }

  return (
    <div className="flex max-w-(--container-measure) flex-col gap-2 border-t border-rule pt-4">
      <label htmlFor={id} className={label}>
        Their handle
      </label>
      <Input
        id={id}
        value={handle}
        onChange={(e) => setHandle(e.target.value)}
        placeholder="ada or sam@lemmy.example"
        className="max-w-sm"
      />
      <div className="flex flex-wrap gap-2">
        <Button type="button" className="w-fit" disabled={busy || handle.trim() === ''} onClick={() => void run(true)}>
          Promote to established
        </Button>
        <Button
          type="button"
          variant="outline"
          className="w-fit"
          disabled={busy || handle.trim() === ''}
          onClick={() => void run(false)}
        >
          Demote to new
        </Button>
      </div>
    </div>
  )
}

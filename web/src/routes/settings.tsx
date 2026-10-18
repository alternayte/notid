import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { createFileRoute, Link } from '@tanstack/react-router'
import { type FormEvent, useEffect, useId, useState } from 'react'
import { errorMessage } from '~/api/errors'
import { deleteMeMutation, meOptions, personOptions, updateProfileMutation } from '~/api/gen/@tanstack/react-query.gen'
import { Button } from '~/ui/primitives/button'
import { Checkbox } from '~/ui/primitives/checkbox'
import { Input } from '~/ui/primitives/input'
import { Textarea } from '~/ui/primitives/textarea'
import { Page } from './__root'

export const Route = createFileRoute('/settings')({
  head: () => ({ meta: [{ title: 'Settings · Open Communities' }] }),
  component: Settings,
})

const label = 'font-sans text-sm font-semibold'
const hint = 'font-sans text-xs text-ink-faint'

function Settings() {
  const { data: me, isPending } = useQuery({ ...meOptions(), retry: false })

  if (!me && !isPending) {
    return (
      <Page>
        <h1 className="font-display text-4xl tracking-tight">Settings</h1>
        <Link to="/login" className="w-fit font-sans text-sm font-semibold text-mark">
          Sign in to change your settings
        </Link>
      </Page>
    )
  }
  // The profile carries the bio, which /api/me does not.
  if (!me)
    return (
      <Page>
        <p className="font-sans text-sm text-ink-faint">Loading…</p>
      </Page>
    )
  return <Form handle={me.handle} />
}

function Form({ handle }: { handle: string }) {
  const queryClient = useQueryClient()
  const { data: p, isError } = useQuery({ ...personOptions({ path: { handle } }), retry: false })
  const update = useMutation(updateProfileMutation())
  const [displayName, setDisplayName] = useState('')
  const [bio, setBio] = useState('')
  const [bot, setBot] = useState(false)
  const [status, setStatus] = useState<{ ok: boolean; message: string } | null>(null)
  const [busy, setBusy] = useState(false)
  const ids = { displayName: useId(), bio: useId() }

  // Start from the stored profile, once it arrives.
  useEffect(() => {
    if (!p) return
    setDisplayName(p.displayName)
    setBio(p.bio)
    setBot(p.bot)
  }, [p])

  async function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    setBusy(true)
    setStatus(null)
    try {
      await update.mutateAsync({ body: { displayName: displayName.trim(), bio, bot } })
      for (const key of [meOptions().queryKey, personOptions({ path: { handle } }).queryKey]) {
        void queryClient.invalidateQueries({ queryKey: key })
      }
      setStatus({ ok: true, message: 'Saved.' })
    } catch (err) {
      setStatus({ ok: false, message: errorMessage(err, 'Your changes were not saved. Try again.') })
    } finally {
      setBusy(false)
    }
  }

  return (
    <Page>
      <div className="flex flex-col gap-1">
        <h1 className="font-display text-4xl tracking-tight">Settings</h1>
        <p className="font-sans text-xs text-ink-faint">
          Your profile is public at{' '}
          <Link to="/u/$handle" params={{ handle }} className="font-semibold text-mark">
            /u/{handle}
          </Link>
          .
        </p>
      </div>
      {isError ? (
        <p role="alert" className="font-sans text-sm text-danger">
          Your profile did not load. Reload the page to try again.
        </p>
      ) : (
        <form onSubmit={submit} className="flex max-w-(--container-measure) flex-col gap-6">
          <div className="flex flex-col gap-2">
            <label htmlFor={ids.displayName} className={label}>
              Display name
            </label>
            <Input
              id={ids.displayName}
              maxLength={100}
              value={displayName}
              onChange={(e) => setDisplayName(e.target.value)}
              aria-describedby="display-name-hint"
            />
            <p id="display-name-hint" className={hint}>
              How your name appears on posts and comments. Your address stays {handle}.
            </p>
          </div>
          <div className="flex flex-col gap-2">
            <label htmlFor={ids.bio} className={label}>
              Bio
            </label>
            <Textarea
              id={ids.bio}
              rows={5}
              maxLength={2000}
              value={bio}
              onChange={(e) => setBio(e.target.value)}
              aria-describedby="bio-hint"
            />
            <p id="bio-hint" className={hint}>
              Shown on your profile. Markdown works.
            </p>
          </div>
          <fieldset className="flex flex-col gap-2">
            <legend className={label}>Automated account</legend>
            <div className="flex items-center gap-2">
              <Checkbox id="bot" checked={bot} onCheckedChange={(v) => setBot(v === true)} />
              <label htmlFor="bot" className="font-sans text-sm">
                This account posts as a bot
              </label>
            </div>
            <p className={hint}>
              Bot accounts carry a Bot label, and peers show them as automated. Only a bot account can create API keys.
            </p>
          </fieldset>
          <div className="flex flex-wrap items-center gap-4">
            <Button type="submit" disabled={busy}>
              {busy ? 'Saving…' : 'Save changes'}
            </Button>
            {status && (
              <p
                role={status.ok ? 'status' : 'alert'}
                className={`font-sans text-sm ${status.ok ? 'text-ok' : 'text-danger'}`}
              >
                {status.message}
              </p>
            )}
          </div>
        </form>
      )}
      <DeleteAccount />
    </Page>
  )
}

// DeleteAccount erases the person's profile and content. Two steps, because the wrong press cannot be undone.
function DeleteAccount() {
  const queryClient = useQueryClient()
  const del = useMutation(deleteMeMutation())
  const [confirming, setConfirming] = useState(false)
  const [message, setMessage] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  async function remove() {
    setBusy(true)
    setMessage(null)
    try {
      await del.mutateAsync({})
      queryClient.clear()
      // A full load drops every piece of client state the deleted account left behind.
      window.location.replace('/')
    } catch (err) {
      setMessage(errorMessage(err, 'Your account was not deleted. Try again.'))
      setBusy(false)
    }
  }

  return (
    <section className="flex max-w-(--container-measure) flex-col gap-3 border-t border-rule pt-6">
      <h2 className="font-display text-lg tracking-tight">Delete your account</h2>
      <p className="font-serif text-sm text-ink-soft">
        Your posts and comments are deleted, and your profile is erased. A community you alone own blocks this until
        someone else owns it. This cannot be undone.
      </p>
      {confirming ? (
        <div className="flex flex-wrap items-center gap-3">
          <Button type="button" disabled={busy} onClick={() => void remove()} className="bg-danger text-paper">
            {busy ? 'Deleting…' : 'Yes, delete my account and its posts'}
          </Button>
          <Button type="button" variant="quiet" disabled={busy} onClick={() => setConfirming(false)}>
            Keep my account
          </Button>
        </div>
      ) : (
        <Button type="button" variant="outline" onClick={() => setConfirming(true)} className="w-fit">
          Delete my account
        </Button>
      )}
      {message && (
        <p role="alert" className="font-sans text-sm text-danger">
          {message}
        </p>
      )}
    </section>
  )
}

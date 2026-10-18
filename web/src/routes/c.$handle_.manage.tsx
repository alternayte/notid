import { useMutation, useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import { createFileRoute, Link, notFound } from '@tanstack/react-router'
import { type FormEvent, useId, useState } from 'react'
import { errorMessage } from '~/api/errors'
import {
  communityOptions,
  communityQueryKey,
  setModeratorMutation,
  updateCommunityMutation,
  uploadImageMutation,
} from '~/api/gen/@tanstack/react-query.gen'
import { accentAttr } from '~/communities/accents'
import { SettingsForm } from '~/communities/SettingsForm'
import { Button } from '~/ui/primitives/button'
import { Input } from '~/ui/primitives/input'
import { Page } from './__root'

export const Route = createFileRoute('/c/$handle_/manage')({
  loader: async ({ context, params }) => {
    try {
      return await context.queryClient.ensureQueryData(communityOptions({ path: { handle: params.handle } }))
    } catch {
      throw notFound()
    }
  },
  head: ({ loaderData }) => ({
    meta: [{ title: `Manage ${loaderData?.title || loaderData?.name || 'community'} · Open Communities` }],
  }),
  component: Manage,
})

function Manage() {
  const { handle } = Route.useParams()
  const { data: c } = useSuspenseQuery(communityOptions({ path: { handle } }))
  const update = useMutation(updateCommunityMutation())
  const upload = useMutation(uploadImageMutation())
  const queryClient = useQueryClient()
  const allowed = c.local && (c.membership?.role === 'owner' || c.membership?.role === 'moderator')

  return (
    <div data-accent={accentAttr(c.theme)}>
      <Page>
        <div className="flex flex-col gap-2">
          <Link to="/c/$handle" params={{ handle }} className="w-fit font-sans text-sm text-ink-soft hover:text-ink">
            ← {c.title || c.name}
          </Link>
          <h1 className="font-display text-4xl tracking-tight">Manage the community</h1>
        </div>
        {!allowed ? (
          <p className="font-sans text-sm text-ink-soft">Only the community's owners and moderators can change it.</p>
        ) : (
          <>
            <SettingsForm
              initial={c}
              initialBannerUrl={c.bannerUrl}
              upload={async (file) => {
                try {
                  return await upload.mutateAsync({ body: { file } })
                } catch (err) {
                  throw new Error(errorMessage(err, 'The banner did not upload. Try again.'))
                }
              }}
              submitLabel="Save changes"
              busyLabel="Saving…"
              onSubmit={async (values) => {
                try {
                  const d = await update.mutateAsync({ path: { handle }, body: values })
                  queryClient.setQueryData(communityQueryKey({ path: { handle } }), d)
                } catch (err) {
                  return errorMessage(err, 'The changes could not be saved. Try again.')
                }
              }}
            />
            <Moderators handle={handle} />
          </>
        )}
      </Page>
    </div>
  )
}

// Moderators appoints a member or returns a moderator to member. Owners only.
function Moderators({ handle }: { handle: string }) {
  const queryClient = useQueryClient()
  const setRole = useMutation(setModeratorMutation())
  const id = useId()
  const [person, setPerson] = useState('')
  const [busy, setBusy] = useState(false)
  const [status, setStatus] = useState<{ ok: boolean; message: string } | null>(null)

  async function run(role: 'moderator' | 'member') {
    setBusy(true)
    setStatus(null)
    try {
      await setRole.mutateAsync({ path: { handle }, body: { actor: person.trim().replace(/^[@!]/, ''), role } })
      await queryClient.invalidateQueries({ queryKey: communityQueryKey({ path: { handle } }) })
      setStatus({ ok: true, message: role === 'moderator' ? 'They are a moderator now.' : 'They are a member again.' })
      setPerson('')
    } catch (err) {
      setStatus({ ok: false, message: errorMessage(err, 'That did not work. Try again.') })
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className="flex max-w-(--container-measure) flex-col gap-2 border-t border-rule pt-6">
      <h2 className="font-display text-2xl tracking-tight">Moderators</h2>
      <p className="font-serif text-sm text-ink-soft">
        Only an owner appoints moderators. The community's log records each appointment, and the person's server hears
        about it.
      </p>
      <label htmlFor={id} className="font-sans text-sm font-semibold">
        Their handle
      </label>
      <Input
        id={id}
        value={person}
        onChange={(e) => setPerson(e.target.value)}
        placeholder="ada or sam@lemmy.example"
        className="max-w-sm"
      />
      <div className="flex flex-wrap items-center gap-3">
        <Button
          type="button"
          className="w-fit"
          disabled={busy || person.trim() === ''}
          onClick={() => void run('moderator')}
        >
          Make moderator
        </Button>
        <Button
          type="button"
          variant="outline"
          className="w-fit"
          disabled={busy || person.trim() === ''}
          onClick={() => void run('member')}
        >
          Make member again
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
    </section>
  )
}

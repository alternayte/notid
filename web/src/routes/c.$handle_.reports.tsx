import { useMutation, useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import { createFileRoute, Link, notFound } from '@tanstack/react-router'
import { errorMessage } from '~/api/errors'
import {
  communityOptions,
  removeObjectMutation,
  reportQueueOptions,
  reportQueueQueryKey,
  resolveReportMutation,
} from '~/api/gen/@tanstack/react-query.gen'
import type { Report } from '~/api/gen/types.gen'
import { accentAttr } from '~/communities/accents'
import { Age } from '~/discussions/Age'
import { Button } from '~/ui/primitives/button'
import { Page } from './__root'

export const Route = createFileRoute('/c/$handle_/reports')({
  loader: async ({ context, params }) => {
    const c = await context.queryClient
      .ensureQueryData(communityOptions({ path: { handle: params.handle } }))
      .catch(() => {
        throw notFound()
      })
    await context.queryClient.prefetchQuery({
      ...reportQueueOptions({ path: { handle: params.handle } }),
      retry: false,
    })
    return c.title || c.name
  },
  head: ({ loaderData }) => ({ meta: [{ title: `Reports in ${loaderData ?? 'a community'} · Open Communities` }] }),
  component: Reports,
})

function Reports() {
  const { handle } = Route.useParams()
  const { data: c } = useSuspenseQuery(communityOptions({ path: { handle } }))
  const { data, isError, isPending } = useSuspenseQuery(reportQueueOptions({ path: { handle } }))
  const queryClient = useQueryClient()
  const resolve = useMutation(resolveReportMutation())
  const remove = useMutation(removeObjectMutation())
  void isError

  const refresh = () => queryClient.invalidateQueries({ queryKey: reportQueueQueryKey({ path: { handle } }) })

  async function act(fn: () => Promise<unknown>) {
    try {
      await fn()
      await refresh()
    } catch (err) {
      alert(errorMessage(err, 'That did not work. Try again.'))
    }
  }

  return (
    <div data-accent={accentAttr(c.theme)}>
      <Page>
        <div className="flex flex-col gap-2">
          <Link to="/c/$handle" params={{ handle }} className="w-fit font-sans text-sm text-ink-soft hover:text-ink">
            ← {c.title || c.name}
          </Link>
          <h1 className="font-display text-4xl tracking-tight">Reports</h1>
          <p className="max-w-(--container-measure) font-serif text-sm text-ink-soft">
            Reports are private. They never appear in the community or in the moderation log, and reporters are shown
            only here.
          </p>
        </div>
        {isPending ? (
          <p className="font-sans text-sm text-ink-faint">Loading…</p>
        ) : data.reports.length === 0 ? (
          <section className="flex max-w-(--container-measure) flex-col gap-2 rounded-sm bg-paper-sunk p-4">
            <h2 className="font-display text-lg tracking-tight">Nothing to review</h2>
            <p className="font-serif text-sm text-ink-soft">No open reports right now.</p>
          </section>
        ) : (
          <ol className="flex max-w-(--container-measure) flex-col gap-6">
            {data.reports.map((r) => (
              <ReportRow key={r.id} report={r} handle={handle} act={act} resolve={resolve} remove={remove} />
            ))}
          </ol>
        )}
      </Page>
    </div>
  )
}

function ReportRow({
  report: r,
  handle,
  act,
  resolve,
  remove,
}: {
  report: Report
  handle: string
  act: (fn: () => Promise<unknown>) => Promise<void>
  resolve: ReturnType<
    typeof useMutation<unknown, unknown, { path: { handle: string; id: number }; body: { upheld: boolean } }>
  >
  remove: ReturnType<typeof useMutation<unknown, unknown, { body: { object: string } }>>
}) {
  const target = r.postApId ?? ''
  const label = r.postTitle ?? r.commentBody ?? 'a comment'
  return (
    <li className="flex flex-col gap-3 border-t border-rule pt-4">
      <p className="font-sans text-xs text-ink-faint">
        Reported by <span className="font-semibold text-ink-soft">{r.reporter}</span> · <Age iso={r.createdAt} />
      </p>
      <p className="font-serif text-base">{r.reason}</p>
      <p className="font-serif text-sm text-ink-soft">
        On:{' '}
        {r.postRef ? (
          <Link
            to="/c/$handle/p/$ref"
            params={{ handle, ref: r.postRef }}
            className="font-sans font-semibold text-mark"
          >
            {label}
          </Link>
        ) : (
          <Link to="/c/$handle/p/$ref" params={{ handle, ref: '' }} className="hidden" aria-hidden="true" tabIndex={-1}>
            {label}
          </Link>
        )}
        {r.commentBody && <span className="block border-l border-rule pl-3 italic">“{r.commentBody}”</span>}
      </p>
      <div className="flex flex-wrap gap-2">
        {target && (
          <Button variant="outline" onClick={() => act(() => remove.mutateAsync({ body: { object: target } }))}>
            Remove the content
          </Button>
        )}
        <Button onClick={() => act(() => resolve.mutateAsync({ path: { handle, id: r.id }, body: { upheld: true } }))}>
          Uphold
        </Button>
        <Button
          variant="quiet"
          onClick={() => act(() => resolve.mutateAsync({ path: { handle, id: r.id }, body: { upheld: false } }))}
        >
          Dismiss
        </Button>
      </div>
    </li>
  )
}

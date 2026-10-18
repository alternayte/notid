import { useQuery } from '@tanstack/react-query'
import { moderationLogOptions } from '~/api/gen/@tanstack/react-query.gen'
import type { Detail } from '~/api/gen/types.gen'
import { Age } from '~/discussions/Age'
import { Kicker } from '~/ui/Kicker'
import { fromRenderer, Rendered } from '~/ui/RenderedHtml'

// About is a community's sidebar: description, rules, moderators and appeals contact.
export function About({ community: c }: { community: Detail }) {
  return (
    <aside aria-label={`About ${c.title || c.name}`} className="flex flex-col gap-6 lg:sticky lg:top-8 lg:self-start">
      <section className="flex flex-col gap-3 border-t-2 border-mark pt-3">
        <Kicker>About</Kicker>
        {c.descriptionHtml ? (
          <Rendered html={fromRenderer(c.descriptionHtml)} className="prose-news text-sm" />
        ) : (
          <p className="font-serif text-sm text-ink-faint">No description yet.</p>
        )}
      </section>
      {c.rules.length > 0 && (
        <section className="flex flex-col gap-3 border-t border-rule pt-3">
          <Kicker>Rules</Kicker>
          <ol className="flex list-decimal flex-col gap-2 pl-5 font-serif text-sm marker:font-sans marker:text-ink-faint">
            {c.rules.map((r) => (
              <li key={r}>{r}</li>
            ))}
          </ol>
        </section>
      )}
      {c.moderators.length > 0 && (
        <section className="flex flex-col gap-3 border-t border-rule pt-3">
          <Kicker>Moderators</Kicker>
          <ul className="flex flex-col gap-1 font-sans text-sm">
            {c.moderators.map((m) => (
              <li key={m.handle}>
                {m.displayName || m.handle}
                {m.role === 'owner' && <span className="text-ink-faint"> · owner</span>}
              </li>
            ))}
          </ul>
        </section>
      )}
      <ModerationLog handle={c.handle} />
      {c.appealsContact && (
        <section className="flex flex-col gap-2 border-t border-rule pt-3">
          <Kicker>Appeals</Kicker>
          <p className="font-sans text-sm text-ink-soft">
            To appeal a moderator's decision, contact <span className="text-ink">{c.appealsContact}</span>.
          </p>
        </section>
      )}
    </aside>
  )
}

// ModerationLog shows the community's public record of moderator actions.
function ModerationLog({ handle }: { handle: string }) {
  const { data, isError } = useQuery({ ...moderationLogOptions({ path: { handle } }), retry: false })
  if (isError) {
    return (
      <section className="flex flex-col gap-2 border-t border-rule pt-3">
        <Kicker>Moderation log</Kicker>
        <p className="font-sans text-sm text-danger">The log did not load.</p>
      </section>
    )
  }
  const entries = data?.entries ?? []
  return (
    <section className="flex flex-col gap-3 border-t border-rule pt-3">
      <Kicker>Moderation log</Kicker>
      {entries.length === 0 ? (
        <p className="font-serif text-sm text-ink-faint">No moderator actions yet.</p>
      ) : (
        <ol className="flex flex-col gap-2 font-sans text-xs text-ink-soft">
          {entries.slice(0, 10).map((e) => (
            <li key={e.id}>
              <span className="font-semibold text-ink">{actionLabel(e.action)}</span>
              {e.target && <span className="text-ink-faint"> · {e.target}</span>}
              {e.reason && <span className="block text-ink-faint">{e.reason}</span>}
              <span className="block text-ink-faint">
                {e.moderator} · <Age iso={e.createdAt} />
              </span>
            </li>
          ))}
        </ol>
      )}
    </section>
  )
}

const actionLabels: Record<string, string> = {
  remove_post: 'Post removed',
  restore_post: 'Post restored',
  remove_comment: 'Reply removed',
  restore_comment: 'Reply restored',
  lock: 'Post locked',
  unlock: 'Post unlocked',
  ban: 'Ban',
  unban: 'Ban lifted',
  report_upheld: 'Report upheld',
  report_dismissed: 'Report dismissed',
}

function actionLabel(action: string): string {
  return actionLabels[action] ?? action.replaceAll('_', ' ')
}

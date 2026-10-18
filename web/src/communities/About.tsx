import type { Detail } from '~/api/gen/types.gen'
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

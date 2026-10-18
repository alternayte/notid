import { createFileRoute } from '@tanstack/react-router'
import { useEffect, useState } from 'react'
import { Composer } from '~/ui/Composer'
import { JoinButton } from '~/ui/JoinButton'
import { Kicker } from '~/ui/Kicker'
import { PostCard } from '~/ui/PostCard'
import { fromRenderer } from '~/ui/RenderedHtml'
import { ThemeSwitch } from '~/ui/ThemeSwitch'
import { type CommentNode, Thread } from '~/ui/Thread'
import { VoteControl } from '~/ui/VoteControl'

// The design specimen: every token and core component on one page, with the
// sample text the design is judged against.
export const Route = createFileRoute('/specimen')({
  head: () => ({ meta: [{ title: 'Specimen · Open Communities' }, { name: 'robots', content: 'noindex' }] }),
  component: Specimen,
})

const wait = (ms: number) => new Promise((r) => setTimeout(r, ms))

function Specimen() {
  const [failing, setFailing] = useState(false)
  const [reduced, setReduced] = useState(false)

  useEffect(() => {
    document.documentElement.dataset.motion = reduced ? 'reduce' : ''
  }, [reduced])

  // Sample network calls: a short delay, and a failure when the switch is on.
  const send = async () => {
    await wait(450)
    if (failing) throw new Error('offline')
  }

  return (
    <div className="mx-auto flex max-w-(--container-page) flex-col gap-12 px-5 pb-24 sm:px-8">
      <header className="flex flex-col gap-4 border-b-2 border-rule-strong pt-8 pb-4">
        <div className="flex flex-wrap items-center justify-between gap-4 font-sans text-xs text-ink-soft">
          <span>Design specimen · Saturday edition</span>
          <div className="flex flex-wrap items-center gap-3">
            <label className="flex items-center gap-2">
              <input
                type="checkbox"
                checked={failing}
                onChange={(e) => setFailing(e.target.checked)}
                className="accent-mark"
              />
              Make actions fail
            </label>
            <label className="flex items-center gap-2">
              <input
                type="checkbox"
                checked={reduced}
                onChange={(e) => setReduced(e.target.checked)}
                className="accent-mark"
              />
              Reduce motion
            </label>
            <ThemeSwitch />
          </div>
        </div>
        <h1 className="font-serif text-4xl font-semibold tracking-tight [font-variation-settings:'opsz'_72]">
          Open Communities
        </h1>
      </header>

      <Swatches />
      <TypeScale />

      <div className="grid gap-12 lg:grid-cols-[minmax(0,1fr)_18rem]">
        <main className="flex flex-col gap-10">
          <section aria-labelledby="feed" className="flex flex-col gap-4">
            <SectionTitle id="feed">Home feed · New</SectionTitle>
            <Composer
              community="woodworking"
              preview={async () => {
                await wait(200)
                return fromRenderer(
                  '<p>A <strong>sharp</strong> chisel and a <em>slow</em> hand.</p><blockquote><p>Measure twice.</p></blockquote>',
                )
              }}
            />
            <div>
              <PostCard
                community="woodworking"
                title="I cut my first hand-cut dovetail joint after six weekends of trying"
                href="#thread"
                author="ada"
                age="2 hours ago"
                comments={14}
                standfirst="The trick that finally worked was paring to the baseline in two passes instead of one. Pictures and the saw I used are in the thread."
                votes={<VoteControl score={128} label="the dovetail post" send={send} />}
              />
              <PostCard
                community="woodworking@lemmy.world"
                title="The best inexpensive smoothing plane in 2026, tested on oak, maple and pine"
                href="#thread"
                author="grain"
                authorHost="lemmy.world"
                age="5 hours ago"
                comments={41}
                link="https://example.org/planes-2026"
                image={<Engraving />}
                votes={<VoteControl score={57} initial={1} label="the plane test" send={send} />}
              />
              <PostCard
                community="woodworking"
                title="Weekly tools thread: what did you buy, sharpen or break?"
                href="#thread"
                author="shopbot"
                bot
                age="1 day ago"
                comments={3}
                votes={<VoteControl score={-2} initial={-1} label="the weekly thread" send={send} />}
              />
            </div>
          </section>

          <section id="thread" aria-labelledby="thread-title" className="flex flex-col gap-6">
            <SectionTitle id="thread-title">Thread</SectionTitle>
            <article className="flex flex-col gap-4">
              <Kicker>woodworking</Kicker>
              <h2 className="font-serif text-3xl font-semibold tracking-tight text-balance [font-variation-settings:'opsz'_60]">
                I cut my first hand-cut dovetail joint after six weekends of trying
              </h2>
              <p className="font-sans text-xs text-ink-faint">
                by <span className="font-semibold text-ink-soft">ada</span> · 2 hours ago
              </p>
              <div className="prose-news">
                <p>
                  The trick that finally worked was paring to the baseline in two passes instead of one. The first pass
                  takes the waste to within a millimetre; the second pass is all wrist.
                </p>
                <blockquote>
                  <p>A sharp chisel forgives a slow hand. A dull one forgives nothing.</p>
                </blockquote>
                <p>
                  I used a <a href="#thread">Japanese dozuki saw</a> and a 12 mm bench chisel, honed to 6000 grit.
                </p>
              </div>
              <VoteControl score={128} label="the dovetail post" send={send} />
            </article>
            <div id="comments" className="border-t border-rule pt-6">
              <Thread
                comments={comments}
                renderVotes={(c) => <VoteControl score={c.score} label={`${c.author}'s comment`} send={send} />}
              />
            </div>
          </section>
        </main>

        <aside className="flex flex-col gap-6 lg:sticky lg:top-8 lg:self-start">
          <CommunityPanel
            name="woodworking"
            about="Hand tools, joinery and the patience between them. Be kind; show your work."
            members="12,408"
            send={send}
          />
          <div data-community-accent="" style={{ ['--community-accent' as string]: 'oklch(0.55 0.12 150)' }}>
            <CommunityPanel
              name="birdwatching"
              about="Sightings, field notes and blurry photos, all welcome. This community lives on PieFed and sets its own green."
              members="3,112"
              remote="piefed.social"
              send={send}
            />
          </div>
          <EmptyState />
        </aside>
      </div>
    </div>
  )
}

function SectionTitle({ id, children }: { id: string; children: React.ReactNode }) {
  return (
    <h2
      id={id}
      className="border-b border-rule-strong pb-1 font-sans text-xs font-bold uppercase tracking-kicker text-ink"
    >
      {children}
    </h2>
  )
}

function CommunityPanel({
  name,
  about,
  members,
  remote,
  send,
}: {
  name: string
  about: string
  members: string
  remote?: string
  send: () => Promise<void>
}) {
  return (
    <section aria-label={`About ${name}`} className="flex flex-col gap-3 border-t-2 border-mark pt-3">
      <Kicker>{remote ? `Community on ${remote}` : 'Community'}</Kicker>
      <h3 className="font-serif text-2xl font-semibold">{name}</h3>
      <p className="font-serif text-sm text-ink-soft">{about}</p>
      <p className="font-sans text-xs text-ink-faint">{members} members</p>
      <JoinButton
        community={name}
        send={async (join) => {
          await send()
          return join ? (remote ? 'pending' : 'in') : 'out'
        }}
      />
    </section>
  )
}

function EmptyState() {
  return (
    <section className="flex flex-col gap-2 rounded-sm bg-paper-sunk p-4">
      <h3 className="font-serif text-lg font-semibold">Nothing here yet</h3>
      <p className="font-serif text-sm text-ink-soft">
        Your home feed fills with posts from the communities you join, newest first.
      </p>
      <a href="#feed" className="w-fit font-sans text-sm font-semibold text-mark">
        Find communities
      </a>
    </section>
  )
}

const swatches = [
  ['paper', 'Paper'],
  ['paper-sunk', 'Paper, sunk'],
  ['ink', 'Ink'],
  ['ink-soft', 'Ink, soft'],
  ['ink-faint', 'Ink, faint'],
  ['rule', 'Rule'],
  ['mark', 'Mark'],
  ['mark-wash', 'Mark wash'],
  ['cool', 'Down vote'],
  ['ok', 'OK'],
  ['danger', 'Danger'],
  ['warn', 'Warning'],
] as const

function Swatches() {
  return (
    <section aria-labelledby="colour" className="flex flex-col gap-4">
      <SectionTitle id="colour">Colour</SectionTitle>
      <ul className="grid grid-cols-3 gap-4 sm:grid-cols-6">
        {swatches.map(([token, name]) => (
          <li key={token} className="flex flex-col gap-1 font-sans text-xs">
            <span className="h-12 rounded-sm border border-rule" style={{ background: `var(--${token})` }} />
            <span className="font-semibold">{name}</span>
            <code className="text-ink-faint">--{token}</code>
          </li>
        ))}
      </ul>
    </section>
  )
}

function TypeScale() {
  return (
    <section aria-labelledby="type" className="flex flex-col gap-4">
      <SectionTitle id="type">Type · major third on 17 px</SectionTitle>
      <div className="flex flex-col gap-3">
        <p className="font-serif text-4xl font-semibold tracking-tight [font-variation-settings:'opsz'_72]">
          Communities that can leave
        </p>
        <p className="font-serif text-2xl font-semibold [font-variation-settings:'opsz'_48]">
          Headlines in Newsreader, set tight
        </p>
        <p className="font-serif text-lg italic text-ink-soft">
          A standfirst in italic, for the sentence that sells the story.
        </p>
        <p className="prose-news">
          Reading text in Newsreader at its text optical size: generous, warm, and quiet enough to read a long thread
          without fatigue.
        </p>
        <p className="font-sans text-sm">Furniture in Source Sans 3 — bylines, counts, buttons and labels.</p>
        <Kicker>Kicker · section label</Kicker>
      </div>
    </section>
  )
}

const hatches = Array.from({ length: 12 }, (_, i) => 26 + i * 9)

// An engraved-plate placeholder drawn in ink, standing in for a post image.
function Engraving() {
  return (
    <svg
      viewBox="0 0 160 120"
      role="img"
      aria-label="Sketch of a smoothing plane"
      className="w-full rounded-xs bg-paper-sunk text-ink-soft"
    >
      <g fill="none" stroke="currentColor" strokeWidth="1.2">
        <path d="M20 84h120l-12-22H44z" />
        <path d="M60 62l10-26h24l6 26" />
        <circle cx="82" cy="30" r="8" />
        {hatches.map((x) => (
          <path key={x} d={`M${x} 88l6 10`} opacity="0.5" />
        ))}
      </g>
    </svg>
  )
}

const comments: CommentNode[] = [
  {
    id: '1',
    author: 'grain',
    authorHost: 'lemmy.world',
    age: '1 hour ago',
    score: 24,
    body: 'Two passes is the whole secret. The tails look crisp. What angle did you mark them at?',
    replies: [
      {
        id: '2',
        author: 'ada',
        age: '52 minutes ago',
        score: 11,
        body: '1:7, with a sliding bevel. It felt steep at first, but it hides small gaps in softwood.',
        replies: [
          {
            id: '3',
            author: 'marquetry',
            authorHost: 'piefed.social',
            age: '40 minutes ago',
            score: 4,
            body: 'Same here. For oak I go to 1:8.',
            replies: [],
          },
        ],
      },
    ],
  },
  {
    id: '4',
    author: 'sawdust',
    age: '30 minutes ago',
    score: 2,
    body: 'Saving this for next weekend. Thanks for writing it up properly.',
    replies: [],
  },
]

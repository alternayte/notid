import { useId, useState } from 'react'
import { Rendered, type RenderedHtml } from './RenderedHtml'
import { Tabs } from './Tabs'

type Kind = 'text' | 'link' | 'image'
type Pane = 'write' | 'preview'

type Props = {
  community: string
  // Returns sanitized HTML from the server's Markdown renderer.
  preview: (markdown: string) => Promise<RenderedHtml>
}

// The composer starts as one line and unfolds when the reader starts a post.
export function Composer({ community, preview }: Props) {
  const [open, setOpen] = useState(false)
  const [kind, setKind] = useState<Kind>('text')
  const [pane, setPane] = useState<Pane>('write')
  const [body, setBody] = useState('')
  const [html, setHtml] = useState<RenderedHtml | null>(null)
  const ids = { title: useId(), body: useId(), url: useId() }

  async function showPane(next: Pane) {
    setPane(next)
    if (next === 'preview') setHtml(await preview(body))
  }

  return (
    <section aria-label={`New post in ${community}`} className="rounded-sm border border-rule bg-paper-raised">
      {!open ? (
        <button
          type="button"
          onClick={() => setOpen(true)}
          className="flex w-full items-center gap-3 px-4 py-3 text-left font-serif text-lg text-ink-faint hover:text-ink-soft"
        >
          <span className="grid size-7 place-items-center rounded-pill bg-mark font-sans text-base text-mark-ink">
            +
          </span>
          Start a post in {community}
        </button>
      ) : (
        <form
          className="flex flex-col gap-4 p-4 motion-safe:animate-[unfold_var(--dur-open)_var(--ease-out)]"
          onSubmit={(e) => e.preventDefault()}
        >
          <Tabs
            label="Post type"
            value={kind}
            onChange={setKind}
            options={[
              { value: 'text', label: 'Text' },
              { value: 'link', label: 'Link' },
              { value: 'image', label: 'Image' },
            ]}
          />
          <label htmlFor={ids.title} className="sr-only">
            Title
          </label>
          <input
            id={ids.title}
            className="field font-display text-lg sm:text-xl tracking-tight"
            placeholder="Title"
            maxLength={200}
            // biome-ignore lint/a11y/noAutofocus: the composer opens on the reader's request
            autoFocus
          />
          {kind === 'link' && (
            <>
              <label htmlFor={ids.url} className="sr-only">
                Link
              </label>
              <input id={ids.url} type="url" className="field font-sans" placeholder="https://" />
            </>
          )}
          {kind === 'image' && (
            <div className="grid place-items-center rounded-sm border border-dashed border-rule px-4 py-8 font-sans text-sm text-ink-soft">
              Drop a JPEG, PNG or WebP here. Location data is removed before it is stored.
            </div>
          )}
          <div className="flex flex-col gap-2">
            <Tabs
              label="Body"
              value={pane}
              onChange={showPane}
              options={[
                { value: 'write', label: 'Write' },
                { value: 'preview', label: 'Preview' },
              ]}
            />
            {pane === 'write' ? (
              <>
                <label htmlFor={ids.body} className="sr-only">
                  Body, in Markdown
                </label>
                <textarea
                  id={ids.body}
                  rows={6}
                  value={body}
                  onChange={(e) => setBody(e.target.value)}
                  className="field font-serif"
                  placeholder="Say more. Markdown works: **bold**, _italic_, [links](https://…), > quotes."
                />
              </>
            ) : (
              <div className="prose-news min-h-36 rounded-sm bg-paper-sunk p-4">
                {html ? <Rendered html={html} /> : <p className="text-ink-faint">Nothing to preview yet.</p>}
              </div>
            )}
          </div>
          <div className="flex items-center justify-end gap-2">
            <button type="button" className="btn-quiet" onClick={() => setOpen(false)}>
              Discard
            </button>
            <button type="submit" className="btn-primary">
              Post
            </button>
          </div>
        </form>
      )}
    </section>
  )
}

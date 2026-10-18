import { useId, useState } from 'react'
import { Button } from './primitives/button'
import { Input } from './primitives/input'
import { Tabs, TabsContent, TabsList, TabsTrigger } from './primitives/tabs'
import { Textarea } from './primitives/textarea'
import { Rendered, type RenderedHtml } from './RenderedHtml'

export type Kind = 'text' | 'link' | 'image'

export type Draft = { kind: Kind; title: string; body: string; url: string }

type Props = {
  community: string
  // Returns sanitized HTML from the server's Markdown renderer.
  preview: (markdown: string) => Promise<RenderedHtml>
  // Sends the post; a rejected promise keeps the draft and shows the error's message.
  send?: (draft: Draft) => Promise<void>
  kinds?: Kind[]
  startOpen?: boolean
}

// The composer starts as one line and unfolds when the reader starts a post.
export function Composer({ community, preview, send, kinds = ['text', 'link', 'image'], startOpen = false }: Props) {
  const [open, setOpen] = useState(startOpen)
  const [kind, setKind] = useState<Kind>(kinds[0] ?? 'text')
  const [title, setTitle] = useState('')
  const [body, setBody] = useState('')
  const [url, setUrl] = useState('')
  const [html, setHtml] = useState<RenderedHtml | null>(null)
  const [previewFailed, setPreviewFailed] = useState(false)
  const [busy, setBusy] = useState(false)
  const [failed, setFailed] = useState<string | null>(null)
  const ids = { title: useId(), body: useId(), url: useId() }

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
          onSubmit={async (e) => {
            e.preventDefault()
            if (!send) return
            setBusy(true)
            setFailed(null)
            try {
              await send({ kind, title: title.trim(), body, url: kind === 'link' ? url.trim() : '' })
            } catch (err) {
              setFailed(err instanceof Error && err.message ? err.message : 'Post not sent.')
            } finally {
              setBusy(false)
            }
          }}
        >
          <Tabs value={kind} onValueChange={(k) => setKind(k as Kind)}>
            {kinds.length > 1 && (
              <TabsList aria-label="Post type">
                {kinds.includes('text') && <TabsTrigger value="text">Text</TabsTrigger>}
                {kinds.includes('link') && <TabsTrigger value="link">Link</TabsTrigger>}
                {kinds.includes('image') && <TabsTrigger value="image">Image</TabsTrigger>}
              </TabsList>
            )}
            <label htmlFor={ids.title} className="sr-only">
              Title
            </label>
            <Input
              id={ids.title}
              className="font-display text-lg tracking-tight sm:text-xl"
              placeholder="Title"
              maxLength={200}
              required
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              autoFocus
            />
            <TabsContent value="link">
              <label htmlFor={ids.url} className="sr-only">
                Link
              </label>
              <Input
                id={ids.url}
                type="url"
                placeholder="https://"
                maxLength={2000}
                required={kind === 'link'}
                value={url}
                onChange={(e) => setUrl(e.target.value)}
              />
            </TabsContent>
            <TabsContent value="image">
              <div className="grid place-items-center rounded-sm border border-dashed border-rule px-4 py-8 font-sans text-sm text-ink-soft">
                Drop a JPEG, PNG or WebP here. Location data is removed before it is stored.
              </div>
            </TabsContent>
          </Tabs>
          <Tabs
            defaultValue="write"
            onValueChange={async (pane) => {
              if (pane !== 'preview') return
              setPreviewFailed(false)
              try {
                setHtml(await preview(body))
              } catch {
                setPreviewFailed(true)
              }
            }}
          >
            <TabsList aria-label="Body">
              <TabsTrigger value="write">Write</TabsTrigger>
              <TabsTrigger value="preview">Preview</TabsTrigger>
            </TabsList>
            <TabsContent value="write">
              <label htmlFor={ids.body} className="sr-only">
                Body, in Markdown
              </label>
              <Textarea
                id={ids.body}
                rows={6}
                maxLength={40000}
                value={body}
                onChange={(e) => setBody(e.target.value)}
                placeholder="Say more. Markdown works: **bold**, _italic_, [links](https://…), > quotes."
              />
            </TabsContent>
            <TabsContent value="preview" className="prose-news min-h-36 max-w-none rounded-sm bg-paper-sunk p-4">
              {previewFailed ? (
                <p role="alert" className="text-danger">
                  The preview did not load. Switch to Write and back to try again.
                </p>
              ) : html ? (
                <Rendered html={html} />
              ) : (
                <p className="text-ink-faint">Nothing to preview yet.</p>
              )}
            </TabsContent>
          </Tabs>
          {failed && (
            <p role="alert" className="font-sans text-sm text-danger">
              {failed} Your draft is kept; try again.
            </p>
          )}
          <div className="flex items-center justify-end gap-2">
            {!startOpen && (
              <Button type="button" variant="quiet" onClick={() => setOpen(false)}>
                Discard
              </Button>
            )}
            <Button type="submit" disabled={busy}>
              {busy ? 'Posting…' : 'Post'}
            </Button>
          </div>
        </form>
      )}
    </section>
  )
}

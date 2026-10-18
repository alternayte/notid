import { useId, useState } from 'react'
import { Button } from './primitives/button'
import { Input } from './primitives/input'
import { Tabs, TabsContent, TabsList, TabsTrigger } from './primitives/tabs'
import { Textarea } from './primitives/textarea'
import { Rendered, type RenderedHtml } from './RenderedHtml'

type Props = {
  community: string
  // Returns sanitized HTML from the server's Markdown renderer.
  preview: (markdown: string) => Promise<RenderedHtml>
}

// The composer starts as one line and unfolds when the reader starts a post.
export function Composer({ community, preview }: Props) {
  const [open, setOpen] = useState(false)
  const [body, setBody] = useState('')
  const [html, setHtml] = useState<RenderedHtml | null>(null)
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
          onSubmit={(e) => e.preventDefault()}
        >
          <Tabs defaultValue="text">
            <TabsList aria-label="Post type">
              <TabsTrigger value="text">Text</TabsTrigger>
              <TabsTrigger value="link">Link</TabsTrigger>
              <TabsTrigger value="image">Image</TabsTrigger>
            </TabsList>
            <label htmlFor={ids.title} className="sr-only">
              Title
            </label>
            <Input
              id={ids.title}
              className="font-display text-lg tracking-tight sm:text-xl"
              placeholder="Title"
              maxLength={200}
              autoFocus
            />
            <TabsContent value="link">
              <label htmlFor={ids.url} className="sr-only">
                Link
              </label>
              <Input id={ids.url} type="url" placeholder="https://" />
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
              if (pane === 'preview') setHtml(await preview(body))
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
                value={body}
                onChange={(e) => setBody(e.target.value)}
                placeholder="Say more. Markdown works: **bold**, _italic_, [links](https://…), > quotes."
              />
            </TabsContent>
            <TabsContent value="preview" className="prose-news min-h-36 max-w-none rounded-sm bg-paper-sunk p-4">
              {html ? <Rendered html={html} /> : <p className="text-ink-faint">Nothing to preview yet.</p>}
            </TabsContent>
          </Tabs>
          <div className="flex items-center justify-end gap-2">
            <Button type="button" variant="quiet" onClick={() => setOpen(false)}>
              Discard
            </Button>
            <Button type="submit">Post</Button>
          </div>
        </form>
      )}
    </section>
  )
}

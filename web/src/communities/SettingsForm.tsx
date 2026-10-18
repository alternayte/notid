import { type FormEvent, useState } from 'react'
import type { Theme } from '~/api/gen/types.gen'
import { cn } from '~/ui/cn'
import { Button } from '~/ui/primitives/button'
import { Checkbox } from '~/ui/primitives/checkbox'
import { Input } from '~/ui/primitives/input'
import { Textarea } from '~/ui/primitives/textarea'
import { type Accent, accents } from './accents'

export type SettingsValues = {
  title: string
  description: string
  rules: string[]
  theme: Theme
  appealsContact: string
  nsfw: boolean
}

type Props = {
  initial: SettingsValues
  submitLabel: string
  busyLabel: string
  // Returns an error message, or nothing on success. form carries extra fields passed as children.
  onSubmit: (values: SettingsValues, form: FormData) => Promise<string | undefined>
  children?: React.ReactNode
}

const label = 'font-sans text-sm font-semibold'
const hint = 'font-sans text-xs text-ink-faint'

// SettingsForm edits what owners and moderators control: title, description, rules, accent, appeals contact.
export function SettingsForm({ initial, submitLabel, busyLabel, onSubmit, children }: Props) {
  const [accent, setAccent] = useState<Accent>(initial.theme.accent ?? 'vermilion')
  const [nsfw, setNsfw] = useState(initial.nsfw)
  const [busy, setBusy] = useState(false)
  const [status, setStatus] = useState<{ ok: boolean; message: string } | null>(null)

  async function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const form = new FormData(e.currentTarget)
    setBusy(true)
    setStatus(null)
    const error = await onSubmit(
      {
        title: String(form.get('title') ?? '').trim(),
        description: String(form.get('description') ?? ''),
        rules: String(form.get('rules') ?? '')
          .split('\n')
          .map((r) => r.trim())
          .filter(Boolean),
        theme: { accent },
        appealsContact: String(form.get('appealsContact') ?? '').trim(),
        nsfw,
      },
      form,
    )
    setBusy(false)
    setStatus(error ? { ok: false, message: error } : { ok: true, message: 'Saved.' })
  }

  return (
    <form onSubmit={submit} className="flex max-w-(--container-measure) flex-col gap-6">
      {children}
      <div className="flex flex-col gap-2">
        <label htmlFor="title" className={label}>
          Title
        </label>
        <Input id="title" name="title" required maxLength={100} defaultValue={initial.title} />
      </div>
      <div className="flex flex-col gap-2">
        <label htmlFor="description" className={label}>
          Description
        </label>
        <Textarea
          id="description"
          name="description"
          rows={4}
          maxLength={10000}
          defaultValue={initial.description}
          aria-describedby="description-hint"
        />
        <p id="description-hint" className={hint}>
          What the community is for. Markdown works.
        </p>
      </div>
      <div className="flex flex-col gap-2">
        <label htmlFor="rules" className={label}>
          Rules
        </label>
        <Textarea
          id="rules"
          name="rules"
          rows={5}
          defaultValue={initial.rules.join('\n')}
          aria-describedby="rules-hint"
        />
        <p id="rules-hint" className={hint}>
          One rule per line, in order. At most 15.
        </p>
      </div>
      <fieldset className="flex flex-col gap-2">
        <legend className={cn(label, 'mb-2')}>Accent</legend>
        <div className="flex flex-wrap gap-2">
          {accents.map((a) => (
            <label
              key={a.value}
              data-accent={a.value === 'vermilion' ? undefined : a.value}
              className="flex cursor-pointer items-center gap-2 rounded-pill py-1.5 pr-3 pl-1.5 font-sans text-sm shadow-[inset_0_0_0_1px_var(--rule)] has-checked:shadow-[inset_0_0_0_2px_var(--mark)] has-focus-visible:outline-2 has-focus-visible:outline-focus"
            >
              <input
                type="radio"
                name="accent"
                value={a.value}
                checked={accent === a.value}
                onChange={() => setAccent(a.value)}
                className="sr-only"
              />
              <span aria-hidden="true" className="size-5 rounded-pill bg-mark" />
              {a.label}
            </label>
          ))}
        </div>
        <p className={hint}>Marks, links and buttons on the community's pages take this colour.</p>
      </fieldset>
      <div className="flex flex-col gap-2">
        <label htmlFor="appealsContact" className={label}>
          Appeals contact
        </label>
        <Input
          id="appealsContact"
          name="appealsContact"
          maxLength={200}
          defaultValue={initial.appealsContact}
          placeholder="An email address or a link"
          aria-describedby="appeals-hint"
        />
        <p id="appeals-hint" className={hint}>
          Shown on removal notices and the about panel, so members know where to appeal.
        </p>
      </div>
      <div className="flex items-center gap-2">
        <Checkbox id="nsfw" checked={nsfw} onCheckedChange={(v) => setNsfw(v === true)} />
        <label htmlFor="nsfw" className="font-sans text-sm">
          Adult content (18+)
        </label>
      </div>
      <div className="flex flex-wrap items-center gap-4">
        <Button type="submit" disabled={busy}>
          {busy ? busyLabel : submitLabel}
        </Button>
        {status && (
          <p
            role={status.ok ? 'status' : 'alert'}
            className={cn('font-sans text-sm', status.ok ? 'text-ok' : 'text-danger')}
          >
            {status.message}
          </p>
        )}
      </div>
    </form>
  )
}

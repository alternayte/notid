import type { Theme } from '~/api/gen/types.gen'

export type Accent = NonNullable<Theme['accent']>

export const accents: { value: Accent; label: string }[] = [
  { value: 'vermilion', label: 'Vermilion' },
  { value: 'moss', label: 'Moss' },
  { value: 'teal', label: 'Teal' },
  { value: 'cobalt', label: 'Cobalt' },
  { value: 'plum', label: 'Plum' },
  { value: 'ochre', label: 'Ochre' },
]

// accentAttr gives the data-accent value for a community's pages; vermilion is the site's own mark.
export function accentAttr(theme: Theme): Accent | undefined {
  return theme.accent && theme.accent !== 'vermilion' ? theme.accent : undefined
}

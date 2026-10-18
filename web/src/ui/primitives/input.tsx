import type * as React from 'react'
import { cn } from '~/ui/cn'

export const fieldClass =
  'w-full rounded-sm border border-rule bg-paper-raised px-3 py-2.5 text-base text-ink transition-[border-color,box-shadow] duration-(--dur-press) ease-(--ease-out) placeholder:text-ink-faint focus-visible:border-mark focus-visible:shadow-[0_0_0_3px_var(--mark-wash)] focus-visible:outline-none disabled:opacity-50 aria-invalid:border-danger'

export function Input({ className, type, ...props }: React.ComponentProps<'input'>) {
  return <input type={type} data-slot="input" className={cn(fieldClass, className)} {...props} />
}

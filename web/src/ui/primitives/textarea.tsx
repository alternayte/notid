import type * as React from 'react'
import { cn } from '~/ui/cn'
import { fieldClass } from './input'

export function Textarea({ className, ...props }: React.ComponentProps<'textarea'>) {
  return <textarea data-slot="textarea" className={cn(fieldClass, 'min-h-20', className)} {...props} />
}

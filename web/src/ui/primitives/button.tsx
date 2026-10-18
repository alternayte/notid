import { cva, type VariantProps } from 'class-variance-authority'
import { Slot } from 'radix-ui'
import type * as React from 'react'
import { cn } from '~/ui/cn'

export const buttonVariants = cva(
  'inline-flex shrink-0 items-center justify-center gap-2 rounded-pill font-sans font-semibold whitespace-nowrap transition-[background-color,color,box-shadow,transform,filter] duration-(--dur-press) ease-(--ease-out) active:scale-95 disabled:pointer-events-none disabled:opacity-50 [&_svg]:pointer-events-none [&_svg]:shrink-0',
  {
    variants: {
      variant: {
        primary: 'bg-mark text-mark-ink hover:brightness-110',
        outline: 'text-ink shadow-[inset_0_0_0_1px_var(--rule-strong)] hover:bg-paper-sunk',
        quiet: 'text-ink-soft hover:bg-paper-sunk hover:text-ink',
        link: 'h-auto px-0 text-mark underline-offset-2 hover:underline active:scale-100',
      },
      size: {
        sm: 'h-8 px-3 text-xs',
        md: 'h-9 px-4 text-sm',
        icon: 'size-8',
      },
    },
    defaultVariants: { variant: 'primary', size: 'md' },
  },
)

export function Button({
  className,
  variant,
  size,
  asChild = false,
  ...props
}: React.ComponentProps<'button'> & VariantProps<typeof buttonVariants> & { asChild?: boolean }) {
  const Comp = asChild ? Slot.Root : 'button'
  return <Comp data-slot="button" className={cn(buttonVariants({ variant, size }), className)} {...props} />
}

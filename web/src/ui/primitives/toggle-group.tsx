import { ToggleGroup as ToggleGroupPrimitive } from 'radix-ui'
import type * as React from 'react'
import { cn } from '~/ui/cn'

// A segmented control: a pill row where the chosen item is set in ink.
export function ToggleGroup({ className, ...props }: React.ComponentProps<typeof ToggleGroupPrimitive.Root>) {
  return (
    <ToggleGroupPrimitive.Root
      data-slot="toggle-group"
      className={cn('flex w-fit rounded-pill border border-rule p-0.5 font-sans text-xs', className)}
      {...props}
    />
  )
}

export function ToggleGroupItem({ className, ...props }: React.ComponentProps<typeof ToggleGroupPrimitive.Item>) {
  return (
    <ToggleGroupPrimitive.Item
      data-slot="toggle-group-item"
      className={cn(
        'rounded-pill px-3 py-1 font-semibold text-ink-soft transition-colors duration-(--dur-press) hover:text-ink data-[state=on]:bg-ink data-[state=on]:text-paper',
        className,
      )}
      {...props}
    />
  )
}

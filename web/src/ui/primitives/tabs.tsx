import { Tabs as TabsPrimitive } from 'radix-ui'
import type * as React from 'react'
import { cn } from '~/ui/cn'

export function Tabs({ className, ...props }: React.ComponentProps<typeof TabsPrimitive.Root>) {
  return <TabsPrimitive.Root data-slot="tabs" className={cn('flex flex-col gap-3', className)} {...props} />
}

export function TabsList({ className, ...props }: React.ComponentProps<typeof TabsPrimitive.List>) {
  return (
    <TabsPrimitive.List
      data-slot="tabs-list"
      className={cn('flex gap-6 border-b border-rule font-sans text-sm', className)}
      {...props}
    />
  )
}

// The active tab carries a vermilion underline that grows in from the centre.
export function TabsTrigger({ className, ...props }: React.ComponentProps<typeof TabsPrimitive.Trigger>) {
  return (
    <TabsPrimitive.Trigger
      data-slot="tabs-trigger"
      className={cn(
        'relative -mb-px pb-2 font-semibold text-ink-faint transition-colors duration-(--dur-press) hover:text-ink-soft data-[state=active]:text-ink',
        'after:absolute after:inset-x-0 after:bottom-0 after:h-0.5 after:origin-center after:scale-x-0 after:bg-mark after:transition-transform after:duration-(--dur-move) after:ease-(--ease-out) data-[state=active]:after:scale-x-100',
        className,
      )}
      {...props}
    />
  )
}

export function TabsContent({ className, ...props }: React.ComponentProps<typeof TabsPrimitive.Content>) {
  return <TabsPrimitive.Content data-slot="tabs-content" className={cn('outline-none', className)} {...props} />
}

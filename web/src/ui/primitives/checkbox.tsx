import { CheckIcon } from 'lucide-react'
import { Checkbox as CheckboxPrimitive } from 'radix-ui'
import type * as React from 'react'
import { cn } from '~/ui/cn'

export function Checkbox({ className, ...props }: React.ComponentProps<typeof CheckboxPrimitive.Root>) {
  return (
    <CheckboxPrimitive.Root
      data-slot="checkbox"
      className={cn(
        'peer grid size-4 shrink-0 place-items-center rounded-xs border border-rule-strong bg-paper-raised transition-colors duration-(--dur-press) data-[state=checked]:border-mark data-[state=checked]:bg-mark data-[state=checked]:text-mark-ink disabled:opacity-50',
        className,
      )}
      {...props}
    >
      <CheckboxPrimitive.Indicator data-slot="checkbox-indicator">
        <CheckIcon className="size-3" strokeWidth={3} />
      </CheckboxPrimitive.Indicator>
    </CheckboxPrimitive.Root>
  )
}

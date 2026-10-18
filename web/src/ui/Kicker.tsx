import type { ReactNode } from 'react'

// A section label in the newspaper sense: small capitals above a headline.
export function Kicker({ children }: { children: ReactNode }) {
  return <span className="font-sans text-2xs font-semibold uppercase tracking-kicker text-mark">{children}</span>
}

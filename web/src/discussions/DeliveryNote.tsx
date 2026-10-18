import type { Delivery } from '~/api/gen/types.gen'
import { splitHandle } from './AuthorLink'

// DeliveryNote tells the author how far their content got to a community on
// another site. The API sends a delivery state only to the author.
export function DeliveryNote({ delivery, community }: { delivery?: Delivery; community: string }) {
  if (!delivery) return null
  const host = splitHandle(community).host
  if (!host) return null
  if (delivery.state === 'pending') {
    return (
      <p role="status" className="font-sans text-xs text-ink-faint">
        Sending to {host}…
      </p>
    )
  }
  if (delivery.state === 'rejected') {
    return (
      <p role="status" className="rounded-sm bg-mark-wash px-3 py-2 font-sans text-xs text-ink-soft">
        <span className="font-semibold text-ink">{host} did not accept this.</span>{' '}
        {delivery.error ? `It answered ${delivery.error}.` : 'That site may limit what it accepts.'} Your copy stays
        here.
      </p>
    )
  }
  return (
    <p role="status" className="font-sans text-xs text-ink-faint">
      Sent to {host}.
    </p>
  )
}

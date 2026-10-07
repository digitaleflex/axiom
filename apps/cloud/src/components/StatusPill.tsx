/**
 * Canonical status pill (Design DNA §4). Status colors are allowed here; the
 * label is always visible text, so status is never conveyed by color alone.
 * Unknown values render raw (never invented).
 */
export function StatusPill({ status, label }: { status?: string | null; label?: string }) {
  const value = status ?? 'unknown'
  return (
    <span className="status-pill" data-status={value}>
      <span className="status-pill__dot" aria-hidden="true" />
      {label ?? status ?? 'Unknown'}
    </span>
  )
}

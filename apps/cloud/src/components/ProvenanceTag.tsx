import type { Provenance } from '../api/types'

const LABELS: Record<Provenance, string> = {
  detected: 'Detected',
  manifest: 'axiom.yaml',
  default: 'Default',
  override: 'Override',
}

/**
 * Provenance tag (analysis §4). Every profile value displays exactly one
 * provenance. A Default is never labelled Detected.
 */
export function ProvenanceTag({ provenance }: { provenance?: Provenance | null }) {
  const value = provenance ?? 'default'
  return (
    <span className={`provenance-tag provenance-tag--${value}`} data-provenance={value}>
      {LABELS[value]}
    </span>
  )
}

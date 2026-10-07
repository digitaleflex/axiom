import { CONFIDENCE_BAND_LABELS, confidenceBand, type ConfidenceBand } from '../workflow/confidence'

const ICONS: Record<ConfidenceBand, string> = {
  high: '✓',
  medium: '◐',
  low: '▲',
}

/**
 * Confidence badge (analysis §2.2). Text + icon only — high confidence uses
 * no status color (confidence is not deployment health). The exact
 * percentage is L3, never shown here.
 */
export function ConfidenceBadge({ confidence }: { confidence: number | undefined | null }) {
  const band = confidenceBand(confidence)
  return (
    <span className={`confidence-badge confidence-badge--${band}`} data-band={band}>
      <span className="confidence-badge__icon" aria-hidden="true">
        {ICONS[band]}
      </span>
      {CONFIDENCE_BAND_LABELS[band]}
    </span>
  )
}

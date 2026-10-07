/**
 * Confidence bands (analysis §2.2).
 *
 * The Engine returns confidence in [0, 1]; the UI never shows a bare number
 * at L1/L2 — it shows a band label. Thresholds are presentation defaults.
 * High confidence deliberately uses no status color.
 */
export type ConfidenceBand = 'high' | 'medium' | 'low'

export const CONFIDENCE_BAND_LABELS: Record<ConfidenceBand, string> = {
  high: 'Detected',
  medium: 'Likely — review',
  low: 'Uncertain — confirm',
}

export function confidenceBand(confidence: number | undefined | null): ConfidenceBand {
  if (confidence === undefined || confidence === null || Number.isNaN(confidence)) return 'low'
  if (confidence >= 0.9) return 'high'
  if (confidence >= 0.6) return 'medium'
  return 'low'
}

/** Whether a band requires user confirmation before continuing (blocking). */
export function bandRequiresConfirmation(band: ConfidenceBand): boolean {
  return band === 'low'
}

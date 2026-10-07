import { useState } from 'react'

const MAX_LENGTH = 24

function truncateValue(value: string, mode: 'middle' | 'start' | 'end'): string {
  if (value.length <= MAX_LENGTH) return value
  if (mode === 'start') return `…${value.slice(-MAX_LENGTH)}`
  if (mode === 'end') return `${value.slice(0, MAX_LENGTH)}…`
  const keep = Math.ceil(MAX_LENGTH / 2)
  return `${value.slice(0, keep)}…${value.slice(-keep)}`
}

/**
 * Technical value (accessibility §8): mono, truncated with the full value
 * available on hover, and copyable. Opaque IDs are never parsed.
 */
export function TechnicalValue({
  value,
  truncate = 'middle',
  copyable = true,
}: {
  value: string
  truncate?: 'middle' | 'start' | 'end'
  copyable?: boolean
}) {
  const [copied, setCopied] = useState(false)

  const onCopy = async () => {
    try {
      await navigator.clipboard.writeText(value)
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch {
      // clipboard unavailable — ignore
    }
  }

  return (
    <span className="technical-value">
      <span className="technical-value__text mono" title={value}>
        {truncateValue(value, truncate)}
      </span>
      {copyable && (
        <button type="button" className="technical-value__copy" onClick={() => void onCopy()} aria-label={`Copy ${value}`}>
          {copied ? '✓' : '⧉'}
        </button>
      )}
    </span>
  )
}

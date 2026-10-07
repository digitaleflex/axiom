import { useState } from 'react'

interface ConfirmButtonProps {
  label: string
  /** Label shown on the second (arming) click; the action fires on the next one. */
  confirmLabel?: string
  onConfirm: () => void
  className?: string
  disabled?: boolean
}

/**
 * Two-step destructive action (settings §4: destructive ops require explicit
 * confirmation). The first click only arms the action — the button relabels to
 * `confirmLabel` and a Cancel button appears. `onConfirm` fires only on the
 * second, explicit click; Cancel disarms without side effects.
 */
export function ConfirmButton({
  label,
  confirmLabel = 'Confirm?',
  onConfirm,
  className = 'btn btn--destructive',
  disabled = false,
}: ConfirmButtonProps) {
  const [armed, setArmed] = useState(false)

  if (armed) {
    return (
      <span className="confirm-button">
        <button
          type="button"
          className={className}
          onClick={() => {
            setArmed(false)
            onConfirm()
          }}
        >
          {confirmLabel}
        </button>
        <button type="button" className="btn btn--ghost btn--sm" onClick={() => setArmed(false)}>
          Cancel
        </button>
      </span>
    )
  }

  return (
    <button type="button" className={className} disabled={disabled} onClick={() => setArmed(true)}>
      {label}
    </button>
  )
}

import { useEffect, useRef, useState } from 'react'

/**
 * Deploy confirmation dialog (deployment-plan §8).
 *
 * Production requires typing the application name; Staging/Preview deploy
 * with a single click. The dialog is a modal: focus starts on the text
 * input, Escape cancels.
 */
export function DeployConfirmDialog({
  open,
  environment,
  applicationName,
  summary,
  rollbackLine,
  onConfirm,
  onCancel,
}: {
  open: boolean
  environment: string
  applicationName: string
  summary: string[]
  rollbackLine?: string
  onConfirm: () => void
  onCancel: () => void
}) {
  const [typed, setTyped] = useState('')
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (open) {
      setTyped('')
      // Focus starts on the text input (deployment-plan §10).
      setTimeout(() => inputRef.current?.focus(), 0)
    }
  }, [open])

  useEffect(() => {
    if (!open) return
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onCancel()
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [open, onCancel])

  if (!open) return null

  const requiresTyping = environment === 'production'
  const confirmed = !requiresTyping || typed.trim() === applicationName

  return (
    <div className="drawer-scrim" onClick={onCancel} aria-hidden="true">
      <div
        className="confirm-dialog"
        role="alertdialog"
        aria-modal="true"
        aria-labelledby="deploy-confirm-title"
        aria-describedby="deploy-confirm-summary"
        onClick={(event) => event.stopPropagation()}
      >
        <h2 className="confirm-dialog__title" id="deploy-confirm-title">
          Deploy {applicationName} to {environment}?
        </h2>
        <div className="confirm-dialog__summary" id="deploy-confirm-summary">
          {summary.map((line) => (
            <div key={line}>{line}</div>
          ))}
          {rollbackLine && <div className="muted">{rollbackLine}</div>}
        </div>
        {requiresTyping && (
          <div className="field">
            <label className="field__label" htmlFor="deploy-confirm-input">
              Type <span className="mono">{applicationName}</span> to confirm
            </label>
            <input
              id="deploy-confirm-input"
              className="field__input"
              ref={inputRef}
              value={typed}
              onChange={(event) => setTyped(event.target.value)}
              autoComplete="off"
            />
          </div>
        )}
        <div className="confirm-dialog__actions">
          <button type="button" className="btn btn--secondary" onClick={onCancel}>
            Cancel
          </button>
          <button
            type="button"
            className="btn btn--primary"
            disabled={!confirmed}
            onClick={onConfirm}
          >
            Deploy
          </button>
        </div>
      </div>
    </div>
  )
}

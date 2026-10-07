import { useEffect, useRef } from 'react'
import type { Evidence } from '../api/types'

/**
 * Evidence drawer (analysis §2.1, §3.6). Dialog semantics: focus trapped,
 * Escape closes, focus returns to the trigger. Evidence never dominates the
 * main reading path — it opens on demand.
 */
export function EvidenceDrawer({
  open,
  title,
  evidence,
  onClose,
}: {
  open: boolean
  title: string
  evidence: Evidence[] | undefined
  onClose: () => void
}) {
  const dialogRef = useRef<HTMLDivElement>(null)
  const closeRef = useRef<HTMLButtonElement>(null)

  useEffect(() => {
    if (!open) return
    closeRef.current?.focus()
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose()
      if (event.key === 'Tab' && dialogRef.current) {
        // Minimal focus trap: keep Tab cycling inside the dialog.
        const focusables = dialogRef.current.querySelectorAll<HTMLElement>(
          'button, [href], input, [tabindex]:not([tabindex="-1"])',
        )
        if (focusables.length === 0) return
        const first = focusables[0]
        const last = focusables[focusables.length - 1]
        if (event.shiftKey && document.activeElement === first) {
          event.preventDefault()
          last.focus()
        } else if (!event.shiftKey && document.activeElement === last) {
          event.preventDefault()
          first.focus()
        }
      }
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [open, onClose])

  if (!open) return null

  return (
    <div className="drawer-scrim" onClick={onClose} aria-hidden="true">
      <div
        className="evidence-drawer"
        role="dialog"
        aria-modal="true"
        aria-label={title}
        ref={dialogRef}
        onClick={(event) => event.stopPropagation()}
      >
        <div className="evidence-drawer__header">
          <h2 className="evidence-drawer__title">{title}</h2>
          <button type="button" className="btn btn--ghost" onClick={onClose} ref={closeRef}>
            Close
          </button>
        </div>
        <div className="evidence-drawer__body">
          {!evidence || evidence.length === 0 ? (
            <p className="muted">No evidence recorded for this finding.</p>
          ) : (
            <ol className="evidence-list">
              {evidence.map((item, index) => (
                <li className="evidence-list__item" key={`${item.path ?? 'evidence'}-${index}`}>
                  <div className="row" style={{ justifyContent: 'space-between' }}>
                    <span className="mono">{item.path ?? '—'}</span>
                    <span className={`evidence-effect evidence-effect--${item.effect}`}>
                      {item.effect === 'supports' ? '✓ Supports' : '✕ Conflicts'}
                    </span>
                  </div>
                  {item.lines && (
                    <div className="mono muted">
                      lines {item.lines}
                    </div>
                  )}
                  {item.explanation && <div>{item.explanation}</div>}
                  {item.rule && (
                    <div className="mono muted">
                      {item.rule}
                    </div>
                  )}
                </li>
              ))}
            </ol>
          )}
        </div>
      </div>
    </div>
  )
}

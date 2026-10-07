import { useEffect, useRef, useState } from 'react'
import { EmptyState } from '../system/EmptyState'

interface ActivityDrawerProps {
  open: boolean
  onClose: () => void
}

const FILTERS = ['All', 'In progress', 'Failed'] as const

/**
 * Workspace activity drawer placeholder (screens/shell §5.1).
 * The Engine workspace event stream is a contract gap (§10); until it lands the
 * drawer renders its empty state and the filter chrome.
 */
export function ActivityDrawer({ open, onClose }: ActivityDrawerProps) {
  const [filter, setFilter] = useState<(typeof FILTERS)[number]>('All')
  const closeRef = useRef<HTMLButtonElement>(null)

  useEffect(() => {
    if (!open) return
    closeRef.current?.focus()
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [open, onClose])

  if (!open) return null

  return (
    <>
      <div className="drawer-scrim" onClick={onClose} aria-hidden="true" />
      <aside className="activity-drawer" aria-label="Activity">
        <div className="activity-drawer__header">
          <h2 className="activity-drawer__title">Activity</h2>
          <button ref={closeRef} type="button" className="icon-button" aria-label="Close activity" onClick={onClose}>
            ×
          </button>
        </div>

        <div className="activity-drawer__filters" role="group" aria-label="Filter activity">
          {FILTERS.map((value) => (
            <button
              key={value}
              type="button"
              className={`filter-chip${filter === value ? ' filter-chip--active' : ''}`}
              aria-pressed={filter === value}
              onClick={() => setFilter(value)}
            >
              {value}
            </button>
          ))}
        </div>

        <div className="activity-drawer__body">
          <EmptyState
            variant="first-use"
            title="No activity yet"
            description="Deployment, GitHub and server events will appear here once the workspace event stream is available."
          />
        </div>
      </aside>
    </>
  )
}

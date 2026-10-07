import { useEffect, useRef, useState } from 'react'
import type { LogEntry } from '../api/types'

/**
 * Log viewer (logs §3–§5, deployment-progress §5.2).
 *
 * Monospace lines with timestamp, severity label, source and message.
 * Follow pins to the bottom; scrolling up pauses follow and shows a
 * "N new lines" jump button. Secrets are redacted by the Engine — the UI
 * renders redaction markers as-is and never attempts reconstruction.
 */
export function LogViewer({
  lines,
  loading,
  follow = true,
  onPause,
  onResume,
}: {
  lines: LogEntry[]
  loading?: boolean
  follow?: boolean
  onPause?: () => void
  onResume?: () => void
}) {
  const bodyRef = useRef<HTMLDivElement>(null)
  const [following, setFollowing] = useState(follow)
  const [pending, setPending] = useState(0)

  useEffect(() => {
    setFollowing(follow)
  }, [follow])

  useEffect(() => {
    const body = bodyRef.current
    if (!body || !following) return
    body.scrollTop = body.scrollHeight
    setPending(0)
  }, [lines, following])

  const onScroll = () => {
    const body = bodyRef.current
    if (!body) return
    const atBottom = body.scrollHeight - body.scrollTop - body.clientHeight < 24
    if (atBottom) {
      if (!following) onResume?.()
      setFollowing(true)
      setPending(0)
    } else if (following) {
      onPause?.()
      setFollowing(false)
      setPending((n) => n + 1)
    }
  }

  return (
    <div className="log-viewer">
      <div className="log-viewer__toolbar">
        <span className="muted">{lines.length} lines</span>
        {loading && <span className="muted">Loading…</span>}
        {!following && (
          <button
            type="button"
            className="btn btn--ghost btn--sm"
            onClick={() => {
              onResume?.()
              setFollowing(true)
              setPending(0)
            }}
          >
            ▼ {pending > 0 ? `${pending} new lines` : 'Jump to bottom'}
          </button>
        )}
      </div>
      <div className="log-viewer__body" ref={bodyRef} onScroll={onScroll} role="log" aria-live="off">
        {lines.length === 0 ? (
          <p className="muted">No log lines match the current filters.</p>
        ) : (
          lines.map((line) => (
            <div className="log-line" key={line.id}>
              <span className="log-line__time mono">{line.occurredAt ?? '—'}</span>
              <span className={`log-line__level log-line__level--${(line.level ?? 'INFO').toLowerCase()}`}>
                {(line.level ?? 'INFO').toUpperCase()}
              </span>
              {line.source && <span className="log-line__source">{line.source}</span>}
              <span className="log-line__message mono">{line.message}</span>
            </div>
          ))
        )}
      </div>
    </div>
  )
}

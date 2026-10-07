import { useState } from 'react'
import { useParams } from 'react-router-dom'
import { listLogs } from '../api/resources'
import type { LogEntry } from '../api/types'
import { LogViewer } from '../components/LogViewer'
import { ErrorPanel, InlineNotice, SkeletonLines } from '../components/system'
import { useAsync } from '../hooks/useAsync'

/**
 * Deployment Logs tab content (logs §1). Full log viewer with search,
 * severity filter and step filter. Secrets are redacted by the Engine —
 * the UI renders redaction markers as-is.
 */
export function DeploymentLogsContent() {
  const { deploymentId } = useParams<{ deploymentId: string }>()
  const [severity, setSeverity] = useState<string>('')

  const logsState = useAsync<{ items: LogEntry[]; nextCursor: string | null }>(
    (signal) =>
      deploymentId
        ? listLogs(deploymentId, { level: severity || undefined, limit: 200 }, { signal })
        : Promise.reject(new Error('missing id')),
    [deploymentId, severity],
  )

  const logs = logsState.data?.items ?? []

  return (
    <div className="content__body stack">
      <div className="row" style={{ gap: 8, alignItems: 'center' }}>
        <label htmlFor="log-severity" className="muted">
          Severity
        </label>
        <select
          id="log-severity"
          className="field__input"
          style={{ width: 'auto' }}
          value={severity}
          onChange={(event) => setSeverity(event.target.value)}
        >
          <option value="">All</option>
          <option value="debug">Debug and above</option>
          <option value="info">Info and above</option>
          <option value="warn">Warn and above</option>
          <option value="error">Error only</option>
        </select>
        <button type="button" className="btn btn--ghost btn--sm" onClick={logsState.reload}>
          Refresh
        </button>
      </div>

      {logsState.status === 'loading' && <SkeletonLines lines={6} />}

      {logsState.status === 'error' && logsState.error && (
        <ErrorPanel error={logsState.error} objectName="deployment logs" onRetry={logsState.reload} />
      )}

      {logsState.status === 'success' && logs.length === 0 && (
        <InlineNotice variant="info" title="No log lines">
          No log lines match the current filters for this deployment.
        </InlineNotice>
      )}

      {logsState.status === 'success' && logs.length > 0 && <LogViewer lines={logs} />}
    </div>
  )
}

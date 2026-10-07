import { ApiError, errorCopy } from '../../api/errors'

interface ErrorPanelProps {
  error: ApiError
  objectName?: string
  onRetry?: () => void
  retryLabel?: string
}

/**
 * Replaces a failed region only (docs/design/components/system §3.3).
 * Branches on `error.code`, never on message.
 */
export function ErrorPanel({ error, objectName = 'this', onRetry, retryLabel = 'Retry' }: ErrorPanelProps) {
  const isAuth = error.isUnauthorized

  return (
    <div className="error-panel" role="alert">
      <h2 className="error-panel__title">{isAuth ? 'Your session expired' : `Couldn't load ${objectName}`}</h2>
      <p className="error-panel__body">{errorCopy(error, objectName)}</p>

      {onRetry && !isAuth && (
        <button type="button" className="btn btn--secondary" onClick={onRetry}>
          {retryLabel}
        </button>
      )}

      <details className="error-panel__details">
        <summary>Details</summary>
        <div>code: {error.code}</div>
        {error.requestId && <div>requestId: {error.requestId}</div>}
        {error.status > 0 && <div>status: {error.status}</div>}
      </details>
    </div>
  )
}

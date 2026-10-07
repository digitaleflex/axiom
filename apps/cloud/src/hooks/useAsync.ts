import { useCallback, useEffect, useRef, useState } from 'react'
import { ApiError } from '../api/errors'

export type AsyncStatus = 'loading' | 'success' | 'error'

export interface AsyncState<T> {
  status: AsyncStatus
  data: T | null
  error: ApiError | null
}

export interface AsyncResult<T> extends AsyncState<T> {
  reload: () => void
}

/**
 * Runs an abortable async function and tracks loading/success/error.
 * Errors are normalized to {@link ApiError} so screens branch on codes.
 */
export function useAsync<T>(fn: (signal: AbortSignal) => Promise<T>, deps: unknown[]): AsyncResult<T> {
  const [state, setState] = useState<AsyncState<T>>({ status: 'loading', data: null, error: null })
  const [nonce, setNonce] = useState(0)
  const fnRef = useRef(fn)
  fnRef.current = fn

  useEffect(() => {
    const controller = new AbortController()
    let active = true
    setState({ status: 'loading', data: null, error: null })

    fnRef
      .current(controller.signal)
      .then((data) => {
        if (active) setState({ status: 'success', data, error: null })
      })
      .catch((cause: unknown) => {
        if (!active || controller.signal.aborted) return
        if (cause instanceof DOMException && cause.name === 'AbortError') return
        const error =
          cause instanceof ApiError
            ? cause
            : ApiError.network(cause instanceof Error ? cause.message : undefined)
        setState({ status: 'error', data: null, error })
      })

    return () => {
      active = false
      controller.abort()
    }
    // deps are supplied by the caller; nonce forces reload
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...deps, nonce])

  const reload = useCallback(() => setNonce((value) => value + 1), [])

  return { ...state, reload }
}

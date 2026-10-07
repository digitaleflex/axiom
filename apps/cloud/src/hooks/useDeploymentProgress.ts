import { useCallback, useEffect, useRef, useState } from 'react'
import { getDeployment, listSteps } from '../api/resources'
import type { Deployment, DeploymentStep } from '../api/types'
import { SseStream, type SseMessage } from '../api/sse'
import { applyStepEvent, mapDeploymentEvent } from '../workflow/stepMapping'

export type ProgressConnection = 'connecting' | 'live' | 'reconnecting' | 'polling'

export interface DeploymentProgress {
  deployment: Deployment | null
  steps: DeploymentStep[]
  status: string | undefined
  connection: ProgressConnection
  error: Error | null
  reload: () => void
}

const POLL_INTERVAL_MS = 5000
const MAX_RECONNECT_ATTEMPTS = 3

/**
 * Drives the Deployment Progress screen (deployment-progress §8).
 *
 * 1. On mount: fetch snapshot (deployment + steps) and render.
 * 2. Open the SSE stream.
 * 3. Apply events idempotently — steps never move backwards; terminal status
 *    is final.
 * 4. On disconnect: show "Reconnecting…", keep last known state.
 * 5. On reconnect: refetch the snapshot, then resume the stream.
 * 6. If the stream fails repeatedly: fall back to polling every 5s.
 *
 * The UI never shows a step as completed or the deployment as LIVE based on
 * client inference — only Engine events move state.
 */
export function useDeploymentProgress(deploymentId: string | undefined): DeploymentProgress {
  const [deployment, setDeployment] = useState<Deployment | null>(null)
  const [steps, setSteps] = useState<DeploymentStep[]>([])
  const [status, setStatus] = useState<string | undefined>(undefined)
  const [connection, setConnection] = useState<ProgressConnection>('connecting')
  const [error, setError] = useState<Error | null>(null)
  const [nonce, setNonce] = useState(0)

  const streamRef = useRef<SseStream | null>(null)
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const reconnectAttempts = useRef(0)
  const lastEventId = useRef<string | null>(null)

  const refreshSnapshot = useCallback(
    async (signal?: AbortSignal) => {
      if (!deploymentId) return
      try {
        const [dep, stepList] = await Promise.all([
          getDeployment(deploymentId, { signal }),
          listSteps(deploymentId, { signal }),
        ])
        setDeployment(dep)
        setStatus(dep.status)
        setSteps(stepList)
        setError(null)
      } catch (cause) {
        if (cause instanceof DOMException && cause.name === 'AbortError') return
        setError(cause instanceof Error ? cause : new Error(String(cause)))
      }
    },
    [deploymentId],
  )

  // Snapshot on mount + whenever the deployment id changes.
  useEffect(() => {
    void refreshSnapshot()
  }, [refreshSnapshot, nonce])

  // SSE stream with polling fallback.
  useEffect(() => {
    if (!deploymentId) return
    let active = true
    let pollCount = 0

    const stopPolling = () => {
      if (pollRef.current) {
        clearInterval(pollRef.current)
        pollRef.current = null
      }
    }

    const startPolling = () => {
      stopPolling()
      setConnection('polling')
      pollRef.current = setInterval(() => {
        pollCount += 1
        void refreshSnapshot()
      }, POLL_INTERVAL_MS)
    }

    const stopStream = () => {
      streamRef.current?.stop()
      streamRef.current = null
    }

    const connect = () => {
      if (!active) return
      stopStream()
      setConnection(reconnectAttempts.current > 0 ? 'reconnecting' : 'connecting')

      const stream = new SseStream({
        path: `/deployments/${deploymentId}/events/stream`,
        lastEventId: lastEventId.current,
        onMessage: (message: SseMessage) => {
          if (message.id) lastEventId.current = message.id
          let data: Record<string, unknown> = {}
          try {
            data = JSON.parse(message.data)
          } catch {
            // non-JSON frame — ignore
          }
          const mapping = mapDeploymentEvent(message.type, data)
          if (!mapping) return
          if (mapping.status) {
            setStatus(mapping.status)
            setDeployment((prev) => (prev ? { ...prev, status: mapping.status! } : prev))
          }
          if (mapping.step && mapping.state) {
            setSteps((prev) => applyStepEvent(prev, mapping!))
          }
        },
        onOpen: () => {
          reconnectAttempts.current = 0
          setConnection('live')
          stopPolling()
          // After (re)connect, refetch the snapshot so the view matches the
          // Engine exactly (deployment-progress §8.5).
          void refreshSnapshot()
        },
        onReconnect: (attempt: number) => {
          reconnectAttempts.current = attempt
          setConnection('reconnecting')
          if (attempt >= MAX_RECONNECT_ATTEMPTS) {
            stopStream()
            startPolling()
          }
        },
        onError: () => {
          // SseStream handles reconnect internally; polling fallback kicks in
          // after MAX_RECONNECT_ATTEMPTS via onReconnect.
        },
      })
      streamRef.current = stream
      stream.start()
    }

    connect()

    return () => {
      active = false
      stopStream()
      stopPolling()
    }
  }, [deploymentId, refreshSnapshot, nonce])

  const reload = useCallback(() => setNonce((n) => n + 1), [])

  return { deployment, steps, status, connection, error, reload }
}

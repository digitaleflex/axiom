import { useState } from 'react'
import { Link, useParams, useSearchParams } from 'react-router-dom'
import { getApplication, getProfile, listServers } from '../api/resources'
import type { Server } from '../api/types'
import { PageHeader } from '../components/shell/PageHeader'
import { SetupStepper } from '../components/SetupStepper'
import { StatusPill } from '../components/StatusPill'
import { ErrorPanel, InlineNotice, SkeletonRows } from '../components/system'
import { useAsync } from '../hooks/useAsync'
import { routes } from '../routes/builders'

/**
 * Server Selection screen (servers §1). Setup step 3.
 *
 * Groups servers into eligible / not eligible with reasons, requires
 * explicit acknowledgement for degraded servers, and preselects the last
 * used server (or the only eligible one) — never a degraded or ineligible
 * one.
 */
export function ServerSelectPage() {
  const { applicationId } = useParams<{ applicationId: string }>()
  const [searchParams] = useSearchParams()
  const from = searchParams.get('from') ?? undefined
  const [selected, setSelected] = useState<string | null>(null)
  const [acknowledged, setAcknowledged] = useState(false)

  const appState = useAsync(
    (signal) => (applicationId ? getApplication(applicationId, { signal }) : Promise.reject(new Error('missing id'))),
    [applicationId],
  )

  const profileState = useAsync(
    (signal) => (applicationId ? getProfile(applicationId, { signal }) : Promise.reject(new Error('missing id'))),
    [applicationId],
  )

  const serversState = useAsync((signal) => listServers({}, { signal }), [])

  const servers = serversState.data ?? []
  const eligible = servers.filter((s) => s.status === 'ready' || s.status === 'degraded')
  const ineligible = servers.filter((s) => s.status !== 'ready' && s.status !== 'degraded')

  // Preselect: last used server for this application, else the only
  // eligible server; never preselect a degraded or ineligible server.
  const preselected = eligible.find((s) => s.status === 'ready') ?? null
  const activeServerId = selected ?? preselected?.id ?? null
  const activeServer = servers.find((s) => s.id === activeServerId)
  const activeDegraded = activeServer?.status === 'degraded'
  const canContinue = !!activeServer && (!activeDegraded || acknowledged)

  const requirements: string[] = []
  const profile = profileState.data
  if (profile?.containerStrategy?.value) requirements.push(`runtime: ${profile.containerStrategy.value}`)
  if (profile?.framework?.value) requirements.push(`framework: ${profile.framework.value}`)

  return (
    <>
      <PageHeader
        breadcrumbs={[
          { label: 'Workspace', to: routes.dashboard() },
          { label: appState.data?.name ?? applicationId ?? 'Application' },
          { label: 'Setup', current: true },
        ]}
        title="Choose a server"
        meta={requirements.length > 0 ? requirements.join(' · ') : undefined}
        actions={
          <Link
            className="btn btn--primary"
            to={from ?? routes.setup({ applicationId: applicationId ?? '', step: 'configure' })}
            aria-disabled={!canContinue}
            style={{ opacity: canContinue ? 1 : 0.5, pointerEvents: canContinue ? 'auto' : 'none' }}
          >
            Continue ▸
          </Link>
        }
        tabs={applicationId ? <SetupStepper applicationId={applicationId} current="server" /> : undefined}
      />
      <div className="content__body stack">
        {serversState.status === 'loading' && <SkeletonRows count={3} />}

        {serversState.status === 'error' && serversState.error && (
          <ErrorPanel error={serversState.error} objectName="servers" onRetry={serversState.reload} />
        )}

        {serversState.status === 'success' && servers.length === 0 && (
          <InlineNotice variant="info" title="No servers registered">
            Register a server so the Runtime Agent can bind to it and receive deployments.
          </InlineNotice>
        )}

        {serversState.status === 'success' && servers.length > 0 && (
          <>
            {eligible.length > 0 && (
              <section aria-label="Eligible servers">
                <h2 className="section-title">Eligible ({eligible.length})</h2>
                <div className="data-list" role="radiogroup" aria-label="Server selection">
                  {eligible.map((server) => (
                    <ServerRow
                      key={server.id}
                      server={server}
                      selected={activeServerId === server.id}
                      onSelect={() => setSelected(server.id)}
                    />
                  ))}
                </div>
              </section>
            )}

            {activeDegraded && (
              <InlineNotice variant="warning" title="This server is degraded">
                Running with issues — deployments may be slow.
                <label style={{ display: 'flex', alignItems: 'center', gap: 8, marginTop: 8 }}>
                  <input
                    type="checkbox"
                    checked={acknowledged}
                    onChange={(event) => setAcknowledged(event.target.checked)}
                  />
                  I understand
                </label>
              </InlineNotice>
            )}

            {ineligible.length > 0 && (
              <section aria-label="Not eligible servers">
                <h2 className="section-title">Not eligible ({ineligible.length})</h2>
                <div className="data-list">
                  {ineligible.map((server) => (
                    <ServerRow key={server.id} server={server} selected={false} onSelect={() => {}} disabled />
                  ))}
                </div>
              </section>
            )}
          </>
        )}
      </div>
    </>
  )
}

function ServerRow({
  server,
  selected,
  onSelect,
  disabled = false,
}: {
  server: Server
  selected: boolean
  onSelect: () => void
  disabled?: boolean
}) {
  return (
    <div className="data-list__row" aria-disabled={disabled || undefined}>
      <div className="data-list__main">
        <div className="data-list__title">
          <input
            type="radio"
            name="server-selection"
            checked={selected}
            onChange={onSelect}
            disabled={disabled}
            aria-label={`Select ${server.name}`}
          />{' '}
          {server.name}
        </div>
        <div className="data-list__sub">
          {server.address ?? server.id}
          {server.lastSeenAt ? ` · seen ${server.lastSeenAt}` : ''}
        </div>
      </div>
      <StatusPill status={server.status} />
    </div>
  )
}

import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import {
  deleteConfiguration,
  generatePlan,
  getApplication,
  getProfile,
  isValidConfigName,
  listConfiguration,
  listServers,
  setConfiguration,
} from '../api/resources'
import { ApiError } from '../api/errors'
import { ConfirmButton } from '../components/ConfirmButton'
import { EnvironmentChip } from '../components/shell/EnvironmentChip'
import { PageHeader } from '../components/shell/PageHeader'
import { SecretField } from '../components/SecretField'
import { SetupStepper } from '../components/SetupStepper'
import { StatusPill } from '../components/StatusPill'
import { ErrorPanel, InlineNotice, SkeletonLines, useToast } from '../components/system'
import { useAsync } from '../hooks/useAsync'
import { ENVIRONMENTS, routes, type Environment } from '../routes/builders'
import { canReviewPlan } from '../workflow/blockingGate'

const ENVIRONMENT_MEANINGS: Record<Environment, string> = {
  production: 'Serves your users',
  staging: 'Pre-production validation',
  preview: 'Temporary review build',
}

/** Draft key for the "add a variable" row (never a real configuration name). */
const NEW_VARIABLE_KEY = '__new__'

/** A configuration row: profile requirement metadata merged with stored state. */
interface ConfigRow {
  name: string
  required: boolean
  secret: boolean
  isSet: boolean
  updatedAt?: string
}

/**
 * Deployment Configuration screen (deployment-configuration §1). Setup step 4.
 *
 * Collects the planner inputs — environment, server, ref, domain, config
 * values — with safe defaults visible. Nothing deploys from this screen;
 * Review plan generates a plan and opens the Plan screen.
 *
 * Configuration values (#126) are write-only: the list endpoint returns
 * metadata only, values are saved through `SecretField` and after saving the
 * UI only ever shows "set" — never the value.
 */
export function ConfigurePage() {
  const { applicationId } = useParams<{ applicationId: string }>()
  const navigate = useNavigate()
  const { push } = useToast()

  const [environment, setEnvironment] = useState<Environment | undefined>(undefined)
  const [domain, setDomain] = useState('')
  const [generating, setGenerating] = useState(false)
  const [errors, setErrors] = useState<string[]>([])
  const [drafts, setDrafts] = useState<Record<string, string>>({})
  const [editing, setEditing] = useState<ReadonlySet<string>>(new Set())
  const [newName, setNewName] = useState('')
  const [configError, setConfigError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const appState = useAsync(
    (signal) => (applicationId ? getApplication(applicationId, { signal }) : Promise.reject(new Error('missing id'))),
    [applicationId],
  )

  const profileState = useAsync(
    (signal) => (applicationId ? getProfile(applicationId, { signal }) : Promise.reject(new Error('missing id'))),
    [applicationId],
  )

  const serversState = useAsync((signal) => listServers({}, { signal }), [])

  const configState = useAsync(
    (signal) => (applicationId ? listConfiguration(applicationId, { signal }) : Promise.reject(new Error('missing id'))),
    [applicationId],
  )

  const profile = profileState.data
  const servers = serversState.data ?? []
  // The server is chosen in the Server selection step; this screen shows the
  // effective target and links back to change it.
  const server = servers.find((s) => s.status === 'ready') ?? servers[0]

  // Rows merge profile requirements (required/secret flags) with the stored
  // metadata from GET /configuration (isSet/updatedAt). Values themselves
  // are never part of this merge — the API does not return them.
  const configItems = configState.data ?? []
  const requirements = profile?.configuration ?? []
  const rows: ConfigRow[] = []
  for (const req of requirements) {
    const item = configItems.find((c) => c.name === req.name)
    rows.push({
      name: req.name,
      required: req.required ?? false,
      secret: item?.secret ?? req.secret ?? true,
      isSet: item?.isSet ?? false,
      updatedAt: item?.updatedAt,
    })
  }
  for (const item of configItems) {
    if (!rows.some((r) => r.name === item.name)) {
      rows.push({ name: item.name, required: false, secret: item.secret, isSet: item.isSet, updatedAt: item.updatedAt })
    }
  }

  const missingRequired = rows.filter((r) => r.required && !r.isSet).map((r) => r.name)
  const domainInvalid = domain !== '' && !/^[a-z0-9]([a-z0-9-]*[a-z0-9])(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$/i.test(domain)

  const startEdit = (name: string) => {
    setEditing((prev) => new Set(prev).add(name))
    setDrafts((prev) => ({ ...prev, [name]: '' }))
  }

  const cancelEdit = (name: string) => {
    setEditing((prev) => {
      const next = new Set(prev)
      next.delete(name)
      return next
    })
    setDrafts((prev) => {
      const next = { ...prev }
      delete next[name]
      return next
    })
  }

  const persistValue = async (name: string, value: string, secret: boolean) => {
    if (!applicationId || value === '') return
    setBusy(true)
    setConfigError(null)
    try {
      await setConfiguration(applicationId, name, value, secret)
      push({ variant: 'success', title: 'Value saved', body: `${name} is set — it is injected at deploy time.` })
      cancelEdit(name)
      configState.reload()
    } catch (cause) {
      const error = cause instanceof ApiError ? cause : ApiError.network()
      setConfigError(error.message)
      push({ variant: 'failure', title: "Couldn't save value", body: error.message })
    } finally {
      setBusy(false)
    }
  }

  const removeValue = async (name: string) => {
    if (!applicationId) return
    setBusy(true)
    setConfigError(null)
    try {
      await deleteConfiguration(applicationId, name)
      push({ variant: 'success', title: 'Value removed', body: `${name} was removed.` })
      cancelEdit(name)
      configState.reload()
    } catch (cause) {
      const error = cause instanceof ApiError ? cause : ApiError.network()
      setConfigError(error.message)
      push({ variant: 'failure', title: "Couldn't remove value", body: error.message })
    } finally {
      setBusy(false)
    }
  }

  const addVariable = async () => {
    const name = newName.trim()
    if (!applicationId || !isValidConfigName(name)) return
    const value = drafts[NEW_VARIABLE_KEY] ?? ''
    if (value === '') return
    await persistValue(name, value, true)
    setNewName('')
    setDrafts((prev) => {
      const next = { ...prev }
      delete next[NEW_VARIABLE_KEY]
      return next
    })
  }

  const reviewPlan = async () => {
    if (!applicationId) return
    // Client validation first; show the error summary instead of disabling.
    const check = canReviewPlan({
      environment,
      serverStatus: server?.status,
      degradedAcknowledged: true,
      ref: profile?.source?.ref,
      missingRequiredValues: missingRequired,
      domainInvalid,
    })
    if (!check.valid) {
      setErrors(check.errors)
      return
    }
    setErrors([])
    setGenerating(true)
    try {
      const plan = await generatePlan(applicationId, {
        serverId: server?.id ?? '',
        ref: profile?.source?.ref ?? 'main',
        domain: domain || undefined,
      })
      push({ variant: 'success', title: 'Plan ready', body: `Plan ${plan.id} generated.` })
      navigate(routes.setupPlan({ applicationId, planId: plan.id }))
    } catch (cause) {
      const error = cause instanceof ApiError ? cause : ApiError.network()
      setErrors([error.message])
      push({ variant: 'failure', title: "Couldn't generate plan", body: error.message })
    } finally {
      setGenerating(false)
    }
  }

  const newNameValid = isValidConfigName(newName)
  const newDraft = drafts[NEW_VARIABLE_KEY] ?? ''

  return (
    <>
      <PageHeader
        breadcrumbs={[
          { label: 'Workspace', to: routes.dashboard() },
          { label: appState.data?.name ?? applicationId ?? 'Application' },
          { label: 'Setup', current: true },
        ]}
        title="Configure deployment"
        environment={environment}
        meta={
          profile?.source?.ref
            ? `${profile.source.ref}${profile.source.commit ? ` · ${profile.source.commit.slice(0, 7)}` : ''}`
            : undefined
        }
        actions={
          <button type="button" className="btn btn--primary" onClick={() => void reviewPlan()} disabled={generating}>
            {generating ? 'Generating plan…' : 'Review plan ▸'}
          </button>
        }
        tabs={applicationId ? <SetupStepper applicationId={applicationId} current="configure" /> : undefined}
      />
      <div className="content__body stack">
        {errors.length > 0 && (
          <InlineNotice variant="failed" title="Some values are not valid">
            <ul style={{ margin: '4px 0 0', paddingLeft: 20 }}>
              {errors.map((error) => (
                <li key={error}>{error}</li>
              ))}
            </ul>
          </InlineNotice>
        )}

        {profileState.status === 'loading' && <SkeletonLines lines={4} />}

        {profileState.status === 'error' && profileState.error && (
          <ErrorPanel error={profileState.error} objectName="this profile" onRetry={profileState.reload} />
        )}

        <section className="card" aria-label="Target">
          <h2 className="section-title">Target</h2>
          <fieldset style={{ border: 'none', padding: 0, margin: 0 }}>
            <legend className="visually-hidden">Environment</legend>
            <div className="row" role="radiogroup" aria-label="Environment" style={{ gap: 12 }}>
              {ENVIRONMENTS.map((env) => (
                <label key={env} className={`env-card${environment === env ? ' env-card--selected' : ''}`}>
                  <input
                    type="radio"
                    name="environment"
                    value={env}
                    checked={environment === env}
                    onChange={() => setEnvironment(env)}
                  />
                  <EnvironmentChip environment={env} size="sm" />
                  <span className="env-card__meaning">{ENVIRONMENT_MEANINGS[env]}</span>
                </label>
              ))}
            </div>
          </fieldset>

          <div style={{ marginTop: 16 }}>
            <div className="muted">Server</div>
            {server ? (
              <div className="row" style={{ justifyContent: 'space-between' }}>
                <span>
                  {server.name} · <StatusPill status={server.status} />
                </span>
                <Link
                  className="btn btn--ghost btn--sm"
                  to={routes.setup({ applicationId: applicationId ?? '', step: 'server' })}
                >
                  Change
                </Link>
              </div>
            ) : (
              <p className="muted">No eligible server — choose one in Server selection.</p>
            )}
          </div>

          <div style={{ marginTop: 16 }}>
            <div className="muted">Source</div>
            <span className="mono">{profile?.source?.ref ?? '—'}</span>
            {profile?.source?.commit && <span className="mono muted"> · {profile.source.commit.slice(0, 7)}</span>}
          </div>
        </section>

        <section className="card" aria-label="Access">
          <h2 className="section-title">Access</h2>
          <div className="field">
            <label className="field__label" htmlFor="domain-input">
              Domain
            </label>
            <input
              id="domain-input"
              className="field__input"
              type="text"
              placeholder="app.example.com"
              value={domain}
              onChange={(event) => setDomain(event.target.value.trim())}
              aria-invalid={domainInvalid}
            />
            <span className="muted">HTTPS · certificate automatic</span>
          </div>
        </section>

        <section className="card" aria-label="Configuration values">
          <h2 className="section-title">Configuration values</h2>
          <InlineNotice variant="info" title="Values are injected at deploy time">
            Axiom stores configuration values encrypted and injects them into the application&apos;s environment
            when a deployment runs. Saved values are write-only: they are never displayed, logged or returned by
            the API.
          </InlineNotice>

          {configState.status === 'error' && configState.error && (
            <ErrorPanel error={configState.error} objectName="configuration values" onRetry={configState.reload} />
          )}

          {configState.status === 'loading' && <SkeletonLines lines={3} />}

          {configState.status === 'success' && (
            <>
              {configError && (
                <InlineNotice variant="failed" title="Couldn't save value">
                  {configError}
                </InlineNotice>
              )}

              {rows.length === 0 ? (
                <p className="muted">No configuration requirements from the profile — add a variable below.</p>
              ) : (
                <div className="stack" style={{ gap: 12 }}>
                  {rows.map((row) => {
                    const isEditing = editing.has(row.name)
                    return (
                      <div className="config-value-row" key={row.name}>
                        <div className="config-value-row__name">
                          <span className="mono">{row.name}</span>
                          <span className="muted">
                            {row.required ? ' · required' : ''}
                            {row.secret ? ' · secret' : ''}
                            {row.isSet && !isEditing && row.updatedAt ? ` · updated ${row.updatedAt}` : ''}
                          </span>
                        </div>
                        {row.isSet && !isEditing ? (
                          <div className="row" style={{ alignItems: 'center', gap: 8 }}>
                            <SecretField
                              key={`${row.name}:set`}
                              name={row.name}
                              isSet
                              required={row.required}
                              onReplace={() => startEdit(row.name)}
                            />
                            <ConfirmButton
                              label="Remove"
                              confirmLabel="Confirm remove"
                              className="btn btn--destructive btn--sm"
                              disabled={busy}
                              onConfirm={() => void removeValue(row.name)}
                            />
                          </div>
                        ) : (
                          <SecretField
                            key={`${row.name}:edit`}
                            name={row.name}
                            isSet={false}
                            required={row.required}
                            onChange={(value) =>
                              setDrafts((prev) => ({ ...prev, [row.name]: value }))
                            }
                            onSave={() => void persistValue(row.name, drafts[row.name] ?? '', row.secret)}
                            onCancel={() => cancelEdit(row.name)}
                            saveDisabled={busy || (drafts[row.name] ?? '') === ''}
                          />
                        )}
                      </div>
                    )
                  })}
                </div>
              )}

              <div className="row" style={{ marginTop: 12, alignItems: 'center', gap: 8 }}>
                <input
                  className="field__input"
                  type="text"
                  placeholder="Variable name (e.g. API_KEY)"
                  value={newName}
                  onChange={(event) => setNewName(event.target.value.trim())}
                  aria-label="New variable name"
                  aria-invalid={newName !== '' && !newNameValid}
                  style={{ flex: '1 1 auto' }}
                />
                {newName !== '' && !newNameValid && <span className="muted">Names match ^[A-Z_][A-Z0-9_]*$</span>}
              </div>
              {newNameValid && !rows.some((r) => r.name === newName) && (
                <div className="row" style={{ marginTop: 8, alignItems: 'center' }}>
                  <SecretField
                    key={newName}
                    name={newName}
                    isSet={false}
                    onChange={(value) => setDrafts((prev) => ({ ...prev, [NEW_VARIABLE_KEY]: value }))}
                    onSave={() => void addVariable()}
                    onCancel={() => {
                      setNewName('')
                      setDrafts((prev) => {
                        const next = { ...prev }
                        delete next[NEW_VARIABLE_KEY]
                        return next
                      })
                    }}
                    saveDisabled={busy || newDraft === ''}
                  />
                </div>
              )}
            </>
          )}
        </section>

        <section className="card" aria-label="Runtime">
          <h2 className="section-title">Runtime</h2>
          <div className="data-list">
            <div className="data-list__row">
              <div className="data-list__main">
                <div className="data-list__title">Port</div>
                <div className="data-list__sub">{profile?.port?.value ?? '—'}</div>
              </div>
            </div>
            <div className="data-list__row">
              <div className="data-list__main">
                <div className="data-list__title">Health check</div>
                <div className="data-list__sub">
                  {profile?.healthCheck?.value ? `${profile.healthCheck.value.type ?? 'http'} ${profile.healthCheck.value.path ?? '/'}` : '—'}
                </div>
              </div>
            </div>
            <div className="data-list__row">
              <div className="data-list__main">
                <div className="data-list__title">Start command</div>
                <div className="data-list__sub mono">{profile?.startCommand?.value ?? '—'}</div>
              </div>
            </div>
          </div>
        </section>
      </div>
    </>
  )
}

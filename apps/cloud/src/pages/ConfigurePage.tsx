import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { generatePlan, getApplication, getProfile, listServers } from '../api/resources'
import type { ConfigRequirement } from '../api/types'
import { ApiError } from '../api/errors'
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

interface ConfigValue {
  name: string
  required?: boolean
  secret?: boolean
  isSet: boolean
  draft: string
}

/**
 * Deployment Configuration screen (deployment-configuration §1). Setup step 4.
 *
 * Collects the planner inputs — environment, server, ref, domain, config
 * values — with safe defaults visible. Nothing deploys from this screen;
 * Review plan generates a plan and opens the Plan screen.
 */
export function ConfigurePage() {
  const { applicationId } = useParams<{ applicationId: string }>()
  const navigate = useNavigate()
  const { push } = useToast()

  const [environment, setEnvironment] = useState<Environment | undefined>(undefined)
  const [domain, setDomain] = useState('')
  const [values, setValues] = useState<ConfigValue[]>([])
  const [extraName, setExtraName] = useState('')
  const [generating, setGenerating] = useState(false)
  const [errors, setErrors] = useState<string[]>([])

  const appState = useAsync(
    (signal) => (applicationId ? getApplication(applicationId, { signal }) : Promise.reject(new Error('missing id'))),
    [applicationId],
  )

  const profileState = useAsync(
    (signal) => (applicationId ? getProfile(applicationId, { signal }) : Promise.reject(new Error('missing id'))),
    [applicationId],
  )

  const serversState = useAsync((signal) => listServers({}, { signal }), [])

  const profile = profileState.data
  const servers = serversState.data ?? []
  // The server is chosen in the Server selection step; this screen shows the
  // effective target and links back to change it.
  const server = servers.find((s) => s.status === 'ready') ?? servers[0]

  // Initialize config value rows from the profile's requirements.
  const requirements = profile?.configuration ?? []
  const configValues: ConfigValue[] = [
    ...requirements.map((req: ConfigRequirement) => ({
      name: req.name,
      required: req.required,
      secret: req.secret,
      isSet: false,
      draft: '',
    })),
    ...values.filter((v) => !requirements.some((r) => r.name === v.name)),
  ]

  const missingRequired = configValues.filter((v) => v.required && !v.isSet).map((v) => v.name)
  const domainInvalid = domain !== '' && !/^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$/i.test(domain)

  const setValue = (name: string, draft: string) => {
    setValues((prev) => {
      const existing = prev.find((v) => v.name === name)
      if (existing) {
        return prev.map((v) => (v.name === name ? { ...v, draft, isSet: draft !== '' } : v))
      }
      return [...prev, { name, draft, isSet: draft !== '' }]
    })
  }

  const removeValue = (name: string) => {
    setValues((prev) => prev.filter((v) => v.name !== name))
  }

  const addExtra = () => {
    const name = extraName.trim()
    if (!name) return
    if (configValues.some((v) => v.name === name)) return
    setValues((prev) => [...prev, { name, draft: '', isSet: false }])
    setExtraName('')
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
          {configValues.length === 0 ? (
            <p className="muted">No configuration requirements from the profile.</p>
          ) : (
            <div className="stack" style={{ gap: 12 }}>
              {configValues.map((value) => (
                <div className="config-value-row" key={value.name}>
                  <div className="config-value-row__name">
                    <span className="mono">{value.name}</span>
                    <span className="muted">
                      {value.required ? ' · required' : ''}
                      {value.secret ? ' · Secret' : ''}
                    </span>
                  </div>
                  {value.secret ? (
                    <SecretField
                      name={value.name}
                      isSet={value.isSet}
                      required={value.required}
                      onChange={(draft) => setValue(value.name, draft)}
                      onReplace={() => setValue(value.name, '')}
                      onRemove={() => removeValue(value.name)}
                    />
                  ) : (
                    <div className="secret-field">
                      <input
                        className="field__input"
                        type="text"
                        value={value.draft}
                        placeholder={value.required ? 'Required' : 'Optional'}
                        autoComplete="off"
                        aria-label={value.name}
                        onChange={(event) => setValue(value.name, event.target.value)}
                      />
                    </div>
                  )}
                </div>
              ))}
            </div>
          )}

          <div className="row" style={{ marginTop: 12 }}>
            <input
              className="field__input"
              type="text"
              placeholder="Variable name"
              value={extraName}
              onChange={(event) => setExtraName(event.target.value)}
              aria-label="New variable name"
              style={{ flex: '1 1 auto' }}
            />
            <button type="button" className="btn btn--secondary" onClick={addExtra}>
              + Add variable
            </button>
          </div>
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

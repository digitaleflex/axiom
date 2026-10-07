import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { createDeployment, getApplication, getPlan, getProfile } from '../api/resources'
import type { Plan } from '../api/types'
import { ApiError } from '../api/errors'
import { DeployConfirmDialog } from '../components/DeployConfirmDialog'
import { PageHeader } from '../components/shell/PageHeader'
import { PlanSequence } from '../components/PlanSequence'
import { SetupStepper } from '../components/SetupStepper'
import { ErrorPanel, InlineNotice, SkeletonLines, useToast } from '../components/system'
import { useAsync } from '../hooks/useAsync'
import { routes } from '../routes/builders'
import { canDeployPlan } from '../workflow/blockingGate'
import { idempotencyKeyForPlan } from '../workflow/idempotency'

/**
 * Deployment Plan screen (deployment-plan §1). Setup step 5.
 *
 * The plan is a first-class review surface: summary, execution sequence with
 * per-step detail, rollback boundary, and the Deploy action. The plan is
 * immutable — the URL always identifies one plan; refresh shows the same
 * plan. Deploy uses a deterministic Idempotency-Key bound to the plan.
 */
export function PlanPage() {
  const { applicationId, planId } = useParams<{ applicationId: string; planId: string }>()
  const navigate = useNavigate()
  const { push } = useToast()
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [deploying, setDeploying] = useState(false)
  const [deployError, setDeployError] = useState<ApiError | null>(null)

  const appState = useAsync(
    (signal) => (applicationId ? getApplication(applicationId, { signal }) : Promise.reject(new Error('missing id'))),
    [applicationId],
  )

  const profileState = useAsync(
    (signal) => (applicationId ? getProfile(applicationId, { signal }) : Promise.reject(new Error('missing id'))),
    [applicationId],
  )

  const planState = useAsync<Plan | null>(
    (signal) => (planId ? getPlan(planId, { signal }) : Promise.reject(new Error('missing id'))),
    [planId],
  )

  const plan = planState.data
  const profile = profileState.data
  const gate = canDeployPlan({ planStatus: plan?.status })

  const deploy = async () => {
    if (!applicationId || !planId) return
    setDeploying(true)
    setDeployError(null)
    try {
      const deployment = await createDeployment(applicationId, planId, idempotencyKeyForPlan(planId))
      push({ variant: 'success', title: 'Deployment started', body: `Deployment #${deployment.number ?? deployment.id} is running.` })
      navigate(
        routes.deployment({
          applicationId,
          environment: (deployment.environment as 'production') ?? 'production',
          deploymentId: deployment.id,
          tab: 'progress',
        }),
      )
    } catch (cause) {
      const error = cause instanceof ApiError ? cause : ApiError.network()
      setDeployError(error)
      push({ variant: 'failure', title: "Couldn't start deployment", body: error.message })
    } finally {
      setDeploying(false)
      setConfirmOpen(false)
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
        title="Deployment plan"
        environment={plan?.environment as 'production' | undefined}
        meta={
          plan
            ? `${plan.id} · profile v${plan.applicationProfileVersion ?? '—'}`
            : undefined
        }
        actions={
          plan && (
            <div className="row" style={{ gap: 8 }}>
              <Link
                className="btn btn--ghost"
                to={routes.setup({ applicationId: applicationId ?? '', step: 'configure' })}
              >
                Edit configuration
              </Link>
              <button
                type="button"
                className="btn btn--primary"
                onClick={() => setConfirmOpen(true)}
                disabled={!gate.canDeploy || deploying}
                aria-disabled={!gate.canDeploy}
              >
                {deploying ? 'Deploying…' : 'Deploy ▸'}
              </button>
            </div>
          )
        }
        tabs={applicationId ? <SetupStepper applicationId={applicationId} current="plan" planId={planId} /> : undefined}
      />
      <div className="content__body stack">
        {planState.status === 'loading' && <SkeletonLines lines={6} />}

        {planState.status === 'error' && planState.error && (
          <ErrorPanel error={planState.error} objectName="this plan" onRetry={planState.reload} />
        )}

        {plan && plan.status !== 'READY' && (
          <InlineNotice variant="failed" title="This plan is not deployable">
            Plan status: {plan.status}. Generate a new plan from the configuration.
          </InlineNotice>
        )}

        {plan && !gate.canDeploy && gate.reason && (
          <InlineNotice variant="warning" title="Deploy is disabled">
            {gate.reason}
          </InlineNotice>
        )}

        {deployError && (
          <InlineNotice variant="failed" title="Couldn't start deployment">
            {deployError.message}
          </InlineNotice>
        )}

        {plan && (
          <>
            <section className="card" aria-label="Plan summary">
              <h2 className="section-title">Plan summary</h2>
              <div className="data-list">
                <SummaryRow label="Source" value={profile?.source ? `${profile.source.ref}${profile.source.commit ? ` @ ${profile.source.commit.slice(0, 7)}` : ''}` : '—'} />
                <SummaryRow label="Application" value={appState.data?.name ?? '—'} />
                <SummaryRow label="Target" value={plan.serverId ?? '—'} />
                <SummaryRow label="Health" value={plan.healthCheck ? `${plan.healthCheck.type} ${plan.healthCheck.path ?? '/'}` : '—'} />
                <SummaryRow
                  label="Rollback"
                  value={plan.rollback?.strategy ? `Strategy: ${plan.rollback.strategy}` : 'Not specified by this plan'}
                />
              </div>
            </section>

            <section className="card" aria-label="Execution sequence">
              <h2 className="section-title">Execution sequence</h2>
              <PlanSequence
                steps={plan.steps ?? []}
                precondition={{
                  label: 'Analyze',
                  detail: profile?.source?.commit ? profile.source.commit.slice(0, 7) : undefined,
                }}
              />
            </section>
          </>
        )}

        <DeployConfirmDialog
          open={confirmOpen}
          environment={plan?.environment ?? 'production'}
          applicationName={appState.data?.name ?? applicationId ?? ''}
          summary={[
            `Ref: ${profile?.source?.ref ?? '—'}`,
            `Server: ${plan?.serverId ?? '—'}`,
            `Domain: ${plan?.healthCheck ? 'routed with HTTPS' : '—'}`,
          ]}
          rollbackLine={plan?.rollback?.strategy ? `Rollback: ${plan.rollback.strategy}` : 'Rollback: not specified by this plan'}
          onConfirm={() => void deploy()}
          onCancel={() => setConfirmOpen(false)}
        />
      </div>
    </>
  )
}

function SummaryRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="data-list__row">
      <div className="data-list__main">
        <div className="data-list__title">{label}</div>
      </div>
      <span className="mono">{value}</span>
    </div>
  )
}

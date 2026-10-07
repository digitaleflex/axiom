import { useParams } from 'react-router-dom'
import { Placeholder } from '../components/Placeholder'
import { PageHeader } from '../components/shell/PageHeader'
import { routes, SETUP_STEPS } from '../routes/builders'

const STEP_TITLES: Record<string, string> = {
  analysis: 'Analyze',
  profile: 'Profile',
  server: 'Server',
  configure: 'Configure',
  plan: 'Plan',
}

/** Setup flow placeholder (navigation §3.2). Rendered as a stepper in #120. */
export function SetupPage() {
  const { applicationId, step } = useParams<{ applicationId: string; step: string }>()

  const stepper = (
    <nav aria-label="Setup steps" className="breadcrumbs" style={{ marginTop: 12, marginBottom: 0 }}>
      {SETUP_STEPS.map((value, index) => (
        <span key={value} className="row" style={{ gap: 8 }}>
          {index > 0 && (
            <span className="breadcrumbs__sep" aria-hidden="true">
              ›
            </span>
          )}
          <span aria-current={step === value ? 'step' : undefined}>
            {index + 1}. {STEP_TITLES[value]}
          </span>
        </span>
      ))}
    </nav>
  )

  return (
    <>
      <PageHeader
        breadcrumbs={[
          { label: 'Workspace', to: routes.dashboard() },
          { label: applicationId ?? 'Application' },
          { label: 'Setup', current: true },
        ]}
        title={STEP_TITLES[step ?? 'analysis'] ?? 'Setup'}
        tabs={stepper}
      />
      <div className="content__body">
        <Placeholder
          params={{ applicationId, step }}
          resources={[
            'POST /api/v1/applications/{applicationId}/analysis',
            'GET /api/v1/applications/{applicationId}/profile',
            'POST /api/v1/applications/{applicationId}/deployment-plans',
          ]}
          note="The five-step setup flow is wired in #120; the stepper chrome renders here."
        />
      </div>
    </>
  )
}

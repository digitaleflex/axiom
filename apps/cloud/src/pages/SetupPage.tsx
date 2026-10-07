import { useParams } from 'react-router-dom'
import { AnalysisPage } from './AnalysisPage'
import { ConfigurePage } from './ConfigurePage'
import { ProfilePage } from './ProfilePage'
import { ServerSelectPage } from './ServerSelectPage'
import { NotFoundPage } from './NotFoundPage'

/**
 * Setup flow router (navigation §3.2). Each step renders its real screen;
 * the stepper chrome is part of each screen's PageHeader.
 */
export function SetupPage() {
  const { step } = useParams<{ applicationId: string; step: string }>()

  switch (step) {
    case 'analysis':
      return <AnalysisPage />
    case 'profile':
      return <ProfilePage />
    case 'server':
      return <ServerSelectPage />
    case 'configure':
      return <ConfigurePage />
    default:
      // 'plan' without a planId has its own route (…/setup/plan/:planId).
      return <NotFoundPage />
  }
}

import { Navigate, Route, Routes, useParams } from 'react-router-dom'
import { routes } from '../routes/builders'
import { RequireAuth } from '../auth/RequireAuth'
import { AuthProvider } from '../auth/AuthContext'
import { AppShell } from '../components/shell/AppShell'
import { ToastProvider } from '../components/system'
import { ApplicationOverviewPage } from '../pages/ApplicationOverviewPage'
import { ApplicationSectionPage } from '../pages/ApplicationSectionPage'
import { DashboardPage } from '../pages/DashboardPage'
import { DeploymentProgressPage } from '../pages/DeploymentProgressPage'
import { DeploymentTabPage } from '../pages/DeploymentTabPage'
import { PlanPage } from '../pages/PlanPage'
import { DomainsPage } from '../pages/DomainsPage'
import { GithubPage } from '../pages/GithubPage'
import { LoginPage } from '../pages/LoginPage'
import { NotFoundPage } from '../pages/NotFoundPage'
import { RepositoriesPage } from '../pages/RepositoriesPage'
import { RepositoryDetailPage } from '../pages/RepositoryDetailPage'
import { ServerDetailPage } from '../pages/ServerDetailPage'
import { ServersPage } from '../pages/ServersPage'
import { SettingsPage } from '../pages/SettingsPage'
import { SetupPage } from '../pages/SetupPage'

function AppsRedirect() {
  // Environment is a contract gap (#71/#117); until it lands, land on the
  // application overview placeholder.
  const { applicationId } = useParams<{ applicationId: string }>()
  return <Navigate to={applicationId ? routes.application(applicationId) : routes.dashboard()} replace />
}

export function AppRoutes() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />

      <Route
        element={
          <RequireAuth>
            <AppShell />
          </RequireAuth>
        }
      >
        <Route path="/" element={<Navigate to="/dashboard" replace />} />
        <Route path="/dashboard" element={<DashboardPage />} />
        <Route path="/github" element={<GithubPage />} />
        <Route path="/repositories" element={<RepositoriesPage />} />
        <Route path="/repositories/:repositoryId" element={<RepositoryDetailPage />} />

        <Route path="/applications" element={<Navigate to="/dashboard" replace />} />
        <Route path="/applications/:applicationId" element={<ApplicationOverviewPage />} />

        {/* Canonical navigation routes (navigation §5.1) */}
        <Route path="/apps/:applicationId" element={<AppsRedirect />} />
        <Route path="/apps/:applicationId/setup/plan/:planId" element={<PlanPage />} />
        <Route path="/apps/:applicationId/setup/:step" element={<SetupPage />} />
        <Route path="/apps/:applicationId/:environment/:section" element={<ApplicationSectionPage />} />
        <Route
          path="/apps/:applicationId/:environment/deployments/:deploymentId"
          element={<Navigate to="progress" replace />}
        />
        <Route
          path="/apps/:applicationId/:environment/deployments/:deploymentId/:tab"
          element={<DeploymentTabPage />}
        />

        {/* Deployment permalink (navigation §5.2) */}
        <Route path="/deployments/:deploymentId" element={<DeploymentProgressPage />} />

        <Route path="/servers" element={<ServersPage />} />
        <Route path="/servers/:serverId" element={<ServerDetailPage />} />
        <Route path="/settings" element={<SettingsPage />} />
        <Route path="/settings/:section" element={<SettingsPage />} />
        <Route path="/domains" element={<DomainsPage />} />

        <Route path="*" element={<NotFoundPage />} />
      </Route>
    </Routes>
  )
}

export function App() {
  return (
    <ToastProvider>
      <AuthProvider>
        <AppRoutes />
      </AuthProvider>
    </ToastProvider>
  )
}

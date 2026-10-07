import type { QueryValue } from '../api/client'
import { buildQuery } from '../api/client'

/**
 * Centralized route builders (docs/design/handoff/shell §5.1).
 *
 * No component concatenates paths. When multi-workspace lands (navigation §9)
 * only this module gains the `/w/:workspaceSlug` prefix.
 */

export type Environment = 'production' | 'staging' | 'preview'

export const ENVIRONMENTS: readonly Environment[] = ['production', 'staging', 'preview']

export const ENVIRONMENT_LABELS: Record<Environment, string> = {
  production: 'Production',
  staging: 'Staging',
  preview: 'Preview',
}

export function isEnvironment(value: string | undefined | null): value is Environment {
  return value === 'production' || value === 'staging' || value === 'preview'
}

export type DeploymentTab = 'summary' | 'plan' | 'progress' | 'logs' | 'health' | 'runtime'

export const DEPLOYMENT_TABS: readonly DeploymentTab[] = [
  'summary',
  'plan',
  'progress',
  'logs',
  'health',
  'runtime',
]

export type ApplicationSection = 'overview' | 'deployments' | 'logs' | 'metrics' | 'domains'

export const APPLICATION_SECTIONS: readonly ApplicationSection[] = [
  'overview',
  'deployments',
  'logs',
  'metrics',
  'domains',
]

export type SetupStep = 'analysis' | 'profile' | 'server' | 'configure' | 'plan'

export const SETUP_STEPS: readonly SetupStep[] = ['analysis', 'profile', 'server', 'configure', 'plan']

function queryString(query?: Record<string, QueryValue>): string {
  return buildQuery(query)
}

export const routes = {
  root: () => '/',
  login: (returnTo?: string) => `/login${queryString({ returnTo })}`,
  dashboard: () => '/dashboard',
  github: (query?: Record<string, QueryValue>) => `/github${queryString(query)}`,
  repositories: (query?: Record<string, QueryValue>) => `/repositories${queryString(query)}`,
  repository: (repositoryId: string) => `/repositories/${encodeURIComponent(repositoryId)}`,
  application: (applicationId: string) => `/applications/${encodeURIComponent(applicationId)}`,
  applications: () => '/applications',
  appSection: (params: {
    applicationId: string
    environment: Environment
    section: ApplicationSection
    query?: Record<string, QueryValue>
  }) =>
    `/apps/${encodeURIComponent(params.applicationId)}/${params.environment}/${params.section}${queryString(
      params.query,
    )}`,
  setup: (params: { applicationId: string; step: SetupStep }) =>
    `/apps/${encodeURIComponent(params.applicationId)}/setup/${params.step}`,
  setupPlan: (params: { applicationId: string; planId: string }) =>
    `/apps/${encodeURIComponent(params.applicationId)}/setup/plan/${encodeURIComponent(params.planId)}`,
  deploymentPermalink: (deploymentId: string) => `/deployments/${encodeURIComponent(deploymentId)}`,
  deployment: (params: {
    applicationId: string
    environment: Environment
    deploymentId: string
    tab?: DeploymentTab
    query?: Record<string, QueryValue>
  }) =>
    `/apps/${encodeURIComponent(params.applicationId)}/${params.environment}/deployments/${encodeURIComponent(
      params.deploymentId,
    )}${params.tab ? `/${params.tab}` : ''}${queryString(params.query)}`,
  servers: (query?: Record<string, QueryValue>) => `/servers${queryString(query)}`,
  server: (serverId: string) => `/servers/${encodeURIComponent(serverId)}`,
  domains: () => '/domains',
  settings: (section?: string) => (section ? `/settings/${encodeURIComponent(section)}` : '/settings'),
}

/** Environment segment for breadcrumbs (not a link target by itself). */
export function environmentPath(applicationId: string, environment: Environment): string {
  return routes.appSection({ applicationId, environment, section: 'overview' })
}

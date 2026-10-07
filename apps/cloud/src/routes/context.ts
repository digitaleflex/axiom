import { isEnvironment, type DeploymentTab, type Environment, type SetupStep } from './builders'

/**
 * Derives shell variant + active navigation from the URL. The URL is the only
 * source of truth for context (navigation §12.1).
 */

export type ShellVariant = 'workspace' | 'setup' | 'application' | 'deployment' | 'infrastructure'

export type SidebarKey =
  | 'dashboard'
  | 'repositories'
  | 'github'
  | 'overview'
  | 'deployments'
  | 'logs'
  | 'metrics'
  | 'app-domains'
  | 'servers'
  | 'domains'
  | 'settings'

export interface RouteContext {
  variant: ShellVariant
  sidebarActive: SidebarKey | null
  applicationId?: string
  environment?: Environment
  deploymentId?: string
  repositoryId?: string
  serverId?: string
  section?: string
  setupStep?: SetupStep
  /** Plan ID on `/apps/:id/setup/plan/:planId`. */
  planId?: string
  deploymentTab?: DeploymentTab
  /** Path does not match any known route. */
  unknown: boolean
}

const KNOWN_DEPLOYMENT_TABS: readonly string[] = ['summary', 'plan', 'progress', 'logs', 'health', 'runtime']

function segment(pathname: string, index: number): string | undefined {
  const parts = pathname.split('/').filter(Boolean)
  return parts[index]
}

export function parseRoute(pathname: string): RouteContext {
  const clean = pathname.split('?')[0].split('#')[0]
  const parts = clean.split('/').filter(Boolean)

  if (parts.length === 0) {
    return { variant: 'workspace', sidebarActive: 'dashboard', unknown: false }
  }

  const [first, second, third, fourth, fifth, sixth] = parts

  if (first === 'dashboard') {
    return { variant: 'workspace', sidebarActive: 'dashboard', unknown: false }
  }

  if (first === 'github') {
    return { variant: 'workspace', sidebarActive: 'github', unknown: false }
  }

  if (first === 'repositories') {
    return {
      variant: 'workspace',
      sidebarActive: 'repositories',
      repositoryId: second,
      unknown: false,
    }
  }

  if (first === 'servers') {
    return {
      variant: 'infrastructure',
      sidebarActive: 'servers',
      serverId: second,
      unknown: false,
    }
  }

  if (first === 'domains') {
    return { variant: 'infrastructure', sidebarActive: 'domains', unknown: false }
  }

  if (first === 'settings') {
    return { variant: 'infrastructure', sidebarActive: 'settings', section: second, unknown: false }
  }

  // Task-lane route: /applications/:id (Application Overview placeholder).
  if (first === 'applications') {
    if (!second) return { variant: 'application', sidebarActive: 'overview', unknown: true }
    return {
      variant: 'application',
      sidebarActive: 'overview',
      applicationId: second,
      unknown: false,
    }
  }

  // Task-lane route: /deployments/:id (Deployment Progress placeholder).
  if (first === 'deployments') {
    if (!second) return { variant: 'deployment', sidebarActive: 'deployments', unknown: true }
    return {
      variant: 'deployment',
      sidebarActive: 'deployments',
      deploymentId: second,
      deploymentTab: 'progress',
      unknown: false,
    }
  }

  // Canonical navigation routes: /apps/:applicationId/...
  if (first === 'apps') {
    if (!second) return { variant: 'application', sidebarActive: 'overview', unknown: true }

    // /apps/:id/setup/:step  and  /apps/:id/setup/plan/:planId
    if (third === 'setup') {
      if (fourth === 'plan') {
        return {
          variant: 'setup',
          sidebarActive: null,
          applicationId: second,
          setupStep: 'plan',
          planId: fifth,
          unknown: false,
        }
      }
      const step = (fourth ?? 'analysis') as SetupStep
      return {
        variant: 'setup',
        sidebarActive: null,
        applicationId: second,
        setupStep: step,
        unknown: false,
      }
    }

    const environment = third
    if (!isEnvironment(environment)) {
      return {
        variant: 'application',
        sidebarActive: 'overview',
        applicationId: second,
        unknown: true,
      }
    }

    // /apps/:id/:env/deployments/:depId/:tab?
    if (fourth === 'deployments') {
      const deploymentId = fifth
      const tab = sixth
      if (deploymentId && tab && !KNOWN_DEPLOYMENT_TABS.includes(tab)) {
        return {
          variant: 'deployment',
          sidebarActive: 'deployments',
          applicationId: second,
          environment,
          deploymentId,
          unknown: true,
        }
      }
      return {
        variant: 'deployment',
        sidebarActive: 'deployments',
        applicationId: second,
        environment,
        deploymentId,
        deploymentTab: (tab as DeploymentTab | undefined) ?? 'progress',
        unknown: false,
      }
    }

    const section = fourth
    const sidebarActive: SidebarKey =
      section === 'deployments'
        ? 'deployments'
        : section === 'logs'
          ? 'logs'
          : section === 'metrics'
            ? 'metrics'
            : section === 'domains'
              ? 'app-domains'
              : 'overview'
    return {
      variant: 'application',
      sidebarActive,
      applicationId: second,
      environment,
      unknown: false,
    }
  }

  // /login is handled outside the shell; treat as unknown for shell purposes.
  return { variant: 'workspace', sidebarActive: null, unknown: true }
}

/** The canonical route for the task-lane application overview placeholder. */
export function applicationIdFromPath(pathname: string): string | undefined {
  return parseRoute(pathname).applicationId
}

/** Reads a single search param without a router dependency (test-friendly). */
export function getSearchParam(search: string, key: string): string | null {
  return new URLSearchParams(search).get(key)
}

export function segmentAt(pathname: string, index: number): string | undefined {
  return segment(pathname, index)
}

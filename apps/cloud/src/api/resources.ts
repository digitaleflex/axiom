import { api, apiFetch, type RequestOptions } from './client'
import { normalizePage, type PageQuery } from './pagination'
import type {
  Analysis,
  Application,
  AuthResult,
  Deployment,
  DeploymentEvent,
  DeploymentStep,
  Domain,
  GithubConnection,
  HealthResult,
  LogEntry,
  Me,
  Paginated,
  Plan,
  Profile,
  Ref,
  Repository,
  Server,
} from './types'

type ReadOptions = Pick<RequestOptions, 'signal'>

/** Tolerates `{ items }` or a bare array. */
export function normalizeItems<T>(value: unknown): T[] {
  if (Array.isArray(value)) return value as T[]
  const items = (value as { items?: unknown } | null)?.items
  return Array.isArray(items) ? (items as T[]) : []
}

/* ------------------------------------------------------------------ Auth §2 */

export function getMe(options: ReadOptions = {}): Promise<Me> {
  return api.get<Me>('/auth/me', options)
}

/** Best-effort logout; the caller clears local state regardless. */
export function logout(): Promise<void> {
  return apiFetch<void>('/auth/logout', { method: 'POST' })
}

/** Sign in with email and password; the Engine sets the session cookie. */
export function login(email: string, password: string): Promise<AuthResult> {
  return apiFetch<AuthResult>('/auth/login', { method: 'POST', body: { email, password }, skipCsrf: true })
}

/** Create an account and start a session. */
export function register(email: string, password: string, name?: string): Promise<AuthResult> {
  return apiFetch<AuthResult>('/auth/register', { method: 'POST', body: { email, password, name }, skipCsrf: true })
}

/** Active sessions for the current user (never returns tokens). */
export function listSessions(options: ReadOptions = {}) {
  return apiFetch<{ items: SessionInfo[] }>('/auth/sessions', options)
}

export function revokeSession(id: string): Promise<void> {
  return apiFetch<void>(`/auth/sessions/${id}`, { method: 'DELETE' })
}

export function revokeOtherSessions(): Promise<void> {
  return apiFetch<void>('/auth/sessions', { method: 'DELETE' })
}

/** A user session (api-contract §2); no token material is ever returned. */
export interface SessionInfo {
  id: string
  userAgent?: string
  ip?: string
  createdAt: string
  lastSeenAt: string
  expiresAt: string
  current?: boolean
}

/* ------------------------------------------------------------ GitHub §4 */

export function listGithubConnections(options: ReadOptions = {}): Promise<GithubConnection[]> {
  return api.get<unknown>('/github/connections', options).then(normalizeItems<GithubConnection>)
}

export interface AuthorizeUrl {
  authorizeUrl: string
}

export function startGithubConnect(): Promise<AuthorizeUrl> {
  return api.post<AuthorizeUrl>('/github/connections')
}

export function disconnectGithubConnection(connectionId: string): Promise<void> {
  return apiFetch<void>(`/github/connections/${encodeURIComponent(connectionId)}`, { method: 'DELETE' })
}

/* -------------------------------------------------------- Repositories §5 */

export interface RepositoryQuery extends PageQuery {
  search?: string
}

export async function listRepositories(
  connectionId: string,
  query: RepositoryQuery = {},
  options: ReadOptions = {},
): Promise<Paginated<Repository>> {
  const data = await api.get<unknown>(`/github/connections/${encodeURIComponent(connectionId)}/repositories`, {
    ...options,
    query: {
      page: query.page,
      limit: query.limit,
      search: query.search,
    },
  })
  return normalizePage<Repository>(data)
}

export function getRepository(repositoryId: string, options: ReadOptions = {}): Promise<Repository> {
  return api.get<Repository>(`/repositories/${encodeURIComponent(repositoryId)}`, options)
}

export function listRefs(repositoryId: string, options: ReadOptions = {}): Promise<Ref[]> {
  return api
    .get<unknown>(`/repositories/${encodeURIComponent(repositoryId)}/refs`, options)
    .then(normalizeItems<Ref>)
}

/* ----------------------------------------------------------- Applications §6 */

export async function listApplications(options: ReadOptions = {}): Promise<Paginated<Application>> {
  const data = await api.get<unknown>('/applications', options)
  return normalizePage<Application>(data)
}

export function getApplication(applicationId: string, options: ReadOptions = {}): Promise<Application> {
  return api.get<Application>(`/applications/${encodeURIComponent(applicationId)}`, options)
}

export function createApplication(
  body: { repositoryId: string; name: string },
  options: ReadOptions = {},
): Promise<Application> {
  return api.post<Application>('/applications', body, options)
}

/* ----------------------------------------------------------- Analysis §7 */

export function startAnalysis(
  applicationId: string,
  body: { ref: string; root?: string },
  options: ReadOptions = {},
): Promise<Analysis> {
  return api.post<Analysis>(`/applications/${encodeURIComponent(applicationId)}/analysis`, body, options)
}

export function getAnalysis(
  applicationId: string,
  analysisId: string,
  options: ReadOptions = {},
): Promise<Analysis> {
  return api.get<Analysis>(
    `/applications/${encodeURIComponent(applicationId)}/analysis/${encodeURIComponent(analysisId)}`,
    options,
  )
}

/* ------------------------------------------------------------ Profile §8 */

export function getProfile(applicationId: string, options: ReadOptions = {}): Promise<Profile> {
  return api.get<Profile>(`/applications/${encodeURIComponent(applicationId)}/profile`, options)
}

export function putProfileOverrides(
  applicationId: string,
  overrides: Record<string, unknown>,
  options: ReadOptions = {},
): Promise<Profile> {
  return api.put<Profile>(`/applications/${encodeURIComponent(applicationId)}/profile/overrides`, overrides, options)
}

/* -------------------------------------------------------------- Plans §10 */

export function generatePlan(
  applicationId: string,
  body: { serverId: string; ref: string; domain?: string },
  options: ReadOptions = {},
): Promise<Plan> {
  return api.post<Plan>(`/applications/${encodeURIComponent(applicationId)}/deployment-plans`, body, options)
}

export function getPlan(planId: string, options: ReadOptions = {}): Promise<Plan> {
  return api.get<Plan>(`/deployment-plans/${encodeURIComponent(planId)}`, options)
}

/* -------------------------------------------------------- Deployments §11 */

export interface DeploymentQuery extends PageQuery {
  status?: string
  environment?: string
}

export async function listDeployments(
  applicationId: string,
  query: DeploymentQuery = {},
  options: ReadOptions = {},
): Promise<Paginated<Deployment>> {
  const data = await api.get<unknown>(`/applications/${encodeURIComponent(applicationId)}/deployments`, {
    ...options,
    query: { page: query.page, limit: query.limit, status: query.status, environment: query.environment },
  })
  return normalizePage<Deployment>(data)
}

/**
 * Creates a deployment from a plan. The caller MUST pass a deterministic
 * Idempotency-Key (see workflow/idempotency) so double-clicks and retries
 * cannot create a second deployment from one plan.
 */
export function createDeployment(
  applicationId: string,
  planId: string,
  idempotencyKey: string,
  options: ReadOptions = {},
): Promise<Deployment> {
  return api.post<Deployment>(
    `/applications/${encodeURIComponent(applicationId)}/deployments`,
    { planId },
    { ...options, headers: { 'Idempotency-Key': idempotencyKey } },
  )
}

export function cancelDeployment(deploymentId: string, options: ReadOptions = {}): Promise<Deployment> {
  return api.post<Deployment>(`/deployments/${encodeURIComponent(deploymentId)}/cancel`, undefined, options)
}

/* ---------------------------------------------------- Steps / events §13–14 */

export function listSteps(deploymentId: string, options: ReadOptions = {}): Promise<DeploymentStep[]> {
  return api
    .get<unknown>(`/deployments/${encodeURIComponent(deploymentId)}/steps`, options)
    .then((data) => normalizeItems<DeploymentStep>(data))
}

export interface EventQuery {
  after?: number
  limit?: number
}

export async function listEvents(
  deploymentId: string,
  query: EventQuery = {},
  options: ReadOptions = {},
): Promise<{ items: DeploymentEvent[]; nextAfter: number | null }> {
  const data = await api.get<unknown>(`/deployments/${encodeURIComponent(deploymentId)}/events`, {
    ...options,
    query: { after: query.after, limit: query.limit },
  })
  const record = (data ?? {}) as { items?: unknown; nextAfter?: number | null }
  return {
    items: Array.isArray(record.items) ? (record.items as DeploymentEvent[]) : [],
    nextAfter: typeof record.nextAfter === 'number' ? record.nextAfter : null,
  }
}

export interface LogQuery {
  step?: string
  level?: string
  source?: string
  q?: string
  cursor?: string
  limit?: number
}

export async function listLogs(
  deploymentId: string,
  query: LogQuery = {},
  options: ReadOptions = {},
): Promise<{ items: LogEntry[]; nextCursor: string | null }> {
  const data = await api.get<unknown>(`/deployments/${encodeURIComponent(deploymentId)}/logs`, {
    ...options,
    query: {
      step: query.step,
      level: query.level,
      source: query.source,
      q: query.q,
      cursor: query.cursor,
      limit: query.limit,
    },
  })
  const record = (data ?? {}) as { items?: unknown; nextCursor?: string | null }
  return {
    items: Array.isArray(record.items) ? (record.items as LogEntry[]) : [],
    nextCursor: typeof record.nextCursor === 'string' ? record.nextCursor : null,
  }
}

/* ------------------------------------------------------------- Health §16 */

export function getDeploymentHealth(deploymentId: string, options: ReadOptions = {}): Promise<HealthResult> {
  return api.get<HealthResult>(`/deployments/${encodeURIComponent(deploymentId)}/health`, options)
}

/* ------------------------------------------------------------ Domains §17 */

export function listDomains(
  applicationId: string,
  environment?: string,
  options: ReadOptions = {},
): Promise<Domain[]> {
  return api
    .get<unknown>(`/applications/${encodeURIComponent(applicationId)}/domains`, {
      ...options,
      query: { environment },
    })
    .then(normalizeItems<Domain>)
}

export function createDomain(
  applicationId: string,
  body: { hostname: string; environment: string },
  options: ReadOptions = {},
): Promise<Domain> {
  return api.post<Domain>(`/applications/${encodeURIComponent(applicationId)}/domains`, body, options)
}

export function deleteDomain(domainId: string, options: ReadOptions = {}): Promise<void> {
  return apiFetch<void>(`/domains/${encodeURIComponent(domainId)}`, { ...options, method: 'DELETE' })
}

export function setPrimaryDomain(domainId: string, options: ReadOptions = {}): Promise<Domain> {
  return api.post<Domain>(`/domains/${encodeURIComponent(domainId)}/primary`, undefined, options)
}

export function checkDomain(domainId: string, options: ReadOptions = {}): Promise<Domain> {
  return api.post<Domain>(`/domains/${encodeURIComponent(domainId)}/check`, undefined, options)
}

/* ---------------------------------------------------------------- Servers §9 */

export interface ServerQuery {
  status?: string
}

export function listServers(query: ServerQuery = {}, options: ReadOptions = {}): Promise<Server[]> {
  return api
    .get<unknown>('/servers', { ...options, query: { status: query.status } })
    .then(normalizeItems<Server>)
}

export function getServer(serverId: string, options: ReadOptions = {}): Promise<Server> {
  return api.get<Server>(`/servers/${encodeURIComponent(serverId)}`, options)
}

/* ------------------------------------------------------------ Deployments §11 */

export function getDeployment(deploymentId: string, options: ReadOptions = {}): Promise<Deployment> {
  return api.get<Deployment>(`/deployments/${encodeURIComponent(deploymentId)}`, options)
}

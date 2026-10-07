import { api, apiFetch, type RequestOptions } from './client'
import { normalizePage, type PageQuery } from './pagination'
import type {
  Application,
  Deployment,
  GithubConnection,
  Paginated,
  Repository,
  Server,
  User,
} from './types'

type ReadOptions = Pick<RequestOptions, 'signal'>

/** Tolerates `{ items }` or a bare array. */
export function normalizeItems<T>(value: unknown): T[] {
  if (Array.isArray(value)) return value as T[]
  const items = (value as { items?: unknown } | null)?.items
  return Array.isArray(items) ? (items as T[]) : []
}

/* ------------------------------------------------------------------ Auth §2 */

export function getMe(options: ReadOptions = {}): Promise<User> {
  return api.get<User>('/auth/me', options)
}

/** Best-effort logout; the caller clears local state regardless. */
export function logout(): Promise<void> {
  return apiFetch<void>('/auth/logout', { method: 'POST' })
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

/* ----------------------------------------------------------- Applications §6 */

export async function listApplications(options: ReadOptions = {}): Promise<Paginated<Application>> {
  const data = await api.get<unknown>('/applications', options)
  return normalizePage<Application>(data)
}

export function getApplication(applicationId: string, options: ReadOptions = {}): Promise<Application> {
  return api.get<Application>(`/applications/${encodeURIComponent(applicationId)}`, options)
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

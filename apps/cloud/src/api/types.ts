/**
 * API resource types.
 *
 * These mirror docs/architecture/api-contract.md. They are intentionally
 * permissive (optional fields, index signatures) because #120 will refine them
 * as endpoints are wired. Unknown fields are never rendered.
 */

export interface ApiErrorEnvelope {
  error: {
    code: string
    message: string
    requestId?: string
    details?: Record<string, unknown>
  }
}

/** Pagination envelope — api-contract §21. */
export interface Paginated<T> {
  items: T[]
  page: number
  limit: number
  total: number
  /** Some collection endpoints (repositories) add this flag — api-contract §5. */
  truncated?: boolean
}

/** Non-paginated collection envelope, e.g. GET /github/connections. */
export interface Collection<T> {
  items: T[]
}

/** api-contract §2. */
export interface User {
  id: string
  name: string
}

/** api-contract §4. */
export interface GithubConnection {
  id: string
  accountLogin: string
  accountType: string
  status: 'active' | 'needs_attention' | 'disconnected'
  scopes?: string
  connectedAt?: string
  updatedAt?: string
}

/** api-contract §5. */
export interface Repository {
  id: string
  connectionId?: string
  externalId?: string
  fullName: string
  cloneUrl?: string
  htmlUrl?: string
  defaultBranch?: string
  private?: boolean
  language?: string
  pushedAt?: string
}

/** api-contract §6. Fields beyond `id`/`name` are not guaranteed in V0.1. */
export interface Application {
  id: string
  name: string
  repositoryId?: string
  createdAt?: string
  updatedAt?: string
}

/** api-contract §9. */
export type ServerStatus = 'ready' | 'degraded' | 'offline' | 'pending' | 'revoked' | 'unknown'

export interface Server {
  id: string
  name: string
  address?: string
  status: ServerStatus
  capabilities?: string[]
  lastSeenAt?: string
  createdAt?: string
}

/** api-contract §11. Status vocabulary is Engine-owned; render unknown raw. */
export interface Deployment {
  id: string
  status: string
  applicationId?: string
  environment?: 'production' | 'staging' | 'preview'
  createdAt?: string
  updatedAt?: string
  url?: string
  /** Human-readable deployment number (e.g. 42) when the Engine reports it. */
  number?: number
  serverId?: string
  planId?: string
  /** FAILED deployments carry a canonical error code (api-contract §18). */
  errorCode?: string
  createdBy?: string
  startedAt?: string
  completedAt?: string
}

/** api-contract §13. */
export interface DeploymentStep {
  name: string
  status: string
  startedAt?: string
  completedAt?: string
}

/** api-contract §14. */
export interface DeploymentEvent {
  id: string
  seq: number
  type: string
  version?: number
  deploymentId?: string
  occurredAt?: string
  requestId?: string
  data?: Record<string, unknown>
}

/** api-contract §14. */
export interface LogEntry {
  id: string
  deploymentId?: string
  occurredAt?: string
  level?: string
  step?: string
  source?: string
  message?: string
}

/** api-contract §16. */
export interface HealthResult {
  status?: string
  deploymentStatus?: string
  http?: { statusCode?: number; latencyMs?: number }
  checkedAt?: string
  attempt?: number
  body?: string
}

/** api-contract §17. */
export interface Domain {
  id: string
  applicationId?: string
  environment?: string
  hostname?: string
  isPrimary?: boolean
  dnsStatus?: string
  dnsExpected?: string
  dnsObserved?: string
  dnsCheckedAt?: string
  tlsStatus?: string
  routingStatus?: string
  target?: { deploymentId?: string; serverId?: string }
}

/** api-contract §5. */
export interface Ref {
  name: string
  type?: string
  commitSha?: string
  default?: boolean
}

/** api-contract §7. */
export type AnalysisStatus = 'COMPLETED' | 'FAILED' | string

export interface Analysis {
  analysisId: string
  applicationId?: string
  ref?: string
  commit?: string
  root?: string
  status?: AnalysisStatus
  analyzerVersion?: string
  profileVersion?: number
  result?: AnalysisResult
  createdAt?: string
  completedAt?: string
  errorCode?: string
}

/** api-contract §7 — `schemas/artifact.schema.json#/$defs/RepositoryAnalysis`. */
export interface AnalysisResult {
  analyzerVersion?: string
  root?: string
  findings?: Finding[]
  warnings?: string[]
}

export type FindingState = 'detected' | 'ambiguous' | 'not_detected' | 'unsupported' | 'not_applicable'

export interface Evidence {
  source?: string
  path?: string
  lines?: string
  effect?: 'supports' | 'conflicts'
  rule?: string
  explanation?: string
}

export interface Finding {
  kind?: string
  state?: FindingState
  value?: string
  values?: string[]
  confidence?: number
  candidates?: string[]
  evidence?: Evidence[]
}

/** api-contract §8 — `schemas/artifact.schema.json#/$defs/ApplicationProfile`. */
export type ProfileStatus = 'ready' | 'needs_review' | 'unsupported'
export type Provenance = 'override' | 'manifest' | 'detected' | 'default'

export interface ProfileValue<T> {
  value?: T
  provenance?: Provenance
  confidence?: number
  candidates?: T[]
  replaced?: T
}

export interface BlockingIssue {
  field?: string
  code?: string
  message?: string
  options?: string[]
}

export interface ConfigRequirement {
  name: string
  required?: boolean
  secret?: boolean
}

export interface HealthCheck {
  type?: string
  path?: string
}

export interface Profile {
  schemaVersion?: number
  version?: number
  analysisId?: string
  analyzerVersion?: string
  source?: { repositoryId?: string; ref?: string; commit?: string; root?: string }
  status?: ProfileStatus
  preset?: string
  summary?: string
  unsupported?: { code?: string; message?: string; detected?: string; alternatives?: string[] }
  blocking?: BlockingIssue[]
  language?: ProfileValue<string>
  runtimeVersion?: ProfileValue<string>
  framework?: ProfileValue<string>
  packageManager?: ProfileValue<string>
  buildCommand?: ProfileValue<string>
  startCommand?: ProfileValue<string>
  port?: ProfileValue<number>
  containerStrategy?: ProfileValue<string>
  healthCheck?: ProfileValue<HealthCheck>
  configuration?: ConfigRequirement[]
  confidence?: number
}

/** api-contract §10. */
export interface Plan {
  id: string
  status?: string
  applicationProfileVersion?: number
  serverId?: string
  environment?: string
  steps?: string[]
  healthCheck?: HealthCheck
  rollback?: { strategy?: string }
}

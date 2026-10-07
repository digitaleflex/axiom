# Axiom REST API & Realtime Contract

## Status

- Version: `v1`
- Scope: Axiom V0.1
- Canonical consumer: Cloud Console
- Secondary consumers: Admin Console and CLI
- Transport: HTTP REST + realtime deployment events
- Source of truth: Axiom Engine

## Purpose

This contract defines the stable application boundary between Axiom clients and the Go Engine.

Clients never communicate directly with Docker, Traefik, Runtime Agents, PostgreSQL or deployment servers. All platform operations go through the Engine API.

The API exposes the deployment workflow:

`GitHub → Analyze → Detect → Plan → Build → Deploy → Health → Live`

## Contract principles

1. Clients consume resources, not infrastructure internals.
2. Every mutating deployment operation is authenticated and authorized.
3. Deployment state is authoritative in PostgreSQL through the Engine.
4. Long-running operations return a deployment/job identity and are observed asynchronously.
5. Every deployment request supports idempotency.
6. Errors use a stable machine-readable envelope.
7. API versions are explicit.
8. Sensitive values are never returned in logs or API responses.
9. The Cloud UI and CLI use the same contracts.
10. Runtime Agent operations are never exposed as arbitrary public shell commands.

---

## 1. Base URL and versioning

V0.1 API prefix:

`/api/v1`

Health endpoints remain outside the API namespace:

- `GET /health`
- `GET /ready`

The API version changes only for breaking contract changes.

---

## 2. Authentication

V0.1 requires an authenticated user for protected endpoints.

### Required request headers

```http
Authorization: Bearer <access-token>
Content-Type: application/json
X-Request-ID: <optional-client-request-id>
```

For mutating operations that create or execute deployments:

```http
Idempotency-Key: <unique-operation-key>
```

### Authentication endpoints

| Method | Endpoint | Purpose |
|---|---|---|
| GET | `/api/v1/auth/me` | Return current user (`{ "id", "name" }`) |
| POST | `/api/v1/auth/logout` | End current session (`204`) |

Interim (until #125): a single bearer token configured with `AXIOM_API_TOKEN` (required in production); development without a token authenticates as a local operator. Missing/invalid credentials → `401 UNAUTHORIZED` with `WWW-Authenticate: Bearer`.

The concrete identity provider is an implementation detail of the Engine and must not leak into the deployment API.

---

## 3. Common resource identifiers

Resources use opaque string IDs.

Example:

```json
{
  "id": "dep_01J...",
  "status": "BUILDING"
}
```

Clients must not infer database structure from identifiers.

---

## 4. GitHub connections

### Connect

`POST /api/v1/github/connections`

Starts the configured OAuth / GitHub App user authorization flow. Response `200 { "authorizeUrl": "https://github.com/login/oauth/authorize?..." }` and an `HttpOnly`, `SameSite=Lax` cookie scoped to the callback path that binds the flow to the browser. The client navigates the browser to `authorizeUrl`. `503 SERVICE_UNAVAILABLE` when GitHub is not configured.

### Callback (browser)

`GET /api/v1/github/callback?code=…&state=…` — public endpoint registered on GitHub. The state is single-use, expires after 10 minutes, is bound to the user who started the flow and to the starting browser; PKCE (S256) protects the code exchange. Always redirects (`302`) to `{AXIOM_CONSOLE_URL}/github?result=connected|denied|error`; never renders tokens.

### List connections

`GET /api/v1/github/connections` → `{ "items": [ { "id": "ghc_…", "accountLogin": "octocat", "accountType": "User", "status": "active|needs_attention|disconnected", "scopes": "", "connectedAt": "…", "updatedAt": "…" } ] }`

### Disconnect

`DELETE /api/v1/github/connections/{connectionId}` → `204`. Revokes the GitHub grant (best effort) and wipes stored tokens; repository access through this connection fails afterwards.

Tokens are never returned by these endpoints. They are stored encrypted (AES-256-GCM, `AXIOM_SECRET_KEY`) and bound to their connection.

---

## 5. Repositories

### List repositories

`GET /api/v1/github/connections/{connectionId}/repositories`

Query parameters:

- `page`
- `limit`
- `search`

Response:

```json
{
  "items": [
    {
      "id": "repo_123",
      "externalId": "123456",
      "fullName": "owner/application",
      "cloneUrl": "https://github.com/owner/application.git",
      "defaultBranch": "main",
      "private": true
    }
  ],
  "page": 1,
  "limit": 20,
  "total": 1
}
```

Repositories are sorted by `fullName` (case-insensitive); `search` is a case-insensitive substring of `fullName` (≤ 100 chars). IDs (`repo_…`) are stable per connection and GitHub repository (`externalId`). Listing is capped at 1000 repositories per connection; `"truncated": true` signals the cap was reached.

### Get repository

`GET /api/v1/repositories/{repositoryId}` — refreshes metadata from GitHub. Fields: `id`, `connectionId`, `externalId`, `fullName`, `cloneUrl`, `htmlUrl`, `defaultBranch`, `private`, `language` (as reported by GitHub, not Axiom's detection), `pushedAt`.

### List branches/refs

`GET /api/v1/repositories/{repositoryId}/refs` → `{ "items": [ { "name": "main", "type": "branch", "commitSha": "…", "default": true }, { "name": "v1.0.0", "type": "tag", … } ] }` — default branch first, then branches by name, then tags by name.

GitHub errors are normalized: `404 NOT_FOUND` (repository, connection or ref), `409 CONFLICT` with `details.reason = "github_reconnect_required"` (token revoked, disconnected or needs attention), `403 FORBIDDEN`, `429 RATE_LIMITED`, `503 SERVICE_UNAVAILABLE` (GitHub unavailable or timed out).

The selected ref becomes part of the application/deployment source definition.

---

## 6. Applications

An Application represents a deployable project derived from a repository.

### Create application

`POST /api/v1/applications`

Request:

```json
{
  "repositoryId": "repo_123",
  "name": "my-app"
}
```

### Get application

`GET /api/v1/applications/{applicationId}`

### List applications

`GET /api/v1/applications`

---

## 7. Repository analysis

### Start analysis

`POST /api/v1/applications/{applicationId}/analysis`

Request:

```json
{
  "ref": "main",
  "root": "apps/web"
}
```

`root` is optional (monorepos; stored for subsequent analyses). The analysis runs synchronously within a bounded time: the ref is resolved to an exact commit, a bounded read-only snapshot is fetched, evidence is collected and a new Application Profile revision is created.

Response `201 Created` (`Location: /api/v1/applications/{id}/analysis/{analysisId}`):

```json
{
  "analysisId": "analysis_123",
  "applicationId": "app_123",
  "ref": "main",
  "commit": "3f9c2a1…",
  "root": "",
  "status": "COMPLETED",
  "analyzerVersion": "evidence/1.0.0",
  "profileVersion": 3,
  "result": { "analyzerVersion": "evidence/1.0.0", "root": "", "findings": [ … ], "warnings": [] },
  "createdAt": "…",
  "completedAt": "…"
}
```

A failed analysis is still `201` with `status: "FAILED"` and `errorCode`: `REF_NOT_FOUND`, `SOURCE_ACCESS_FAILED`, `SNAPSHOT_REJECTED` (oversized, malformed, unsafe or empty source). No profile revision is created.

Analysis must inspect repository metadata and files without executing untrusted project code.

### Get analysis

`GET /api/v1/applications/{applicationId}/analysis/{analysisId}`

The result contains evidence and confidence for detected characteristics (`schemas/artifact.schema.json#/$defs/RepositoryAnalysis`): each finding has `kind`, `state` (`detected`, `ambiguous`, `not_detected`, `unsupported`, `not_applicable`), `value` / `values`, `confidence` (0–1), `candidates`, and `evidence[]` (`source`, `path`, `lines`, `effect`, `rule`, `explanation`).

---

## 8. Application Profile / Stack Detection

### Get current profile

`GET /api/v1/applications/{applicationId}/profile`

Returns the latest profile revision (`schemas/artifact.schema.json#/$defs/ApplicationProfile`, `schemaVersion: 1`). `404 NOT_FOUND` before the first successful analysis.

Every value is an object `{ "value", "provenance", "confidence", "candidates", "replaced" }` with provenance `override` › `manifest` › `detected` › `default`. Defaults are never reported as detected.

```json
{
  "schemaVersion": 1,
  "version": 3,
  "status": "ready",
  "preset": "nextjs",
  "summary": "Axiom will build this Next.js app with pnpm and run it as a container on port 3000.",
  "blocking": [],
  "framework": { "value": "Next.js", "provenance": "detected", "confidence": 0.98 },
  "packageManager": { "value": "pnpm", "provenance": "detected", "confidence": 1 },
  "buildCommand": { "value": "pnpm run build", "provenance": "detected", "confidence": 0.95 },
  "startCommand": { "value": "pnpm start", "provenance": "detected", "confidence": 0.95 },
  "port": { "value": 3000, "provenance": "default" },
  "containerStrategy": { "value": "source", "provenance": "default" },
  "healthCheck": { "value": { "type": "http", "path": "/" }, "provenance": "default" },
  "configuration": [ { "name": "DATABASE_URL", "required": true, "secret": true } ],
  "confidence": 0.95
}
```

`status`: `ready` (deployable), `needs_review` (`blocking[]` items with `field`, `code` = `ambiguous` | `low_confidence` | `not_detected` | `invalid_override`, `options`), `unsupported` (`unsupported.code` e.g. `UNSUPPORTED_FRAMEWORK`, `MISSING_START_COMMAND`, `AMBIGUOUS_APPLICATION_ROOT`, with `alternatives`). Only `ready` profiles can be planned.

### Replace overrides

`PUT /api/v1/applications/{applicationId}/profile/overrides`

```json
{ "packageManager": "pnpm", "port": 8080, "buildCommand": "pnpm build", "startCommand": "pnpm start", "healthPath": "/healthz", "entrypoint": "./cmd/api", "strategy": "dockerfile", "publicService": "web" }
```

All fields optional; omitted fields revert to detection. Rebuilds the profile from the latest analysis without accessing the repository and returns the new revision. Unsafe commands are reported as `invalid_override` blocking issues and never applied.

The profile is versioned and traceable to an analysis (`analysisId`, `source.commit`).

---

## 9. Servers

### List servers

`GET /api/v1/servers?status={ready|degraded|offline|pending|revoked|unknown}`

`status` is optional. Reported `status` is the *effective* status: a ready or degraded server whose heartbeat is older than 5 minutes reads as `offline` (unknown freshness is never assumed dead).

### Get server

`GET /api/v1/servers/{serverId}`

### Register server

`POST /api/v1/servers`

```json
{ "name": "srv-eu-1", "address": "203.0.113.10" }
```

`name` is a lowercase slug (1–63 chars); `address` is `host` or `host:port` without scheme. Response `201 Created` (`Location: /api/v1/servers/{id}`) with the record in `pending` status. The Runtime Agent binds to the record during registration (#76), which establishes the trust boundary; the record alone grants nothing.

### Rename server

`PATCH /api/v1/servers/{serverId}` — `{ "name": "srv-eu-2" }`.

### Remove server

`DELETE /api/v1/servers/{serverId}` → `204`. Refused with `409 CONFLICT` (`details.reason = "server_in_use"`) while active (non-FAILED/CANCELLED) deployments reference it, or `"server_has_history"` while plans or past deployments reference it.

### Server status

`GET /api/v1/servers/{serverId}/health`

A server is eligible for deployment only when its effective status is ready (degraded proceeds with explicit acknowledgement in the Console) and its capabilities satisfy the plan: `docker` always, `docker_compose` for Compose presets, `traefik` when a domain is served. The executor re-verifies eligibility after planning and before building; a server that went offline in between fails the deployment with `DEPLOYMENT_NOT_ELIGIBLE` before any work runs.

---

## 10. Deployment Plans

### Generate plan

`POST /api/v1/applications/{applicationId}/deployment-plans`

Request:

```json
{
  "serverId": "srv_123",
  "ref": "main",
  "domain": "app.example.com"
}
```

Response:

```json
{
  "id": "plan_123",
  "status": "READY",
  "applicationProfileVersion": 3,
  "steps": [
    "BUILD",
    "CREATE_RUNTIME",
    "NETWORK",
    "START",
    "VERIFY"
  ],
  "healthCheck": {
    "type": "http",
    "path": "/"
  }
}
```

A plan must be reviewable before execution.

### Get plan

`GET /api/v1/deployment-plans/{planId}`

---

## 11. Deployments

### Create deployment

`POST /api/v1/applications/{applicationId}/deployments`

Headers:

```http
Idempotency-Key: deploy-unique-key
```

Request:

```json
{
  "planId": "plan_123"
}
```

Server and environment are taken from the plan; the request accepts only `planId` (unknown fields are rejected). A plan is single-use.

Response:

```http
HTTP/1.1 202 Accepted
Location: /api/v1/deployments/dep_123
Idempotent-Replayed: true        (only when an Idempotency-Key replay returned an existing deployment)
```

```json
{
  "id": "dep_123",
  "number": 42,
  "applicationId": "app_123",
  "serverId": "srv_123",
  "environment": "production",
  "planId": "plan_123",
  "status": "PENDING",
  "createdAt": "2026-10-07T14:01:50Z",
  "updatedAt": "2026-10-07T14:01:50Z"
}
```

Deployment resources also carry, when set: `url` (LIVE), `errorCode` (FAILED, §18), `createdBy`, `startedAt`, `completedAt`.

Errors: `404 NOT_FOUND` (application or plan), `409 CONFLICT` (plan already used, or Idempotency-Key reused with a different request), `422 VALIDATION_FAILED` (missing `planId`).

### Get deployment

`GET /api/v1/deployments/{deploymentId}`

### List deployments

`GET /api/v1/applications/{applicationId}/deployments`

Query parameters:

- `page`
- `limit`
- `status` (canonical status)
- `environment` (`production`, `staging`, `preview`)

### Cancel deployment

`POST /api/v1/deployments/{deploymentId}/cancel`

Cancellation is allowed in every non-terminal state except `VERIFYING` (the outcome belongs to health verification). Returns the updated deployment (`status: CANCELLED`); otherwise `409 DEPLOYMENT_INVALID_STATE`.

---

## 12. Deployment lifecycle

Canonical V0.1 state machine:

```text
PENDING
   ↓
ANALYZING
   ↓
PLANNING
   ↓
BUILDING
   ↓
DEPLOYING
   ↓
VERIFYING
   ├──→ LIVE
   └──→ FAILED

Any non-terminal state → FAILED
Any non-terminal state except VERIFYING → CANCELLED (via cancel)
```

Terminal states: `LIVE`, `FAILED`, `CANCELLED`.

The Engine is the authoritative owner of deployment state.

Invalid transitions must return a conflict/error response rather than being silently accepted.

---

## 13. Deployment steps

### List steps

`GET /api/v1/deployments/{deploymentId}/steps`

Example:

```json
{
  "items": [
    {
      "name": "BUILD",
      "status": "COMPLETED",
      "startedAt": "...",
      "completedAt": "..."
    },
    {
      "name": "START",
      "status": "RUNNING",
      "startedAt": "..."
    }
  ]
}
```

---

## 14. Logs and events

### Deployment logs

`GET /api/v1/deployments/{deploymentId}/logs`

Query parameters:

- `step`
- `level`
- `cursor`
- `limit`

### Deployment events

`GET /api/v1/deployments/{deploymentId}/events?after={seq}&limit={n}`

Returns persisted events ordered by `seq` (strictly increasing per deployment, starting at 1). `after` resumes a timeline; `nextAfter` is set when more events may exist.

```json
{ "items": [ { "id": "evt_…", "seq": 2, "type": "deployment.status.changed", "version": 1,
               "deploymentId": "dep_123", "occurredAt": "…", "data": { "from": "PENDING", "status": "ANALYZING" } } ],
  "nextAfter": null }
```

Event types: `deployment.created`, `deployment.status.changed`, `deployment.step.started`, `deployment.step.completed`, `deployment.step.failed`, `deployment.step.skipped`.

Events include:

- deployment state changes
- step started/completed/failed
- build output references
- runtime events
- health-check results
- security/policy decisions
- final outcome

Secrets and credentials must be redacted before persistence or transmission.

---

## 15. Realtime deployment updates

The canonical realtime interface for V0.1 is Server-Sent Events (SSE).

Endpoint:

`GET /api/v1/deployments/{deploymentId}/events/stream`

Headers:

```http
Accept: text/event-stream
```

Example:

```text
event: deployment.step.started
data: {"deploymentId":"dep_123","step":"BUILD"}

event: deployment.step.completed
data: {"deploymentId":"dep_123","step":"BUILD"}

event: deployment.status.changed
data: {"deploymentId":"dep_123","status":"DEPLOYING"}

event: deployment.status.changed
data: {"deploymentId":"dep_123","status":"LIVE","url":"https://app.example.com"}
```

Stream semantics (implemented, #118):

- Each frame carries `id: <seq>` (the event's per-deployment sequence), `event: <type>` and `data: <event envelope JSON>`.
- On connect the Engine replays persisted events after `Last-Event-ID` (header, sent automatically by browsers on reconnect) or the `lastEventId` query parameter, then continues live — in order, without duplicates or gaps.
- `retry: 3000` is advertised; `: ping` comment frames are sent every 15 s.
- The stream ends after the terminal status event (`LIVE`, `FAILED`, `CANCELLED`), on client disconnect, or on Engine shutdown. Reconnecting after the terminal event returns an empty stream that closes immediately.
- Invalid `Last-Event-ID` → `400`; unknown or inaccessible deployment → `404`.

SSE is intentionally selected for V0.1 because the primary realtime requirement is server → client deployment progress. A bidirectional WebSocket protocol may be introduced later if product requirements justify it.

---

## 16. Health

### Engine health

`GET /health`

Liveness only.

### Engine readiness

`GET /ready`

Readiness checks required dependencies.

### Deployment health

`GET /api/v1/deployments/{deploymentId}/health`

Example:

```json
{
  "status": "HEALTHY",
  "http": {
    "statusCode": 200,
    "latencyMs": 84
  },
  "runtime": {
    "status": "RUNNING"
  }
}
```

A deployment cannot transition to `LIVE` before successful health verification.

---

## 17. Domains

### List application domains

`GET /api/v1/applications/{applicationId}/domains`

### Add domain

`POST /api/v1/applications/{applicationId}/domains`

Request:

```json
{
  "hostname": "app.example.com"
}
```

### Remove domain

`DELETE /api/v1/domains/{domainId}`

Traefik and Let's Encrypt remain implementation details behind the Engine boundary.

---

## 18. Error contract

All API errors use:

```json
{
  "error": {
    "code": "DEPLOYMENT_NOT_ELIGIBLE",
    "message": "The selected server is not eligible for this deployment.",
    "requestId": "req_123",
    "details": {
      "serverId": "srv_123"
    }
  }
}
```

### Required error classes

- `INVALID_REQUEST`
- `UNAUTHORIZED`
- `FORBIDDEN`
- `NOT_FOUND`
- `CONFLICT`
- `RATE_LIMITED`
- `VALIDATION_FAILED`
- `DEPLOYMENT_NOT_ELIGIBLE`
- `DEPLOYMENT_INVALID_STATE`
- `BUILD_FAILED`
- `RUNTIME_FAILED`
- `HEALTH_CHECK_FAILED`
- `POLICY_DENIED`
- `INTERNAL_ERROR`
- `SERVICE_UNAVAILABLE` — a required dependency (e.g. database) is unavailable (HTTP 503)

Every response carries `X-Request-ID` (client value echoed when it matches `[A-Za-z0-9._-]{1,64}`, otherwise generated); the same value is `error.requestId`. `details` is always an object; validation errors use `details.fields`.

Request bodies must be `application/json` (else `415 INVALID_REQUEST`), at most 1 MiB (else `413 INVALID_REQUEST`), a single JSON object without unknown fields (else `400 INVALID_REQUEST`).

The human-readable message is not the stable machine contract; clients must branch on the error code.

---

## 19. HTTP status conventions

| Status | Meaning |
|---|---|
| 200 | Successful read/action |
| 201 | Resource created |
| 202 | Long-running operation accepted |
| 204 | Successful deletion/no body |
| 400 | Invalid request |
| 401 | Missing/invalid authentication |
| 403 | Authenticated but unauthorized |
| 404 | Resource not found |
| 409 | State/idempotency/conflict |
| 413 | Request body too large |
| 415 | Unsupported content type |
| 422 | Validation failure |
| 429 | Rate limited |
| 500 | Unexpected Engine failure |
| 503 | Dependency/readiness failure |

---

## 20. Idempotency

Mutating deployment operations must accept `Idempotency-Key`.

For the same authenticated actor + operation + key:

- the first accepted request creates the operation;
- retries return the original operation identity;
- a key cannot silently create a second deployment;
- conflicting reuse of a key must return `409 CONFLICT`.

The Engine must persist the idempotency result sufficiently to survive restart.

---

## 21. Pagination

Collection endpoints use:

```text
?page=1&limit=20
```

Response:

```json
{
  "items": [],
  "page": 1,
  "limit": 20,
  "total": 0
}
```

Maximum `limit` is controlled by the Engine.

---

## 22. Security boundary

The Cloud/API layer may request:

- repository analysis
- deployment plan generation
- deployment execution
- status/log retrieval
- domain configuration

The API may not expose:

- arbitrary shell execution
- arbitrary Docker commands
- unrestricted Traefik configuration
- raw GitHub access tokens
- database credentials
- Runtime Agent credentials

The Engine authorizes and translates high-level operations into bounded Agent operations.

---

## 23. Deployment event envelope

All realtime and persisted deployment events use a common envelope:

```json
{
  "id": "evt_123",
  "type": "deployment.status.changed",
  "version": 1,
  "deploymentId": "dep_123",
  "occurredAt": "2026-09-22T00:00:00Z",
  "requestId": "req_123",
  "data": {
    "status": "LIVE"
  }
}
```

Event payloads must remain free of secrets.

---

## 24. V0.1 contract boundary

The following are intentionally outside this contract:

- billing
- Kubernetes
- multi-cloud orchestration
- GitLab
- advanced autonomous AI operations
- marketplace
- advanced backup orchestration
- arbitrary remote shell
- full organization/RBAC model beyond the minimum authentication boundary

These can receive new versioned contracts later.

---

## 25. V0.1 acceptance test

A conforming implementation must support:

```text
Authenticate
  ↓
Connect GitHub
  ↓
List repositories
  ↓
Select repository/ref
  ↓
Analyze
  ↓
Read Application Profile
  ↓
Select eligible server
  ↓
Generate Deployment Plan
  ↓
Review plan
  ↓
Create deployment
  ↓
Receive realtime progress
  ↓
Observe BUILD → DEPLOYING → VERIFYING
  ↓
Health check passes
  ↓
Deployment becomes LIVE
  ↓
Return live URL
  ↓
Read logs/events without SSH
```

The Cloud Console, CLI and future clients must be able to execute this workflow using only the documented Engine contracts.

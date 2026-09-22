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
| GET | `/api/v1/auth/me` | Return current user |
| POST | `/api/v1/auth/logout` | End current session |

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

Creates a GitHub connection through the configured OAuth/App flow.

### List connections

`GET /api/v1/github/connections`

### Disconnect

`DELETE /api/v1/github/connections/{connectionId}`

Tokens are never returned by these endpoints.

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

### Get repository

`GET /api/v1/repositories/{repositoryId}`

### List branches/refs

`GET /api/v1/repositories/{repositoryId}/refs`

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
  "ref": "main"
}
```

Response:

```json
{
  "analysisId": "analysis_123",
  "status": "COMPLETED"
}
```

Analysis must inspect repository metadata and files without executing untrusted project code.

### Get analysis

`GET /api/v1/applications/{applicationId}/analysis/{analysisId}`

The result contains evidence and confidence for detected characteristics.

---

## 8. Application Profile / Stack Detection

### Get current profile

`GET /api/v1/applications/{applicationId}/profile`

Example:

```json
{
  "language": "TypeScript",
  "framework": "Next.js",
  "packageManager": "pnpm",
  "buildCommand": "pnpm build",
  "startCommand": "pnpm start",
  "port": 3000,
  "containerStrategy": "docker",
  "services": [],
  "confidence": 0.98
}
```

The profile is versioned and traceable to an analysis.

---

## 9. Servers

### List servers

`GET /api/v1/servers`

### Get server

`GET /api/v1/servers/{serverId}`

### Register server

`POST /api/v1/servers`

The registration flow must establish the Runtime Agent trust boundary.

### Server status

`GET /api/v1/servers/{serverId}/health`

A server is eligible for deployment only when its Agent, Docker/runtime capabilities and health satisfy the Deployment Plan requirements.

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

Response:

```http
HTTP/1.1 202 Accepted
```

```json
{
  "id": "dep_123",
  "status": "PENDING"
}
```

### Get deployment

`GET /api/v1/deployments/{deploymentId}`

### List deployments

`GET /api/v1/applications/{applicationId}/deployments`

Query parameters:

- `page`
- `limit`
- `status`

### Cancel deployment

`POST /api/v1/deployments/{deploymentId}/cancel`

Cancellation is allowed only when the current state permits it.

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
```

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

`GET /api/v1/deployments/{deploymentId}/events`

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

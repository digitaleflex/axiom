# Axiom Engine — Operational Failure Matrix & Runbook

> Issue #104. V0.1 operational runbook for the Axiom Engine.
>
> Scope: GitHub, analysis, build, Agent offline, Docker, Traefik/TLS, health, database failures; safe recovery actions.
>
> Every critical failure has a detection signal, likely cause, safe operator action and escalation boundary.

## 1. How to use this runbook

1. Start at the **On-call quick-reference** (§2) — top 10 symptoms → actions.
2. For detail, go to the failure domain section (§3–§11).
3. Each failure lists: **Detection signal** (exact log line, metric or API symptom with real codes), **Likely causes** (2–4), **Safe operator action** (exact commands where safe), **Escalation boundary** (when to stop and ask).
4. Commands marked **READ** are safe to run anytime. Commands marked **RESTART** restart a service and may drop in-flight work — confirm first. Commands marked **DESTRUCTIVE** are never run without explicit human confirmation.

## 2. On-call quick-reference (top 10 symptoms → actions)

| # | Symptom | Immediate action | Section |
|---|---|---|---|
| 1 | `GET /ready` returns 503 `database_unavailable` | Check PostgreSQL: `systemctl status postgresql`; if down, `sudo systemctl restart postgresql` (RESTART). If Engine won't start, check logs for `database migrations failed`. | §10 |
| 2 | Deployment stuck in `BUILDING` > 20 min | `GET /api/v1/deployments/{id}/steps` — if step `BUILD` is `RUNNING`, check Docker daemon: `sudo systemctl status docker`. If daemon down, `sudo systemctl restart docker` (RESTART). | §5 |
| 3 | Deployment `FAILED` with `errorCode: BUILD_FAILED` | `GET /api/v1/deployments/{id}/logs?step=BUILD&level=error` — read the build output tail. Common: `BUILD_NO_DOCKERFILE`, `BUILD_SOURCE_FAILED`, `docker build exited with code N`. | §5 |
| 4 | Deployment `FAILED` with `errorCode: RUNTIME_FAILED` | `GET /api/v1/deployments/{id}/steps` — which step failed? `CREATE_RUNTIME`, `NETWORK` or `START`? Check server status: `GET /api/v1/servers/{serverId}/health`. | §6 |
| 5 | Deployment `FAILED` with `errorCode: HEALTH_CHECK_FAILED` | `GET /api/v1/deployments/{id}/health` — what status code / reason? Check the app is actually serving on the expected port. | §8 |
| 6 | Deployment `FAILED` with `errorCode: DEPLOYMENT_NOT_ELIGIBLE` | `GET /api/v1/servers/{serverId}` — is status `ready` or `degraded`? Is `lastSeenAt` older than 5 minutes? Agent is offline. | §6 |
| 7 | Deployment `FAILED` with `errorCode: POLICY_DENIED` | Plan was tampered with or contains a secret value. Regenerate the plan. Do not bypass policy. | §4 |
| 8 | GitHub operations return `409 CONFLICT` with `details.reason: github_reconnect_required` | Connection token was revoked or expired. User must reconnect GitHub. | §3 |
| 9 | GitHub operations return `429 RATE_LIMITED` | GitHub API rate limit hit. Wait and retry; do not hammer. | §3 |
| 10 | SSE stream disconnects / events missing | Check `GET /api/v1/deployments/{id}/events?after={seq}` for the missing range. Reconnect with `Last-Event-ID`. | §9 |

## 3. GitHub failures

### 3.1 Connection / OAuth flow

**Detection signal**
- `POST /api/v1/github/connections` returns `503 SERVICE_UNAVAILABLE` ("GitHub integration is not configured") — `AXIOM_GITHUB_CLIENT_ID` not set.
- `GET /api/v1/github/callback` redirects to `{AXIOM_CONSOLE_URL}/github?result=error` — callback rejected.
- Log line: `github callback rejected` with `reason` field.

**Likely causes**
1. GitHub OAuth app misconfiguration (wrong `AXIOM_GITHUB_CLIENT_ID` / `AXIOM_GITHUB_CLIENT_SECRET` / `AXIOM_GITHUB_REDIRECT_URL`).
2. `AXIOM_SECRET_KEY` missing or invalid when GitHub is configured — Engine fails fast at startup.
3. GitHub OAuth app callback URL does not match `AXIOM_GITHUB_REDIRECT_URL`.
4. User denied the authorization (`error=access_denied` → `result=denied`).

**Safe operator action**
- READ: `GET /api/v1/github/connections` — list connections and their status (`active`, `needs_attention`, `disconnected`).
- READ: Check Engine startup logs for `GitHub integration disabled` or `AXIOM_SECRET_KEY is required when GitHub is configured`.
- RESTART: If config was corrected, restart the Engine to pick up new env vars.

**Escalation boundary**
- If the OAuth app itself is broken (GitHub-side), escalate to the platform team — the Engine cannot fix GitHub app configuration.
- If tokens are suspected compromised, use `DELETE /api/v1/github/connections/{connectionId}` to revoke and wipe stored tokens, then reconnect.

### 3.2 Repository discovery

**Detection signal**
- `GET /api/v1/github/connections/{connectionId}/repositories` returns `404 NOT_FOUND` ("GitHub connection or repository not found").
- `GET /api/v1/repositories/{repositoryId}` returns `404 NOT_FOUND`.
- `GET /api/v1/repositories/{repositoryId}/refs` returns `404 NOT_FOUND` ("ref not found").

**Likely causes**
1. Connection is `disconnected` or `needs_attention` — token invalid.
2. Repository does not exist or was renamed.
3. Ref (branch/tag) does not exist.
4. Token lacks required scopes.

**Safe operator action**
- READ: `GET /api/v1/github/connections` — check connection status.
- READ: `GET /api/v1/repositories/{repositoryId}` — verify repository exists and is accessible.
- If connection is `needs_attention` or `disconnected`, user must reconnect GitHub.

**Escalation boundary**
- If the connection is healthy but repository is still not found, the token may lack scopes — escalate to platform team to review OAuth scope configuration.

### 3.3 Rate limiting

**Detection signal**
- Any GitHub-backed API returns `429 RATE_LIMITED` ("GitHub rate limit reached; retry later").
- Log line: `github: rate limited` (from `repos.ErrRateLimited`).

**Likely causes**
1. GitHub API rate limit exhausted (primary or secondary).
2. Too many concurrent analyses or deployments hitting GitHub API.
3. Shared GitHub App installation quota exhausted.

**Safe operator action**
- READ: Identify which operations are rate-limited via `GET /api/v1/deployments` and `GET /api/v1/applications/{id}/analysis/{analysisId}`.
- Wait for the rate limit window to reset (GitHub typically 1 hour for primary, shorter for secondary).
- Reduce concurrency: avoid triggering multiple analyses simultaneously.

**Escalation boundary**
- If rate limits persist after waiting, escalate to platform team — may need GitHub App installation token or increased quota.

## 4. Analysis failures

### 4.1 Ref not found

**Detection signal**
- `POST /api/v1/applications/{applicationId}/analysis` returns `201` with `status: "FAILED"` and `errorCode: "REF_NOT_FOUND"`.
- Log line: `analysis failed` with `errorCode=REF_NOT_FOUND`.

**Likely causes**
1. Ref name is invalid (contains `..`, starts/ends with `/`, ends with `.lock`, or contains `//`).
2. Ref does not exist in the repository (branch deleted, tag not pushed).
3. Repository connection is broken.

**Safe operator action**
- READ: `GET /api/v1/repositories/{repositoryId}/refs` — list available refs.
- READ: `GET /api/v1/applications/{applicationId}/analysis/{analysisId}` — get failure detail.
- Retry with a valid ref name.

**Escalation boundary**
- If the ref exists in GitHub but analysis still fails, escalate — may be a GitHub API issue.

### 4.2 Source access failed

**Detection signal**
- Analysis `FAILED` with `errorCode: "SOURCE_ACCESS_FAILED"`.
- Log line: `analysis failed` with `errorCode=SOURCE_ACCESS_FAILED`.

**Likely causes**
1. GitHub token invalid or expired (connection `needs_attention`).
2. Repository is private and token lacks access.
3. GitHub API timeout or unavailable.
4. Archive download failed.

**Safe operator action**
- READ: `GET /api/v1/github/connections` — check connection status.
- READ: `GET /api/v1/applications/{applicationId}/analysis/{analysisId}` — get failure detail.
- If connection is `needs_attention`, reconnect GitHub.

**Escalation boundary**
- If connection is healthy but source access fails, escalate to platform team.

### 4.3 Snapshot rejected

**Detection signal**
- Analysis `FAILED` with `errorCode: "SNAPSHOT_REJECTED"`.
- Log line: `analysis failed` with `errorCode=SNAPSHOT_REJECTED`.

**Likely causes**
1. Repository exceeds size limits: archive > 200 MiB compressed, total > 1 GiB uncompressed, > 50 000 files.
2. Archive is malformed (corrupt tarball).
3. Archive contains unsafe paths (path traversal, absolute paths, symlinks).
4. Archive is empty.

**Safe operator action**
- READ: `GET /api/v1/applications/{applicationId}/analysis/{analysisId}` — get failure detail.
- If oversized, reduce repository size (remove large binaries, use `.gitignore`).
- If malformed, check repository integrity in GitHub.

**Escalation boundary**
- If the repository is within limits but still rejected, escalate — may be a snapshot parser bug.

### 4.4 Unsupported stack

**Detection signal**
- `GET /api/v1/applications/{applicationId}/profile` returns profile with `status: "unsupported"` and `unsupported.code` (e.g. `UNSUPPORTED_FRAMEWORK`, `MISSING_START_COMMAND`, `AMBIGUOUS_APPLICATION_ROOT`).

**Likely causes**
1. Framework not in the supported preset list.
2. No start command detected and none provided.
3. Monorepo root is ambiguous.

**Safe operator action**
- READ: `GET /api/v1/applications/{applicationId}/profile` — read `unsupported.alternatives`.
- Use `PUT /api/v1/applications/{applicationId}/profile/overrides` to provide explicit values (e.g. `startCommand`, `entrypoint`, `root`).
- Re-analyze after overrides.

**Escalation boundary**
- If the framework should be supported but isn't, escalate to platform team — may need a new preset.

## 5. Build failures

### 5.1 Build failed (exit code)

**Detection signal**
- Deployment `FAILED` with `errorCode: "BUILD_FAILED"`.
- `GET /api/v1/deployments/{id}/steps` — step `BUILD` is `FAILED` with `exitCode` set.
- `GET /api/v1/deployments/{id}/logs?step=BUILD&level=error` — build output tail.
- Log line: `docker build exited with code N` (from `build.CodeBuildFailed`).

**Likely causes**
1. Dockerfile syntax error or build command failure.
2. Base image pull failure (network or registry issue).
3. Build context too large or contains invalid files.
4. Build timeout (20 minutes for executor, 10 minutes for builder).

**Safe operator action**
- READ: `GET /api/v1/deployments/{id}/logs?step=BUILD&level=error` — read the build output tail (up to 100 lines by default, max 1000).
- READ: `GET /api/v1/deployments/{id}/steps` — check exit code.
- If base image pull failed, check network and registry access.
- Fix the Dockerfile or build command, then create a new deployment (plans are single-use).

**Escalation boundary**
- If the build fails with an unclear error, escalate to platform team with the build log.

### 5.2 No Dockerfile

**Detection signal**
- Deployment `FAILED` with `errorCode: "BUILD_FAILED"` and build error `BUILD_NO_DOCKERFILE`.
- Log line: `strategy is dockerfile but no Dockerfile was found in the repository`.

**Likely causes**
1. Repository has no `Dockerfile`, `docker/Dockerfile`, or `deploy/Dockerfile`.
2. Build strategy is `dockerfile` but repository uses a source preset.

**Safe operator action**
- READ: `GET /api/v1/deployments/{id}/logs?step=BUILD&level=error`.
- Add a Dockerfile to the repository, or change the build strategy via overrides.

**Escalation boundary**
- None — this is a repository configuration issue.

### 5.3 Source failed

**Detection signal**
- Deployment `FAILED` with `errorCode: "BUILD_FAILED"` and build error `BUILD_SOURCE_FAILED`.
- Log line: `source exceeds build limits` or `source archive is unusable` or `extract source archive`.

**Likely causes**
1. Source archive exceeds limits (200 MiB compressed, 1 GiB total, 50 000 files, 100 MiB per file).
2. Archive is malformed or contains unsafe paths.
3. GitHub archive download failed.

**Safe operator action**
- READ: `GET /api/v1/deployments/{id}/logs?step=BUILD&level=error`.
- If oversized, reduce repository size.
- If malformed, check repository integrity.

**Escalation boundary**
- If the repository is within limits but still fails, escalate.

### 5.4 Build interrupted

**Detection signal**
- Deployment `FAILED` with `errorCode: "BUILD_FAILED"` and build error `BUILD_INTERRUPTED`.
- Log line: `build interrupted`.

**Likely causes**
1. Build context cancelled (deployment cancelled during build).
2. Build timeout exceeded.
3. Engine shutdown during build.

**Safe operator action**
- READ: `GET /api/v1/deployments/{id}` — check if deployment was cancelled.
- If cancelled, no action needed — create a new deployment.
- If timed out, optimize the build (smaller context, faster base image).

**Escalation boundary**
- If builds are frequently interrupted without cancellation, escalate — may be an Engine stability issue.

### 5.5 Docker daemon down

**Detection signal**
- `GET /api/v1/deployments/{id}/steps` — step `BUILD` is `RUNNING` but no progress.
- `GET /api/v1/servers/{serverId}/health` — server status may be `offline` if agent is also affected.
- Build logs show Docker daemon connection errors.

**Likely causes**
1. Docker daemon stopped or crashed on the build host.
2. Docker daemon overloaded (too many concurrent builds).
3. Disk full on the build host.

**Safe operator action**
- READ: `sudo systemctl status docker` — check Docker daemon status.
- READ: `sudo journalctl -u docker --since "1 hour ago"` — check Docker logs.
- RESTART: `sudo systemctl restart docker` (RESTART) — only if daemon is down and no builds are running.
- READ: `df -h` — check disk space.

**Escalation boundary**
- If Docker daemon crashes repeatedly, escalate to infrastructure team — may be a resource issue.

### 5.6 Image pull failure

**Detection signal**
- Build logs show `docker pull` errors or `manifest unknown` or `unauthorized`.
- Build fails with `BUILD_FAILED` and exit code 1.

**Likely causes**
1. Base image does not exist or was deleted.
2. Registry authentication required but not configured.
3. Network issue pulling from registry.
4. Registry rate limit.

**Safe operator action**
- READ: `GET /api/v1/deployments/{id}/logs?step=BUILD&level=error`.
- READ: `docker pull <image>` manually on the build host to verify.
- If registry auth is needed, configure Docker credentials on the build host.

**Escalation boundary**
- If the image exists but cannot be pulled, escalate to infrastructure team.

### 5.7 Port conflicts

**Detection signal**
- Deployment `FAILED` with `errorCode: "RUNTIME_FAILED"` at step `CREATE_RUNTIME` or `START`.
- Log line: `docker create` or `docker start` error mentioning port already in use.

**Likely causes**
1. Another container is already using the host port.
2. Previous deployment's container was not cleaned up.
3. Host port range conflict.

**Safe operator action**
- READ: `docker ps -a` — check for conflicting containers.
- READ: `GET /api/v1/applications/{applicationID}/deployments?status=LIVE` — check for other live deployments on the same server.
- If a stale container exists, remove it: `docker rm -f <container>` (DESTRUCTIVE — confirm first).

**Escalation boundary**
- If port conflicts persist, escalate to infrastructure team — may need port range reconfiguration.

## 6. Agent offline / degraded

### 6.1 Agent offline (heartbeat stale)

**Detection signal**
- `GET /api/v1/servers/{serverId}` — status is `offline` (effective status).
- `lastSeenAt` is older than 5 minutes (`StaleAfter`).
- Deployment `FAILED` with `errorCode: "DEPLOYMENT_NOT_ELIGIBLE"` — executor pre-flight rejected because server is offline.

**Likely causes**
1. Agent process stopped or crashed.
2. Network connectivity loss between agent and Engine.
3. Agent host rebooted or is down.
4. Engine restarted and agent hasn't re-registered yet.

**Safe operator action**
- READ: `GET /api/v1/servers/{serverId}/health` — check `lastSeenAt` and `agentVersion`.
- READ: `GET /api/v1/agent/status?serverId={serverId}` — check agent registration status.
- RESTART: Restart the agent on the server (agent-specific command, e.g. `sudo systemctl restart axiom-agent` — RESTART).
- If agent host is down, escalate to infrastructure team.

**Escalation boundary**
- If the agent is running but heartbeats are not reaching the Engine, escalate to infrastructure team — may be a network/firewall issue.

### 6.2 Agent degraded

**Detection signal**
- `GET /api/v1/servers/{serverId}` — status is `degraded`.
- Deployment proceeds but with explicit acknowledgement in the Console.

**Likely causes**
1. Agent reports `DEGRADED` status in heartbeat (resource pressure: CPU, memory, disk).
2. Docker daemon on server is degraded.
3. Traefik on server is degraded.

**Safe operator action**
- READ: `GET /api/v1/servers/{serverId}/health` — check `capabilities` (docker, traefik) and `agentVersion`.
- READ: Check server-level monitoring (if available).
- If resources are constrained, reduce load on the server (fewer deployments).

**Escalation boundary**
- If the server is resource-constrained, escalate to infrastructure team for capacity expansion.

### 6.3 Agent credential expired

**Detection signal**
- Agent cannot authenticate to Engine.
- `POST /api/v1/agent/rotate` returns `401 UNAUTHORIZED` with `details.reason: "expired"`.
- Log line: `agent.rotate` audit with `reason=expired`.

**Likely causes**
1. Agent credential exceeded its 90-day TTL (`CredentialTTL`).
2. Agent was revoked.
3. Clock skew between agent and Engine.

**Safe operator action**
- READ: `GET /api/v1/agent/status?agentId={agentId}` — check `credentialVersion` and status.
- If credential expired, use `POST /api/v1/agent/rotate` to rotate (requires current credential).
- If credential is lost, re-register the agent via `POST /api/v1/agent/register` with a new bootstrap token.

**Escalation boundary**
- If the agent cannot rotate (credential lost), escalate to platform team — may need manual credential reset.

## 7. Docker failures

### 7.1 Daemon down

**Detection signal**
- `sudo systemctl status docker` — inactive.
- Build or runtime operations fail with Docker daemon connection errors.
- `GET /api/v1/servers/{serverId}/health` — server may be `offline` if agent cannot report.

**Likely causes**
1. Docker daemon crashed or was stopped.
2. Disk full (Docker cannot write layers).
3. Docker daemon overloaded.

**Safe operator action**
- READ: `sudo systemctl status docker`
- READ: `sudo journalctl -u docker --since "1 hour ago"`
- READ: `df -h` — check disk space
- RESTART: `sudo systemctl restart docker` (RESTART) — only when no builds/deployments are running.

**Escalation boundary**
- If Docker daemon crashes repeatedly, escalate to infrastructure team.

### 7.2 Image pull failure

**Detection signal**
- Build logs show `docker pull` errors.
- `BUILD_FAILED` with exit code 1.

**Likely causes**
1. Base image does not exist.
2. Registry authentication required.
3. Network issue.
4. Registry rate limit.

**Safe operator action**
- READ: `GET /api/v1/deployments/{id}/logs?step=BUILD&level=error`
- READ: `docker pull <image>` manually to verify
- Configure Docker registry credentials if needed.

**Escalation boundary**
- If the image exists but cannot be pulled, escalate.

### 7.3 Port conflicts

**Detection signal**
- `RUNTIME_FAILED` at `CREATE_RUNTIME` or `START`.
- Log line: `docker create` or `docker start` error mentioning port already in use.

**Likely causes**
1. Another container using the host port.
2. Stale container from previous deployment.
3. Host port range conflict.

**Safe operator action**
- READ: `docker ps -a` — check for conflicting containers
- READ: `GET /api/v1/applications/{applicationID}/deployments?status=LIVE` — check other live deployments
- DESTRUCTIVE: `docker rm -f <container>` — only with confirmation.

**Escalation boundary**
- If port conflicts persist, escalate.

## 8. Health verification failures

### 8.1 Health check failed

**Detection signal**
- Deployment `FAILED` with `errorCode: "HEALTH_CHECK_FAILED"`.
- `GET /api/v1/deployments/{id}/health` — `status: "UNHEALTHY"`, `http.statusCode` not in expected range.
- `GET /api/v1/deployments/{id}/logs?step=VERIFY&level=error` — probe failure details.

**Likely causes**
1. Application not serving on the expected port.
2. Health path returns wrong status code.
3. Application takes too long to start (exceeds health timeout).
4. Health check policy misconfigured (wrong expected status range).

**Safe operator action**
- READ: `GET /api/v1/deployments/{id}/health` — check `http.statusCode` and `reason`.
- READ: `GET /api/v1/deployments/{id}/logs?step=VERIFY&level=error` — check probe details.
- READ: `GET /api/v1/deployments/{id}/steps` — check which attempt failed.
- If application is slow to start, increase `healthCheck.timeoutSeconds` in the plan (requires new plan).
- If health path is wrong, fix the application or update the plan.

**Escalation boundary**
- If the application is serving correctly but health checks still fail, escalate — may be a network/routing issue.

### 8.2 Health check timeout

**Detection signal**
- Deployment stuck in `VERIFYING` for longer than expected.
- `GET /api/v1/deployments/{id}/health` — `status: "UNKNOWN"` (no probe data yet).

**Likely causes**
1. Application takes longer to start than the health timeout.
2. Health check policy has too few retries or too short interval.
3. Network issue preventing the probe from reaching the application.

**Safe operator action**
- READ: `GET /api/v1/deployments/{id}` — check current state.
- READ: `GET /api/v1/deployments/{id}/steps` — check VERIFY step status.
- If application is still starting, wait — the executor retries (default 3 attempts, 2s interval, 5s timeout per attempt).
- If the application never starts, check its logs (if available via agent).

**Escalation boundary**
- If the application is up but health probes time out, escalate — may be a routing/firewall issue.

## 9. SSE disconnects

### 9.1 Stream disconnects

**Detection signal**
- Client receives incomplete event stream.
- `GET /api/v1/deployments/{id}/events?after={seq}` shows gaps in `seq`.
- Log line: `sse: load deployment` with error (if store unavailable).

**Likely causes**
1. Client network interruption.
2. Engine restart (in-process bus is not durable; events are replayed from store on reconnect).
3. Slow subscriber missing live events (bus drops events for slow subscribers).
4. Proxy timeout (if Engine is behind a reverse proxy without proper SSE support).

**Safe operator action**
- READ: `GET /api/v1/deployments/{id}/events?after={lastSeq}` — fetch missing events.
- Reconnect with `Last-Event-ID` header set to the last received `seq`.
- If behind a proxy, ensure `X-Accel-Buffering: no` is set (Engine sets this header) and proxy timeout is > 15s (heartbeat interval).

**Escalation boundary**
- If events are missing from the store (not just the live stream), escalate — may be a persistence issue.

### 9.2 Invalid Last-Event-ID

**Detection signal**
- `GET /api/v1/deployments/{id}/events/stream` returns `400 INVALID_REQUEST` with message "Last-Event-ID must be a non-negative integer".

**Likely causes**
1. Client sent a malformed `Last-Event-ID` header.
2. Client sent a negative or non-integer `lastEventId` query parameter.

**Safe operator action**
- READ: Check the `Last-Event-ID` header or `lastEventId` query parameter format.
- Reconnect without `Last-Event-ID` to start from the beginning, or with a valid integer.

**Escalation boundary**
- None — this is a client-side issue.

## 10. PostgreSQL failures

### 10.1 Database unavailable

**Detection signal**
- `GET /ready` returns `503` with `status: "not_ready"`, `reason: "database_unavailable"`.
- Engine fails to start with log line `database is required but DATABASE_URL is not configured` or `database migrations failed`.
- API endpoints return `503 SERVICE_UNAVAILABLE` ("a required dependency is unavailable").

**Likely causes**
1. PostgreSQL is down or unreachable.
2. `DATABASE_URL` is misconfigured.
3. Database migrations failed at startup.
4. Connection pool exhausted.

**Safe operator action**
- READ: `systemctl status postgresql` — check PostgreSQL status.
- READ: `sudo journalctl -u postgresql --since "1 hour ago"` — check PostgreSQL logs.
- READ: Check Engine startup logs for `database migrations failed` or `ping database`.
- RESTART: `sudo systemctl restart postgresql` (RESTART) — if PostgreSQL is down.
- If migrations failed, check the specific migration error in Engine logs. Do not manually edit the database — escalate.

**Escalation boundary**
- If migrations fail, escalate to platform team — manual database fixes may corrupt state.
- If PostgreSQL is down and cannot be restarted, escalate to infrastructure team immediately.

### 10.2 Connection loss (runtime)

**Detection signal**
- API requests fail with `503 SERVICE_UNAVAILABLE` or `500 INTERNAL_ERROR`.
- Log lines showing database connection errors (e.g. `connection refused`, `connection reset`).
- `GET /ready` returns `503` with `reason: "database_unavailable"`.

**Likely causes**
1. PostgreSQL restarted or became unreachable.
2. Network issue between Engine and PostgreSQL.
3. Connection pool exhausted (too many concurrent queries).
4. PostgreSQL max connections reached.

**Safe operator action**
- READ: `GET /ready` — check if database is reachable.
- READ: Check Engine logs for database connection errors.
- RESTART: Restart the Engine (RESTART) — the connection pool will be recreated.
- If PostgreSQL is down, restart it first (RESTART).

**Escalation boundary**
- If the database is frequently unreachable, escalate to infrastructure team — may be a network or capacity issue.

### 10.3 Migration failure at startup

**Detection signal**
- Engine fails to start with log line `database migrations failed: apply migration NNN_description.sql: ...`.
- `GET /ready` returns `503` (if Engine is running but migrations failed on a previous start).

**Likely causes**
1. A new migration has a syntax error or conflicts with existing schema.
2. Database was manually modified, breaking migration assumptions.
3. Concurrent Engine instances trying to migrate simultaneously (mitigated by `schema_migrations` table, but edge cases exist).

**Safe operator action**
- READ: Check Engine startup logs for the specific migration error.
- READ: `psql $DATABASE_URL -c "SELECT * FROM schema_migrations ORDER BY version DESC LIMIT 5"` — check which migrations were applied.
- Do NOT manually edit the database or migration files.

**Escalation boundary**
- Always escalate migration failures to the platform team — manual fixes may corrupt state.

## 11. Traefik / TLS failures

### 11.1 Routing failed

**Detection signal**
- `GET /api/v1/applications/{applicationId}/domains` — domain `routingStatus` is not `active`.
- Application URL returns 502 Bad Gateway or 404.
- `GET /api/v1/deployments/{id}/health` — health check fails with connection refused (Traefik not routing).

**Likely causes**
1. Traefik on the server is down or misconfigured.
2. Agent's network adapter failed to configure Traefik.
3. Domain DNS does not point to the server.
4. Traefik dynamic configuration not reloaded.

**Safe operator action**
- READ: `GET /api/v1/applications/{applicationId}/domains` — check `routingStatus` and `dnsStatus`.
- READ: `POST /api/v1/domains/{domainId}/check` — verify DNS resolution.
- READ: `GET /api/v1/servers/{serverId}/health` — check server and agent status.
- If Traefik is down on the server, restart it (agent-specific or infrastructure team).
- If DNS is misconfigured, fix the DNS record to point to the server address.

**Escalation boundary**
- If Traefik is running but not routing, escalate to platform team — may be an agent network adapter issue.

### 11.2 Certificate issuing failed

**Detection signal**
- `GET /api/v1/applications/{applicationId}/domains` — domain `tlsStatus` is not `valid`.
- Application URL returns TLS certificate error.
- Domain `dnsStatus` is `ok` but `tlsStatus` is `pending` or `error`.

**Likely causes**
1. Let's Encrypt ACME challenge failed (DNS not propagated, or port 80/443 blocked).
2. Traefik ACME configuration error.
3. Rate limit on Let's Encrypt (too many certificates for the same domain).
4. Domain DNS does not resolve to the server (ACME HTTP-01 challenge fails).

**Safe operator action**
- READ: `GET /api/v1/applications/{applicationId}/domains` — check `tlsStatus`.
- READ: `POST /api/v1/domains/{domainId}/check` — verify DNS.
- If DNS is not propagated, wait and retry.
- If ACME challenge fails, check Traefik logs on the server (agent or infrastructure team).

**Escalation boundary**
- If certificates cannot be issued after DNS is confirmed, escalate to platform team — may be a Traefik ACME configuration issue.

## 12. Severity table

| Severity | Definition | Examples | Response time |
|---|---|---|---|
| **P0** | Complete service outage; no deployments possible | PostgreSQL down; Engine won't start; Docker daemon down on all servers | Immediate |
| **P1** | Major functionality broken; deployments failing | Build failures; health check failures; agent offline on a server | < 1 hour |
| **P2** | Degraded functionality; workarounds available | SSE disconnects (reconnect works); single domain DNS mismatch | < 4 hours |
| **P3** | Minor issues; no immediate impact | Single analysis failure (retry works); rate limiting | < 1 day |

## 13. Escalation contacts

| Issue | Escalate to |
|---|---|
| PostgreSQL down / migrations failed | Infrastructure team → Platform team |
| Docker daemon down | Infrastructure team |
| Agent offline / degraded | Infrastructure team → Platform team |
| Traefik / TLS failures | Platform team |
| GitHub OAuth failures | Platform team |
| Build failures (unclear) | Platform team |
| Health check failures (app-side) | Application owner |
| Security incidents | Security team (see `docs/security/threat-model.md`) |

## 14. Related documentation

- `docs/architecture/api-contract.md` — API contract, error classes (§18), HTTP status conventions (§19)
- `docs/architecture/deployment-policy.md` — Deployment security boundary, gates, policy rules
- `docs/architecture/agent-protocol.md` — Agent ↔ Engine protocol, operation types, error codes
- `docs/adr/0005-traefik-reverse-proxy.md` — Traefik as reverse proxy with ACME
- `services/engine/migrations/README.md` — Database migration discipline and state ownership
- `docs/security/threat-model.md` — Security threat model (DRAFT)

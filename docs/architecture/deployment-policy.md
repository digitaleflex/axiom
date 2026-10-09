# Deployment Security Boundary v1

> Issue #67. Enforcement points between user intent and infrastructure. Threat model: #129 (owns `docs/security/`).

## 1. Rule

**The Engine dispatches an operation only when the deployment policy authorizes it.** Policy is deterministic code (`services/engine/internal/policy`), independent of model output and of UI confirmation: even a crafted API request cannot execute an unauthorized plan.

## 2. Gates Before Dispatch

| Order | Gate | Decides | Denial |
|---|---|---|---|
| 1 | Authentication (#125, interim bearer token) | who calls | `401 UNAUTHORIZED` |
| 2 | Ownership (#127, application-scoped in API) | caller's resource | `404` (no existence leak) |
| 3 | Plan validation (#97) | well-formed plan | `422` field errors |
| 4 | Server eligibility (#63, re-checked pre-flight) | live target | `DEPLOYMENT_NOT_ELIGIBLE` |
| 5 | **Policy evaluation (#67)** | authorized plan | `POLICY_DENIED` |
| 6 | Step execution (#100) | per-step success | step code (`BUILD_FAILED`, …) |

## 3. Policy Rules (V0.1)

1. Plan status is `READY`.
2. Fingerprint matches recomputed contents (tamper-evident; empty only in tests).
3. Strategy is a known V0.1 preset; environment is production/staging/preview.
4. Plan targets the server being deployed to (binding).
5. Plan passes full validation (steps, domain, health, rollback, capabilities).
6. Serialized plan contains no secret values (patterns for passwords, tokens, bearer credentials, private keys, provider key prefixes). Plans carry configuration **names**, never values.

## 4. Secret Handling

| Secret | At rest | In transit / logs | Injected into |
|---|---|---|---|
| GitHub tokens | AES-256-GCM (`AXIOM_SECRET_KEY`) | never returned, never logged (#91) | GitHub API calls only |
| Application config values | #126 (secret store) | write-only in UI; `[REDACTED]` in logs (#66); redacted in 5xx server logs (#67) | build workspace and runtime only, by reference |
| Agent credentials | #77 | never in API responses | Engine-signed operations |

## 5. Isolation

- Analysis never executes repository code (#93, #94).
- Builds run in ephemeral 0700 workspaces with extracted (not executed) sources; builder child processes always get an explicit minimal environment — a fixed allowlist (PATH, HOME, TMPDIR, locale, Docker daemon connectivity, proxy settings) plus explicitly caller-requested entries — and never inherit the Engine process environment, so `AXIOM_SECRET_KEY`, `DATABASE_URL`, `AXIOM_API_TOKEN` and other Engine secrets cannot reach `docker build` (#98).
- Runtime Agent accepts only typed operations, re-validates authorization and owns no orchestration (#80, #89).
- The public API exposes no shell, no Docker/Traefik access, no database (#117 §22).

## 6. Audit

Privileged mutations emit audit events with actor, action, target, result, request ID and timestamp: `deployment.create/cancel`, `plan.create`, `domain.add/remove/set_primary`, `server.register/remove`, `github.disconnect`, `appconfig.set/delete`. Success and failure are both recorded; secrets are redacted before persistence (#128, `audit_events` table). The trail is readable via `GET /api/v1/audit` (owner-scoped).

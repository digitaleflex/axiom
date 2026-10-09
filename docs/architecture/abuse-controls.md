# Abuse Controls — coherence check (#129 M7.5)

> Cross-reference: `docs/security/threat-model.md` section 14 (new). Verified against commit `d69bdea` and the mounted working tree after `6017b35` / `fix-4` / `fix-5`.

## Mounted-code checklist (no .go edited)

| Control | Source (path:line) | State in doc |
|---|---|---|
| Agent listener loopback | `services/agent/internal/config/config.go:35`, `198-222` | EN PLACE / masked |
| Fail-closed (`refuseInbound`) | `bootstrap.go:212`, `listener.go:47-52`, `112-119` | EN PLACE |
| TLS optional (#88) | `listener.go` — no impl; `transport.go:71-91` outbound https only | ABSENT / masked |
| Per-agent HMAC (ADR-0008) | `docs/adr/0008-agent-engine-communication.md`; `agentclient/client.go:527-556`; `protocol.go:211-235` | EN PLACE |
| `applicationId` separate from `deploymentId` | `protocol.go:224-229`; `client.go:546-559`; `ownership/ownership.go:139-149` | EN PLACE (E3 resolved) |
| Webhook spoofing (T31) | Grep `webhook` = 0 across `services/`, `schemas/`; `api/api.go:137-142` | ABSENT by design |
| Build sandbox / resource limits | `builder.go:74-81` (none); `workspace.go:78` (isolated); `builder.go:59-63` (timeout) | PARTIEL / G1 accepted |
| Rate limit (T20) | `transport.go:117-159` (non-cabled); no middleware in `api/` | ABSENT / NON CABLE |

## Menace 31 (T31) — webhook spoofing

Documented in `threat-model.md` §8 (line 697+). The model notes correctly that **there are no webhooks** (0 occurrences). The control required when webhooks are eventually introduced (`X-Hub-Signature-256` + replay-bound) is explicitly recorded, preventing an unauthenticated write path into the deployment pipeline.

## What changed in this commit

- `docs/security/threat-model.md`: added section 14 (Abuse Controls) with 5 subsections (loopback/TLS, HMAC/ADR-0008, webhook/T31, build-resource abuse, residual-risk table).
- `docs/architecture/abuse-controls.md`: new synthesis file (this file).
- Zero `.go` files touched; `internal/protocol/` and `services/agent/` untouched (respecting `6017b35` / `d9b0e6e` boundaries).

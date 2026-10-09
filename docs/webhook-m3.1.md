# M3.1 Webhook minimal (#57) — architecture note

## Added
- `services/engine/internal/webhook/handler.go` (1 file) — endpoint `POST /api/v1/webhook/github`.
- Route wired in `services/engine/internal/api/api.go`.

## Branching on existing
- `repos.Service` / `ghauth.Service` are composed in `bootstrap.go:232-237`; webhook uses `RepositoryDiscovery` interface available on `api.Deps`.
- Signature validation uses `crypto/hmac` + `crypto/sha256` (`X-Hub-Signature-256`) per `docs/security/threat-model.md` §14.3 (T31).

## Blocked / deferred (not forced)
- Full pipeline trigger needs `repo→connection→user→deployment plan`; `internal/protocol/` (agent) carries no `ApplicationID` (`bridge.go:76` stamps `deploymentID`), so ownership cannot distinguish app from deployment (#145).
- Agent credential direction `Engine→Agent` (#77) is missing (`refuseInbound{}` bootstrap:211); building without agent auth is unsafe.
- Memory / Docker load is high (7.8 GB RAM, 55 containers, swap full 4 GB); no new containers created.
- Endpoint returns 202 with deferred note; operational trigger lands after #145 + #77.

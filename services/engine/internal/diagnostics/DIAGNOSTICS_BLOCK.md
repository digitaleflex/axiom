# #102 — Deployment Diagnostics API (M8.2)

Status: ENDPOINT PRESENT — blocage côté agent non résolu.

## Endpoint exposé (services/engine/internal/api/)

- `GET /api/v1/deployments/{deploymentID}/diagnostics` — rapport complet (`deploymentDiagnostic`, diagnostics.go:32)
- `GET /api/v1/deployments/{deploymentID}/diagnostics/logs` — page paginée (`deploymentDiagnosticLogs`, diagnostics.go:60)

Câblé dans `api.go:234-235`. Service construit dans `bootstrap` (`internal/diagnostics.New`). Package `internal/diagnostics/` monté (`diagnostics.go`, `analyze.go`, `logs.go`, `redact.go`).

## Blocage restant

Le contexte d'audit (exp-2 / mémoire projet) signale : `logs.Fetcher agent sans endpoint`. L'agent (`agent` module — HORS PÉRIMÈTRE ici) n'expose pas d'endpoint recevant le résultat du diagnostic engine. Le diagnostic est donc lisible side-car engine mais non consommé par l'agent pour alimenter un fetcher de logs. Aucun autre module (`protocol`, `cloud`) n'est modifié.

## Vérifications

- Build/vet OK (`GOTOOLCHAIN=go1.25.14`)
- Un seul `.go` modifié : NON NÉCESSAIRE (déjà présent)
- Aucun lancement docker (RAM 7,8 GB / 55 containers / swap plein — évité)
- Aucune modification dans `agent/`, `protocol/`, `cloud/`

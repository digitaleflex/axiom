# Axiom CLI (M9.1 — #105)

Squelette minimal du CLI Go du dépôt Axiom.

## Emplacement

- `services/engine/cmd/cli/main.go` (unique fichier Go)
- `docs/cli.md` (cette doc)

## Commandes

| Commande     | Description                                           |
|--------------|-------------------------------------------------------|
| `cli status` | Module Go + version (git describe)                    |
| `cli build`  | `go build ./...` dans `services/engine/`              |
| `cli containers` | `docker compose -f docker-compose.dev.yml ps` (si dispo) |

## Construction

```bash
export GOTOOLCHAIN=go1.25.14
cd services/engine/cmd/cli
go build ./...
```

## Restrictions

- `flag` standard (pas de `cobra` — absent du `go.sum` du module engine).
- Ne lance aucun conteneur lourd (docker-compose uniquement en lecture `ps`).
- Aucun autre module touché (pas d'agent, pas de internal/protocol, pas de docs/security, pas d'apps/cloud).

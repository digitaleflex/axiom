# Deployment Expert Tool Permissions

> Issue #37. Machine-readable catalog: `schemas/expert-tools.yaml`. Per-expert grants: `tools` in `schemas/experts/<id>.yaml`.

## 1. Default deny

An expert may invoke only tools listed in its contract; every tool must exist in the catalog (checked by `tests/contracts/validate_schemas.py`). There is no wildcard and no implicit tool.

## 2. Catalog summary

| Tool | Category | Sensitivity | Destructive | Approval | Audited |
|---|---|---|---|---|---|
| `repository.read` | repository | normal | no | — | no |
| `github.api.read` | repository | controlled | no | — | yes |
| `profile.read` | artifact | normal | no | — | no |
| `preset.resolve` | artifact | normal | no | — | no |
| `server.read` | infrastructure | normal | no | — | no |
| `plan.validate` | artifact | normal | no | — | no |
| `build.workspace` | execution | controlled | no | — | yes |
| `image.build` | execution | controlled | no | — | yes |
| `agent.dispatch` | infrastructure | privileged | conditional | destructive operations | yes |
| `secret.reference` | secrets | privileged | no | — | yes |
| `policy.evaluate` | security | normal | no | — | yes |

## 3. Concern mapping (#37 scope)

| Concern | Rule |
|---|---|
| Repository access | read-only, pinned commit (`repository.read`, `github.api.read`) |
| Filesystem access | only the ephemeral build workspace (`build.workspace`) |
| Command execution | only preset-validated build commands inside `image.build`; no shell elsewhere |
| Docker/runtime operations | only typed operations via `agent.dispatch` |
| Infrastructure APIs | Engine-internal server records (`server.read`); no provider APIs in V0.1 |
| Secrets | references only (`secret.reference`); values never returned |
| Network | per contract `runtime_limits.network_access`: `none`, `github`, `agent`, `registry` |
| Human approval | required for destructive `agent.dispatch` operations (STOP/REMOVE of a LIVE runtime, domain removal) and policy exceptions |

## 4. Authorization chain

1. Contract grants the tool.
2. Engine authorizes the specific call (user ownership #127, policy).
3. Destructive calls require recorded human approval.
4. Runtime Agent re-validates operation type and server binding (#80, #89).
5. Audited calls emit an audit event (#128).

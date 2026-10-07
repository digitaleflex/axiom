# Deployment Expert Configuration Format

> Issue #35. Schema: `schemas/expert-config.schema.json`. Examples: `schemas/examples/expert-config/`.

## 1. Two layers

| Layer | File | Contains | Changes when |
|---|---|---|---|
| Contract | `schemas/experts/<id>.yaml` | identity, capabilities, inputs/outputs, tools, forbidden responsibilities, dependencies, quality gates, escalation, context, runtime limits | responsibility changes (reviewed, versioned) |
| Configuration | `expert-config` document (Engine config) | binding to an implementation (code adapter or provider/model), enabled flag, tightening overrides | deployment/operations choices |

Contracts are provider-independent. Provider/model settings exist only in configuration.

## 2. Fields

| Field | Rule |
|---|---|
| `config_version` | `"1"` |
| `expert_id` | must reference an existing contract |
| `contract_version` | must equal the contract's version |
| `enabled` | disabled experts cannot be invoked; a stage with a disabled required expert fails closed |
| `implementation.kind: code` | `adapter` identifier registered in the Engine |
| `implementation.kind: model-assisted` | `provider`, `model`, `credentials_ref` (`secret://…` reference only) |
| `overrides` | may **only tighten** limits (`max_execution_seconds`, `max_retries`) or add `human_review` |

## 3. Registration

At Engine startup:

1. Load all contracts; validate against `expert-contract.schema.json`.
2. Load configurations; validate against `expert-config.schema.json`.
3. Cross-check: contract exists, versions match, tools in catalog, dependencies exist and acyclic, overrides tighten only.
4. Register adapters; any failure aborts startup (deterministic, fail-closed).

The same files always produce the same registry (no environment-dependent defaults).

## 4. Secrets

No secret value in contracts or configurations. Credentials are `secret://` references resolved by the secret store (#126) inside the invocation boundary. Unknown fields (e.g. `api_key`) are rejected.

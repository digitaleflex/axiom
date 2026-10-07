# Deployment Expert Framework

> Epic #15. Bounded experts used **at runtime by the Deployment Engine** to turn a repository into a LIVE deployment.
>
> Not to be confused with the engineering/coding-agent roles in [`../roles.md`](../roles.md), which govern how Axiom itself is developed.

| Document | Issue |
|---|---|
| [Roles & responsibility matrix](roles.md) | #34 |
| [Configuration format](configuration.md) | #35 |
| [Context boundaries](context.md) | #36 |
| [Tool permissions](tools.md) | #37 |
| [Quality gates & evaluation](quality-gates.md) | #38 |
| [Reference specifications](../specs/deployment/README.md) | #39 |

Machine-readable sources: `schemas/expert-contract.schema.json`, `schemas/experts/*.yaml`, `schemas/expert-config.schema.json`, `schemas/expert-tools.yaml`, `schemas/artifact.schema.json`. Validation: `python3 tests/contracts/validate_schemas.py`.

## Invocation model

```text
Deployment Engine
  ├─ selects stage → expert contract
  ├─ builds minimal context (context.md)
  ├─ invokes Expert interface  ──► adapter (code | model-assisted)
  ├─ receives artifact (draft)
  ├─ runs quality gates (quality-gates.md)
  └─ valid → next stage | rejected → revision/escalation
```

The Engine depends on an interface, never on a model provider:

```go
type Expert interface {
    ID() string
    Run(ctx context.Context, in ExpertInput) (Artifact, error)
}
```

V0.1 experts are deterministic Go code (`implementation.kind: code`).

# Axiom Expert Capabilities

## Purpose

Capabilities describe what an expert is authorized and technically able to perform. A capability is not a role: roles define responsibility, while capabilities define bounded actions.

## Naming

Use stable identifiers in `domain.action` form, for example:

- `architecture.design`
- `repository.read`
- `repository.write`
- `database.schema_design`
- `security.threat_model`
- `tests.execute`
- `infrastructure.plan`
- `deployment.plan`
- `observability.define`
- `documentation.write`

## Rules

1. Every capability belongs to a declared expert role.
2. A capability must have an explicit permission boundary.
3. Capabilities do not grant access to secrets by themselves.
4. Destructive capabilities require explicit approval when configured as sensitive operations.
5. The orchestrator may schedule only capabilities declared by the selected expert contract.
6. Runtime execution must validate authorization again; orchestration intent is not sufficient authority.
7. Capability identifiers are versioned and must remain backward-compatible within a contract version.

## Initial capability catalogue

| Capability | Primary role | Sensitivity |
|---|---|---|
| `architecture.design` | Architect | normal |
| `repository.read` | all technical experts | normal |
| `repository.write` | Backend/Frontend/Documentation | controlled |
| `database.schema_design` | Database | normal |
| `database.migration_prepare` | Database | controlled |
| `security.threat_model` | Security | normal |
| `security.review` | Security | sensitive |
| `tests.execute` | QA | controlled |
| `infrastructure.plan` | Infrastructure | controlled |
| `deployment.plan` | DevOps | controlled |
| `deployment.execute` | DevOps/Runtime | approval-required |
| `observability.define` | Observability | normal |
| `documentation.write` | Documentation | controlled |

This catalogue is the initial v0.1 baseline. New capabilities must be documented, assigned to an owner, and covered by permission and quality-gate tests.

# Axiom Expert Roles

Axiom uses specialized experts with explicit authority boundaries. The goal is specialization without duplicated ownership or uncontrolled autonomy.

| Expert | Primary responsibility | Must not own |
|---|---|---|
| Business Domain | Requirements, domain rules, workflows | Infrastructure implementation |
| Architect | System architecture, boundaries, ADRs | Business policy |
| Backend | APIs, services, application logic | Infrastructure policy |
| Frontend | UI, client behavior, accessibility | Server infrastructure |
| Database | Schema, migrations, indexes, data integrity | Product requirements |
| Security | Security controls, threat model, authorization constraints | Business priorities |
| QA | Validation strategy, tests, regression, acceptance evidence | Production authority |
| DevOps | CI/CD, delivery workflow, release automation | Domain rules |
| Infrastructure | Runtime infrastructure, networking, deployment environment | Business logic |
| Observability | Logs, metrics, traces, health, alerting | Product semantics |
| Documentation | Technical and operational documentation | Architectural authority |

## Coordination model

```text
Business Domain
      ↓
Business Expert
      ↓
Architect
      ↓
Backend / Frontend / Database
      ↓
Security
      ↓
QA
      ↓
DevOps / Infrastructure
      ↓
Runtime Agent
```

The Orchestrator coordinates this workflow. It does not replace the responsibility of an expert.

## Ownership rules

- Every decision has an accountable role.
- Cross-domain decisions require explicit handoff or orchestration.
- Business requirements are authoritative for domain behavior.
- Security constraints remain enforceable regardless of the requesting role.
- Infrastructure experts may reject technically unsafe deployment requirements.
- Experts cannot silently expand their authority through tool access.

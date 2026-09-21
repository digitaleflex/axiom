# Platform / Domain Boundaries

## Axiom owns

Architecture, technical contracts, expert coordination, infrastructure integration, security constraints, observability and runtime operations.

## Domain project owns

Business rules, product requirements, domain workflows and project-specific UX.

## Rules

1. Axiom must not encode business rules for a specific project.
2. Infrastructure components must not decide business behavior.
3. Business experts must not silently override technical security or runtime constraints.
4. The orchestrator coordinates decisions but does not become the owner of every domain.
5. The runtime agent performs authorized operational work and does not contain business logic.

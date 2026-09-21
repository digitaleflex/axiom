# Expert Quality Gates

Quality gates prevent an expert from passing incomplete or unsafe work to the next stage.

## Gate model

```text
WORK → VALIDATE → PASS
              └→ REVISION_REQUIRED
```

A gate should define:

- required inputs;
- required artifact types;
- schema/contract validation;
- role-specific acceptance criteria;
- automated checks where possible;
- uncertainty or missing-information reporting;
- approval requirements;
- rejection/revision behavior.

## Independence

An expert must not self-approve a critical artifact solely because it produced that artifact. Critical outputs require an independent validation step, another expert, an automated gate, or explicit human approval according to policy.

## Examples

### Architecture

- boundaries are explicit;
- dependencies have defined direction;
- domain/platform separation is preserved;
- required ADRs exist;
- unresolved architectural risks are reported.

### Backend

- contract validation passes;
- tests cover critical behavior;
- error handling is explicit;
- security constraints are preserved;
- implementation matches the approved architecture.

### Security

- threat model considered;
- authorization boundaries validated;
- secrets are not embedded;
- critical findings are classified;
- exceptions are explicit and approved.

### QA

- acceptance criteria are mapped to tests;
- critical paths are validated;
- regression evidence is recorded;
- failures are traceable to the relevant task/artifact.

### Infrastructure / DevOps

- deployment plan is reproducible;
- health/readiness checks exist;
- rollback or recovery path is defined where applicable;
- production-sensitive operations have required approval.

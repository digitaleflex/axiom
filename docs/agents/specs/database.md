# Database Expert

## Responsibility

Design and validate persistence structures, data integrity, migrations, indexes, transactions, and database performance within the approved architecture.

## Inputs

- Business Specification;
- Architecture Specification;
- domain entities and invariants;
- API/data contracts;
- operational constraints.

## Outputs

- database schema;
- migration plan;
- indexes and constraints;
- transaction rules;
- backup/recovery requirements;
- database tests and validation evidence.

## Forbidden responsibilities

- redefining domain semantics without business handoff;
- granting application permissions outside approved security policy;
- production destructive operations without authorization.

## Quality gate

Schema integrity, migrations, constraints, critical queries and recovery assumptions must be explicit and validated.

# Architect Expert

## Responsibility

Transform validated business requirements into a coherent technical architecture while respecting Axiom platform constraints.

## Inputs

- Business Specification;
- project manifest;
- applicable ADRs;
- platform contracts;
- security constraints.

## Outputs

- Architecture Specification;
- component/dependency model;
- interfaces and contracts;
- technical ADRs when needed;
- implementation plan;
- architectural risks and open decisions.

## Forbidden responsibilities

- redefining business requirements without explicit handoff;
- bypassing security constraints;
- performing production runtime operations.

## Quality gate

Architecture must preserve the validated domain requirements, define component responsibilities and dependencies, identify important trade-offs, and expose unresolved decisions rather than silently assuming them.

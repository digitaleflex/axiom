# Observability Expert

## Responsibility

Define and validate operational visibility through structured logs, metrics, traces, health signals, correlation and alerting.

## Inputs

- Architecture Specification;
- service interfaces;
- runtime requirements;
- operational objectives;
- security constraints.

## Outputs

- telemetry specification;
- dashboards;
- alert rules;
- health/readiness definitions;
- correlation strategy;
- operational diagnostics.

## Forbidden responsibilities

- changing business rules;
- deploying arbitrary production changes without authorization;
- treating telemetry as a substitute for security controls.

## Quality gate

Critical operations must expose sufficient telemetry to correlate request, task/job, execution and outcome where technically applicable.

# Axiom Repository Instructions

## Project

Axiom is a deployment and application hosting platform that transforms a GitHub repository into a deployed application.

Core pipeline:

GitHub → Repository Analysis → Stack Detection → Application Profile → Deployment Plan → Build → Runtime → Networking/Domain/TLS → Health Check → LIVE.

## Agent operating rules

- Read the relevant architecture and issue specification before editing code.
- Treat the GitHub issue as the source of task scope.
- Respect the declared OWNED PATHS and FORBIDDEN PATHS.
- Do not modify unrelated files to make a task appear complete.
- Do not invent APIs, schemas, runtime behavior, or architecture when a contract exists.
- If a required dependency is not ready, stop and report BLOCKED.
- Prefer small, reviewable changes.
- Preserve existing interfaces unless the issue explicitly changes them.
- Never claim tests passed unless they were actually executed.
- Never expose credentials, tokens, secrets, or sensitive infrastructure information in logs or commits.
- Deployment/runtime code must use bounded interfaces rather than arbitrary shell execution.
- Security-sensitive operations must fail closed.
- Record important assumptions and limitations in the handoff.

## Documentation hierarchy

Before implementation, consult as applicable:

1. `docs/product/`
2. `docs/architecture/`
3. `docs/architecture/agent-task-specification.md`
4. `docs/architecture/agent-work-map.md`
5. `docs/architecture/agent-workflow.md`
6. `.github/axiom/agent-readiness.yml`
7. the assigned issue

## Delivery

The expected delivery artifact is a pull request linked to the issue.

The PR must contain:

- implementation summary
- tests actually run
- security considerations
- ownership confirmation
- known limitations
- integration notes

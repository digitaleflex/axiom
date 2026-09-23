# Axiom Progressive Disclosure & Technical Density v1.0

> UX contract for Axiom V0.1.
>
> Governing principle: **Application context first. Infrastructure detail on demand.**

## 1. Purpose

Progressive Disclosure Infrastructure is an explicit interaction rule.

Axiom must expose enough information to make the current action and system state understandable without requiring infrastructure expertise. Deeper operational and technical information remains accessible through deliberate drill-down.

This document complements the canonical Design DNA in `docs/design/design-dna/README.md`.

---

## 2. Disclosure Levels

| Level | Name | Default purpose | Typical content |
|---|---|---|---|
| L1 | Essential | Understand current state and act | application, environment, status, URL, primary action |
| L2 | Operational | Operate and diagnose | deployments, health, resources, domains, recent events, diagnostics |
| L3 | Technical | Understand implementation | runtime, image, port, proxy, build, network, health probes, deployment steps |
| L4 | Expert | Deep infrastructure diagnostics | container IDs, digests, internal routing, telemetry, low-level diagnostics |

### Global rule

- L1/L2 are the default experience.
- L3/L4 are available through deliberate expansion, tabs, drawers or dedicated technical views.
- L3/L4 must never visually overpower the current application state or primary action.
- Every technical value must have a label and contextual meaning.
- Technical content must be relevant to the actual runtime and deployment.

---

## 3. Context Preservation

Every drill-down must preserve the user's current context:

1. Workspace/account
2. Application
3. Environment
4. Deployment where applicable
5. Server where applicable

A technical drawer or detail page must make the parent context obvious.

### Example

A container ID must never appear as an unexplained identifier. It should be presented in context:

`Application → Production → Deployment #42 → Runtime → Container`

---

## 4. Canonical V0.1 Screen Matrix

| Screen | Default level | Visible by default | Drill-down |
|---|---|---|---|
| Dashboard | L1/L2 | application health, environment, deployment status, primary actions | recent events, resource details |
| GitHub Connection | L1 | connection state, account, permissions, primary action | scopes, technical connection details |
| Repository List | L1/L2 | repository, branch/ref, status, last activity | repository metadata |
| Repository Detail | L1/L2 | repository, ref, detected state, action | files/metadata/analysis details |
| Repository Analysis | L2 | detection result, confidence, evidence summary, next action | evidence and technical detection details |
| Application Profile | L2 | framework, package manager, commands, port, runtime strategy | raw evidence, overrides, technical values |
| Server Selection | L1/L2 | eligible servers, status, capacity, compatibility | capabilities, resources, agent details |
| Deployment Configuration | L1/L2 | target, environment, domain, primary configuration | runtime/build/network options |
| Deployment Plan | L2 | ordered deployment steps, strategy, target, risk/warnings | build/runtime/network/TLS details |
| Deployment Progress | L1/L2 | current step, overall state, progress, primary controls | live logs, step diagnostics |
| Deployment Success | L1 | LIVE state, URL, environment, next action | deployment/runtime details |
| Deployment Failure | L1/L2 | failure state, cause summary, recovery action | logs, failed step, diagnostics |
| Application Overview | L1/L2 | status, URL, environment, current deployment, health | runtime/resources/events |
| Application Deployments | L2 | deployment history, status, refs, timestamps | deployment details and logs |
| Application Logs | L2/L3 | live logs, filters, severity, context | raw identifiers, stack traces, metadata |
| Application Metrics | L2 | CPU, memory, disk, network, latency/health where available | telemetry detail |
| Application Domains | L1/L2 | domains, TLS state, primary domain, actions | routing/TLS technical details |
| Server Overview | L1/L2 | server status, capacity, workloads, agent health | capabilities and runtime detail |
| Server Details | L2/L3 | resources, agent, workloads, health | low-level runtime/network diagnostics |
| Settings | L1/L2 | user/workspace/application settings | advanced technical configuration |

---

## 5. Primary Action Rule

Every screen must have one visually dominant primary action or one clearly dominant state.

Examples:

- GitHub Connection → Connect GitHub
- Repository Detail → Analyze
- Application Profile → Continue
- Server Selection → Select server
- Deployment Configuration → Review plan
- Deployment Plan → Deploy
- Deployment Progress → Observe / Cancel when permitted
- Deployment Success → Open application
- Deployment Failure → Retry / Inspect failure
- Application Overview → Deploy

Technical information must never compete with this action.

---

## 6. Technical Density Rules

### Tables

Tables must optimize:

- scanability
- alignment
- comparison
- status recognition
- copyability where relevant

Avoid:

- decorative columns
- unexplained abbreviations
- excessive horizontal density
- technical fields that do not support the current task

### Logs

Logs may be high-density.

Required behavior where applicable:

- monospace
- stable line-height
- timestamp
- severity
- searchable/filterable
- copyable technical values
- foldable stack traces
- clear deployment/step context

### Technical identifiers

Use technical typography for:

- SHA
- deployment ID
- image reference
- image digest
- container ID
- port
- timestamp
- path
- exit code
- command

Long values should be truncated visually but remain fully accessible/copyable.

---

## 7. Runtime-Aware Disclosure

Axiom must not display irrelevant infrastructure concepts.

Examples:

- Node/Next.js deployment → expose Node runtime, package manager, build/start commands, port, image and health details.
- Go deployment → expose Go runtime/build details relevant to the application.
- Docker deployment → expose image/runtime/container details relevant to the selected strategy.
- Compose deployment → expose service-level details relevant to the selected services.

Do not surface Kubernetes, eBPF, service-mesh or unrelated platform concepts merely because they are technically possible.

---

## 8. Deployment-Specific Disclosure

The deployment plan is a first-class explanation layer.

### Default

Show:

```
Analyze       ✓
Build         ✓
Create Runtime ○
Network       ○
Start         ○
Verify        ○
```

### Expanded

Allow the user to inspect:

- selected strategy
- build strategy
- package manager
- build command
- runtime
- start command
- port
- network/proxy
- domain/TLS
- health check
- rollback behavior

### Expert

Expose only when requested:

- image digest
- container ID
- internal routing
- raw probe data
- low-level runtime diagnostics

---

## 9. Failure Disclosure

Failures follow the same hierarchy.

### L1

Show:

- FAILED state
- concise cause
- affected application/deployment
- recovery action

### L2

Show:

- failed step
- timestamp
- actionable diagnostics
- recent events

### L3

Show:

- build/runtime/network details
- health probe
- command/exit code
- relevant logs

### L4

Show:

- container/image identifiers
- raw telemetry
- low-level routing/runtime diagnostics

The interface must not force the user to inspect raw logs to understand that a deployment failed.

---

## 10. Empty / Loading / Processing

States must preserve the same information hierarchy.

### Empty

Explain:

1. what is missing
2. why it matters
3. what the user should do next

### Loading

Show the expected object/context and meaningful progress where available.

### Processing

Show the current deployment step rather than an indeterminate generic spinner when the system knows the step.

### Offline / Degraded

Make the operational impact explicit at L1/L2 and expose diagnostics at L3/L4.

---

## 11. Navigation Rule

Drill-down should feel reversible.

The user should be able to move:

```
Application
  ↓
Deployment
  ↓
Runtime
  ↓
Infrastructure
```

and return without losing:

- selected application
- environment
- deployment
- filters
- relevant time range
- current operation context

---

## 12. Visual Weight Rules

L1 receives the strongest visual hierarchy.

L2 is prominent but secondary.

L3 uses technical panels, drawers, tabs or detail sections.

L4 is intentionally subordinate and diagnostic.

Never make infrastructure internals the visual hero of an ordinary application screen.

---

## 13. Acceptance Criteria

A screen passes this contract when:

- its primary disclosure level is defined
- L1/L2 information is visible without unnecessary interaction
- L3/L4 information is accessible without dominating the screen
- primary action/current state remains obvious
- every technical value has context
- runtime-specific information is relevant
- application/deployment/server context survives drill-down
- no V0.1 workflow requires Kubernetes knowledge
- dense tables and logs remain scannable
- status semantics follow the canonical Design DNA
- technical typography follows the canonical token system

## 14. Non-Negotiable Rules

1. **Never expose complexity merely because it exists.**
2. **Never hide operational state merely to make the UI look simple.**
3. **Never make users understand infrastructure before they need it.**
4. **Never make technical detail inaccessible to an expert who needs it.**
5. **Never introduce a second disclosure vocabulary on an individual screen.**

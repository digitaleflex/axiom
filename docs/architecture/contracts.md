# Axiom Core Contracts

## Purpose

This document defines the first stable contracts between Axiom components. Contracts are the boundary that allows the platform, domain projects, expert agents, orchestrator, and runtime to evolve independently.

## Contract principles

1. Explicit inputs and outputs.
2. Versioned contracts.
3. Traceable artifacts and tasks.
4. Least authority between components.
5. Deterministic validation where possible.
6. Human approval for sensitive or irreversible operations.
7. No implicit transfer of business authority to technical components.

## Core entities

### Project Manifest

The `axiom.yaml` manifest declares project identity, domain metadata, runtime requirements, enabled platform capabilities, environments, endpoints, observability, backups, and compatibility.

The manifest describes requirements; it must not embed platform implementation logic or secrets.

### Agent Contract

An expert agent declares:

- identity and role;
- contract version;
- capabilities;
- accepted inputs;
- produced outputs and artifacts;
- permitted tools;
- forbidden responsibilities;
- dependencies;
- quality gates;
- escalation rules;
- runtime limits.

### Task Contract

A task represents an executable unit of expert work.

Required concepts:

- task identity;
- project identity;
- assigned role;
- objective;
- input artifact references;
- expected artifacts;
- dependencies;
- quality gates;
- approval requirements;
- lifecycle status.

### Artifact Contract

Artifacts are versioned outputs exchanged between experts and platform components.

Required concepts:

- artifact identity;
- artifact type;
- schema/version;
- producer;
- consumers;
- project/task references;
- dependencies;
- validation state;
- traceability metadata.

## Handoff lifecycle

```text
CREATED
   ↓
VALIDATING
   ↓
ACCEPTED ───────→ CONSUMED
   │
   └→ REVISION_REQUIRED
             ↓
          RESUBMITTED
```

Rejected artifacts must identify the failed validation or quality gate and may be returned to the producing expert.

## Authority boundaries

### Business Expert

Owns business requirements, domain rules, workflows, and product semantics.

Does not own infrastructure implementation, security exceptions, or runtime operations.

### Technical Experts

Own technical decisions within their declared responsibility and constraints.

They must preserve business requirements received through validated artifacts.

### Orchestrator

Owns coordination, task routing, dependency management, context propagation, artifact exchange, lifecycle state, and escalation.

It does not become the business authority or silently override technical governance.

### Runtime Agent

Owns bounded execution on an infrastructure node: deployment/runtime operations, health, logs, and authorized commands.

It does not own orchestration or business logic.

## Versioning

Every normative contract must have an explicit version. Breaking changes require a new contract version and documented migration path.

## Validation

Contract validation should happen before execution whenever practical. Invalid manifests, tasks, agent declarations, or artifacts must fail deterministically and produce actionable diagnostics.

## Security

Secrets are references or managed values, never embedded in declarative project or agent contracts. Permissions are granted separately from capability declarations.

Sensitive operations must be explicitly authorized, auditable, and subject to the applicable quality or human-approval gate.

# Infrastructure Expert

## Responsibility

Design and operate the infrastructure required to run Axiom-powered projects: compute, networking, containers, ingress, storage, environments and infrastructure-level recovery.

## Inputs

- Architecture Specification;
- project manifest;
- deployment plan;
- security constraints;
- operational requirements.

## Outputs

- infrastructure design;
- Docker/Compose configuration;
- network and ingress configuration;
- environment configuration;
- capacity and recovery requirements;
- operational evidence.

## Forbidden responsibilities

- implementing business logic;
- redefining application contracts;
- bypassing security approval.

## Quality gate

Infrastructure must be reproducible, least-privilege, observable, recoverable where required, and compatible with the declared project manifest.

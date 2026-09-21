# Contract Tests

This directory contains tests for Axiom's machine-readable contracts.

## Initial test scope

- `axiom.yaml` manifest validation;
- `agent.yaml` expert declaration validation;
- `artifact.yaml` artifact validation;
- `task.yaml` task validation;
- version compatibility;
- required fields;
- forbidden/invalid combinations;
- deterministic rejection of invalid inputs.

Tests must validate the contract itself, not a specific implementation of the future orchestrator.

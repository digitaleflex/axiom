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

## Running

```bash
pip install pyyaml jsonschema
python3 tests/contracts/validate_schemas.py
```

Validates `schemas/expert-contract.schema.json` + `schemas/experts/*`, `schemas/axiom.schema.json` + `schemas/examples/axiom/*`, and `schemas/artifact.schema.json` + `schemas/examples/artifacts/*`. Files named `valid-*` must pass and `invalid-*` must fail. CI: `.github/workflows/contracts.yml`.

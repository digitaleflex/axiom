#!/usr/bin/env python3
"""Validate Axiom contract schemas and their examples (issues #30, #31, #32).

Convention: example files named `valid-*.yaml` must validate; `invalid-*.yaml`
must fail. Run: python3 tests/contracts/validate_schemas.py
Requires: pyyaml, jsonschema.
"""
import glob
import json
import os
import sys

import jsonschema
import yaml

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))


def load_schema(name):
    with open(os.path.join(ROOT, "schemas", name)) as f:
        schema = json.load(f)
    jsonschema.Draft202012Validator.check_schema(schema)
    return jsonschema.Draft202012Validator(schema)


def check(validator, path, expect_valid):
    with open(path) as f:
        data = yaml.safe_load(f)
    errors = list(validator.iter_errors(data))
    ok = (not errors) == expect_valid
    rel = os.path.relpath(path, ROOT)
    if ok:
        print(f"ok    {rel}")
    else:
        detail = errors[0].message if errors else "unexpectedly valid"
        print(f"FAIL  {rel}: {detail}")
    return ok


def main():
    results = []
    expert = load_schema("expert-contract.schema.json")
    for p in sorted(glob.glob(os.path.join(ROOT, "schemas", "experts", "*.yaml"))):
        results.append(check(expert, p, True))

    manifest = load_schema("axiom.schema.json")
    results.append(check(manifest, os.path.join(ROOT, "schemas", "axiom.yaml"), True))
    for p in sorted(glob.glob(os.path.join(ROOT, "schemas", "examples", "axiom", "*.yaml"))):
        results.append(check(manifest, p, os.path.basename(p).startswith("valid-")))

    artifact = load_schema("artifact.schema.json")
    results.append(check(artifact, os.path.join(ROOT, "schemas", "artifact.yaml"), True))
    for p in sorted(glob.glob(os.path.join(ROOT, "schemas", "examples", "artifacts", "*.yaml"))):
        results.append(check(artifact, p, os.path.basename(p).startswith("valid-")))

    failed = results.count(False)
    print(f"\n{len(results) - failed}/{len(results)} passed")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())

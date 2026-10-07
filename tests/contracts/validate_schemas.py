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


def _registry():
    from referencing import Registry, Resource
    with open(os.path.join(ROOT, "schemas", "artifact.schema.json")) as f:
        schema = json.load(f)
    return Registry().with_resource(schema["$id"], Resource.from_contents(schema))


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


def report(ok, label):
    print(f"{'ok   ' if ok else 'FAIL '} {label}")
    return ok


def cross_checks():
    """Semantic rules spanning files (issues #34, #35, #37)."""
    out = []
    with open(os.path.join(ROOT, "schemas", "expert-tools.yaml")) as f:
        catalog = set(yaml.safe_load(f)["tools"])
    with open(os.path.join(ROOT, "schemas", "expert-contract.schema.json")) as f:
        schema_tools = set(json.load(f)["properties"]["tools"]["items"]["enum"])
    out.append(report(catalog == schema_tools, "tool catalog matches contract schema tool enum"))

    experts = {}
    for p in glob.glob(os.path.join(ROOT, "schemas", "experts", "*.yaml")):
        with open(p) as f:
            c = yaml.safe_load(f)
        experts[c["expert"]["id"]] = c
    for eid, c in sorted(experts.items()):
        out.append(report(set(c["tools"]) <= catalog, f"{eid}: tools declared in catalog"))
        missing = [d for d in c["dependencies"] if d not in experts]
        out.append(report(not missing, f"{eid}: dependencies exist {missing or ''}"))

    # acyclic dependency graph
    state = {}

    def visit(n):
        if state.get(n) == 1:
            return False
        if state.get(n) == 2:
            return True
        state[n] = 1
        ok = all(visit(d) for d in experts[n]["dependencies"] if d in experts)
        state[n] = 2
        return ok

    out.append(report(all(visit(n) for n in experts), "expert dependency graph is acyclic"))

    for p in sorted(glob.glob(os.path.join(ROOT, "schemas", "examples", "expert-config", "valid-*.yaml"))):
        with open(p) as f:
            cfg = yaml.safe_load(f)
        c = experts.get(cfg["expert_id"])
        name = os.path.basename(p)
        out.append(report(c is not None, f"{name}: expert_id references a contract"))
        if c:
            ov = cfg.get("overrides", {})
            lim = c["runtime_limits"]
            tight = ov.get("max_execution_seconds", 0) <= lim["max_execution_seconds"] and ov.get("max_retries", 0) <= lim["max_retries"]
            out.append(report(tight, f"{name}: overrides only tighten contract limits"))
    return out


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

    # Profiles produced by the Go profile builder must satisfy the contract.
    profile_validator = jsonschema.Draft202012Validator({"$ref": "https://axiom.dev/schemas/artifact.schema.json#/$defs/ApplicationProfile"},
                                                        registry=_registry())
    for p in sorted(glob.glob(os.path.join(ROOT, "services", "engine", "internal", "profile", "testdata", "golden", "*.json"))):
        with open(p) as f:
            data = json.load(f)
        errors = list(profile_validator.iter_errors(data))
        results.append(report(not errors, f"{os.path.relpath(p, ROOT)} matches ApplicationProfile contract {errors[0].message if errors else ''}"))

    config = load_schema("expert-config.schema.json")
    for p in sorted(glob.glob(os.path.join(ROOT, "schemas", "examples", "expert-config", "*.yaml"))):
        results.append(check(config, p, os.path.basename(p).startswith("valid-")))

    results.extend(cross_checks())

    failed = results.count(False)
    print(f"\n{len(results) - failed}/{len(results)} passed")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())

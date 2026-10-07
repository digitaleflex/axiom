# `axiom.yaml` — Project Manifest Specification v1

> Issue #31. Schema: [`schemas/axiom.schema.json`](../../schemas/axiom.schema.json). Canonical example: [`schemas/axiom.yaml`](../../schemas/axiom.yaml). Examples: [`schemas/examples/axiom/`](../../schemas/examples/axiom/).

## 1. Principle

`axiom.yaml` is **optional in V0.1**. Auto-detection is the default path; existing repositories deploy without rewriting them. The manifest only adds hints or overrides when detection is insufficient or explicit configuration is required.

Location: repository root, or `<app.root>/axiom.yaml` for monorepos (the root manifest wins when both exist).

## 2. Fields

| Field | Purpose | Notes |
|---|---|---|
| `version` | manifest version | required; `1` |
| `app.name` | application identity (DNS-safe slug) | default: repository name |
| `app.root` | application directory in monorepos | relative, no `..` |
| `strategy` | preset hint: `nextjs`, `vite`, `node`, `go`, `dockerfile`, `compose` | must be a V0.1 preset (#96) |
| `runtime.language` / `version` / `packageManager` | stack hints | |
| `build.command` / `build.dockerfile` / `build.context` | build overrides | `dockerfile` strategy forbids `runtime` and `build.command` |
| `start.command` | start override | single line, no backticks |
| `port` | application port | 1–65535 |
| `env[]` | configuration requirements: `name`, `required`, `secret`, `description` | **names only — values forbidden** |
| `services.file` / `include` / `public` | Compose service selection | requires `strategy: compose` |
| `domains[]` | requested hostnames | lowercase FQDN |
| `health` | `type` (`http`/`tcp`), `path`, `expectedStatus`, `timeoutSeconds`, `retries` | `http` requires `path` |
| `constraints` | `architecture`, `minMemoryMB`, `minDiskGB` | used for server eligibility |

Unknown fields are rejected (no silent ignore).

## 3. Precedence

Highest first — consistent with the Application Profile provenance model (`docs/design/screens/analysis/README.md` §4):

1. **Override** — user value entered in the Console
2. **Manifest** — `axiom.yaml`
3. **Detected** — analyzer evidence
4. **Default** — preset default

Each resolved profile value records its provenance. Final precedence implementation is #124.

## 4. Validation Rules (deterministic)

Validation runs during analysis, before profile building. The same manifest always yields the same result.

| Code | Condition |
|---|---|
| `MANIFEST_PARSE_ERROR` | not valid YAML or not a mapping |
| `MANIFEST_VERSION_UNSUPPORTED` | `version` missing or not supported by this Axiom version |
| `MANIFEST_SCHEMA_INVALID` | any JSON Schema violation (unknown field, bad type, pattern, conditional rule) |
| `MANIFEST_SECRET_VALUE` | an `env` entry carries a value (detected before generic schema error) |
| `MANIFEST_STRATEGY_UNSUPPORTED` | `strategy` not available on this Axiom version/server |
| `MANIFEST_CONFLICT` | manifest contradicts repository evidence irreconcilably (e.g. `strategy: compose` without a compose file) |

Errors are reported as findings with `source: axiom_yaml`, path and line when available. An invalid manifest blocks profile resolution; it is never partially applied.

## 5. Version Compatibility

| Manifest `version` | Axiom 0.1.x | Future |
|---|---|---|
| `1` | supported | supported until a deprecation is announced in an ADR |
| `2+` | rejected (`MANIFEST_VERSION_UNSUPPORTED`) | introduced with migration notes |

Additive optional fields may be added within version 1 only if older Axiom versions would reject them explicitly (unknown fields are rejected), so manifests never silently change meaning.

## 6. Examples

| File | Shows |
|---|---|
| `valid-nextjs.yaml` | minimal hint |
| `valid-go.yaml` | commands, port, health |
| `valid-dockerfile.yaml` | Dockerfile strategy, TCP health |
| `valid-compose.yaml` | service selection |
| `valid-monorepo.yaml` | `app.root` |
| `invalid-*.yaml` | each with expected error code in a comment |

## 7. Security

- Secret values never belong in the manifest; values are entered in the Console (write-only).
- Paths cannot escape the repository.
- Commands are data for preset-validated execution inside the build/runtime boundary — never run during analysis.

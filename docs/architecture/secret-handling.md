# Secret Handling

> Issue #126. How the Engine protects GitHub tokens, application secrets and deployment configuration at rest, in logs and at the injection boundary. Complements [deployment-policy.md](./deployment-policy.md) §4.

## 1. Principles

1. **Encryption at rest.** Every secret is sealed with AES-256-GCM (`security/secrets.Box`, key from `AXIOM_SECRET_KEY`) before it touches the database. Plaintext never reaches a column, a log line or an error message.
2. **Binding.** Each ciphertext is bound to `scope + "/" + name` as GCM associated data, so a row cannot be moved to another scope or renamed without detection.
3. **Write-only metadata.** Listing APIs return names and timestamps only. Values are never serialized, logged or wrapped in errors.
4. **Injection by reference.** Plans and profiles carry configuration **names** only (`planner.RuntimePlan.Configuration`). Values are resolved into `KEY=VALUE` env entries at the build/runtime boundary, never stored in the plan.
5. **Redaction.** Anything that could carry a value to an output boundary (logs, API error text) is redacted before exposure.

## 2. At-rest store

`security/secrets.EncryptedStore` (migration `012_secrets.sql`, table `secrets`):

| Column | Purpose |
|---|---|
| `secret_id` | opaque PK (`sec_…`) |
| `scope` | `application:<id>` \| `connection:<id>` |
| `name` | configuration name |
| `value_sealed` | `version ‖ nonce ‖ AES-256-GCM ciphertext` |
| `created_at` / `updated_at` | timestamps |

`UNIQUE (scope, name)`; `scope` and `name` must be non-empty.

| Method | Returns | Note |
|---|---|---|
| `Put(ctx, scope, name, plaintext)` | — | upsert; seals with AAD `scope/name` |
| `Get(ctx, scope, name)` | plaintext | **internal use only** — callers must be authorized to consume the value (GitHub API client, build/runtime injection boundary) |
| `Exists(ctx, scope, name)` | bool | |
| `Delete(ctx, scope, name)` | — | revocation/rotation |
| `ListNames(ctx, scope)` | `[]{Name, UpdatedAt}` | never values |

## 3. Injection boundary

`security/secrets.Resolver` resolves a list of configuration **names** for a scope into a runtime env `[]string` of `KEY=VALUE`:

- `Resolve` skips names with no stored value; `ResolveRequired` fails with `*MissingRequiredError` listing the missing **names only** (never values).
- Output order is deterministic (sorted by name); `Touched` records which names resolved.
- `RedactForOutput(env)` returns a copy with every value replaced by `***`, preserving keys — use before logging or exposing env entries.

## 4. Application configuration service

`internal/secrets` (package `appconfig`) is the application-facing configuration service over `EncryptedStore`, scoped `application:<appID>`:

- `Set(ctx, appID, name, value, secret)` — validates `name` against `^[A-Z_][A-Z0-9_]*$`, stores sealed. The `secret` flag is accepted for forward compatibility: every value in the store is encrypted at rest and write-only, so all entries report `Secret: true`.
- `List(ctx, appID)` → `[]{Name, Secret, IsSet, UpdatedAt}` — metadata only, never values.
- `Delete(ctx, appID, name)` — removes one value.
- `Resolve(ctx, appID, required)` → injection env, delegating to `Resolver.ResolveRequired`.

## 5. Rotation & revocation

- **Rotate:** `Put` again under the same `(scope, name)`; the upsert replaces the ciphertext in place.
- **Revoke:** `Delete`. A revoked name fails resolution (`MissingRequiredError` when required) instead of leaking a stale value.

## 6. Integration points (open)

| Consumer | Status |
|---|---|
| GitHub token storage (`internal/github/auth`) | today: tokens sealed per-connection into `github_connections` columns (#91); migration to `EncryptedStore` scope `connection:<id>` is follow-up work |
| Build env (#83) | `executor` resolves `plan.Runtime.Configuration` names via `appconfig.Service.Resolve` and passes env at `build.Input` construction time |
| Runtime env | `CreateRuntimeRequest` carries resolved env entries to the agent (protocol §6 payload) |
| API surface | configuration write/read endpoints own the `appconfig.Service`; values are write-only in the UI |

## 7. Test strategy

`go test -race -count=1` with `AXIOM_TEST_DATABASE_URL` (isolated schemas per test):

- put/get roundtrip; ciphertext in the database contains no plaintext;
- AAD binding: wrong scope fails to decrypt; upsert updates the value;
- `List` never leaks values (raw DB scan + JSON shape assertions);
- no secret in logs (captured `slog` output); errors never contain plaintext;
- injection resolves required names; missing-required error lists names only;
- `RedactForOutput`; name validation; per-application scope isolation.

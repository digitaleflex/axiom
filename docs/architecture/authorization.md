# Authorization & Resource Ownership Model

> Issue #127. Server-side authorization for applications, servers, deployments and related resources.

## 1. Rule

**The Engine authorizes every operation against the caller's relationship to the resource.** Authorization is deterministic code (`services/engine/internal/authz`), independent of UI visibility: a crafted API request cannot act on a resource the caller does not own.

## 2. V0.1 Policy: Single-User Ownership

An actor may act on a resource only when they own it:

- **Owner**: `application.ownerId == actor.userID` (or the resource's `OwnerID` equals the actor).
- **Member**: future organization boundary (not yet implemented; `authz.NoRoles` returns no membership).
- **None**: everyone else — denied.

The action set is closed (`authz.Action`): `application.read/write/delete`, `deployment.create/cancel`, `server.read/write/register/remove`, `domain.write`, `config.write`, `github.manage`. Unknown actions are denied.

## 3. The 404-Hiding Rule (Deliberate Divergence)

`authz.Authorize` returns **FORBIDDEN semantics** for non-owners, but the API **keeps answering 404** for resource IDs the caller does not own — on reads *and* on writes. This is the single consistent V0.1 rule:

| Caller | Read foreign resource | Write to known foreign ID |
|---|---|---|
| Owner | 200 | 200/201/204 |
| Non-owner | **404 NOT_FOUND** | **404 NOT_FOUND** |

**Why 404 everywhere:** returning 403 on writes would leak the existence of foreign resource IDs (an attacker could distinguish "exists but forbidden" from "does not exist"). 404 everywhere avoids the leak.

**Where authz still matters:** authz is enforced as **defense-in-depth** beneath the hiding rule. Every mutating handler calls `authz.Authorize` before acting. If a future change ever makes a foreign resource visible (e.g. organization sharing), writes still fail closed with FORBIDDEN — the hiding rule alone would not protect them.

## 4. Enforcement Points

| Layer | Mechanism |
|---|---|
| Authentication (#125) | `api.Principal` in request context; 401 when missing |
| Resource loaders | `ownedApplication` / `loadDeployment` / `ownedDomain` resolve ownership via `authz.Resolver`; foreign → 404 |
| Mutating handlers | `authz.Authorize(action, resource)` before every privileged mutation |
| Reads | owner-scoped queries (e.g. `applications.List(ownerID)`) plus the 404-hiding loaders |

## 5. Future Organization Boundary

When organizations land, `authz.Roles` resolves membership and `RelationshipMember` grants read-only access to shared resources. The API call sites do not change — only the `Roles` implementation and the member policy in `authz.Authorize`.

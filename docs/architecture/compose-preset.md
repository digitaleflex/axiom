# Docker Compose Preset & Service Selection (#123)

> Package: `services/engine/internal/runtime/presets/compose/`.
> Build path: `services/engine/internal/build/compose.go`.
> Planner fields: `planner.RuntimePlan.Services`, `planner.RuntimePlan.DependencyOrder`, `planner.NetworkPlan.PublicService`.

## 1. Objective

Deploy an **approved Compose application while only Axiom-owned resources are
modified**. Axiom deploys a bounded subset of Compose: host control is refused,
the network boundary is explicit, and service selection is deterministic.

The manifest fields that drive this are `services.file` / `services.include` /
`services.public` (`docs/architecture/axiom-yaml.md` §2), all requiring
`strategy: compose`.

## 2. Model

`compose.Parse` parses the supported subset into a `Document`:

- `services`: name, `build{context,dockerfile}` (scalar or mapping form),
  `image`, `ports`, `depends_on` (list or mapping), `volumes`,
  `environment` (**names only**), `networks`, `network_mode`, `pid`, `ipc`,
  `privileged`, `volumes_from`.
- top-level `networks` and `volumes`.

Unknown keys are not modelled but are preserved by `compose.Rewrite`, which
mutates the original YAML node tree rather than re-marshalling the struct — a
rewritten document never silently drops user configuration.

## 3. Validation rules

Rules carry stable codes; clients branch on the code, never the message.

| Code | Severity | Condition |
|---|---|---|
| `COMPOSE_NO_SERVICES` | error | the compose file defines no services |
| `COMPOSE_PRIVILEGED` | error | `privileged: true` |
| `COMPOSE_NETWORK_MODE_HOST` | error | `network_mode: host` |
| `COMPOSE_PID_HOST` | error | `pid: host` |
| `COMPOSE_IPC_HOST` | error | `ipc: host` |
| `COMPOSE_HOST_MOUNT_SENSITIVE` | error | host-absolute bind mount of `/`, `/etc`, `/root`, `/home`, `/var/run/docker.sock` |
| `COMPOSE_VOLUMES_FROM` | warn | `volumes_from` shares another container's volumes |
| `COMPOSE_PRIVILEGED_PORT` | error | publishes a host port below 1024 |
| `COMPOSE_HOST_PUBLISH_ALL` | info | publishes on all host interfaces (0.0.0.0) |
| `COMPOSE_PUBLIC_HOST_BIND` | error | a **non-public** service publishes on all host interfaces |
| `COMPOSE_UNKNOWN_SERVICE` | error | `include` names a service that does not exist |
| `COMPOSE_PUBLIC_NOT_INCLUDED` | error | `public` is not part of the selection |
| `COMPOSE_UNKNOWN_DEPENDENCY` | error | `depends_on` references a missing service |
| `COMPOSE_DEPENDENCY_CYCLE` | error | the selected `depends_on` graph has a cycle |
| `COMPOSE_NO_PUBLIC_SERVICE` | warn | no selected service publishes a port |
| `COMPOSE_NO_BUILDABLE_SERVICE` | warn | no selected service has a `build:` section |

## 4. Service selection

`Document.SelectServices(include []string, public string) ValidationResult`
returns `{Services, Public, DependencyOrder, Issues}`:

- an empty `include` selects every service; otherwise exactly the included
  services;
- selection is **closed under `depends_on`** (transitively): a service cannot
  start without its dependencies;
- `DependencyOrder` is a deterministic topological order (Kahn's algorithm,
  name-sorted) with dependencies first; a cycle is an error and the order is
  partial;
- `public` defaults to the first selected service (in dependency order) that
  publishes a port; a public service that is not selected is an error;
- a non-public service that publishes on `0.0.0.0` is an error: it would expose
  itself directly, bypassing Axiom's routing boundary.

## 5. Build path decision

Axiom's build artifact (`docs/architecture/artifacts.md` §3) is a **single**
image reference + digest, and the runtime handoff
(`executor.CreateRuntimeRequest.ImageRef`) is single-image.

- **Single-service compose (option a).** A selection that resolves to exactly
  one service with a `build:` section maps cleanly onto the artifact contract.
  Axiom runs `docker build` on the service context and emits the labeled image
  `axiom-<app>-<service>:<commit>` (labels `axiom.deployment`, `axiom.commit`,
  `axiom.strategy=compose`, `axiom.service`, `axiom.application`).
- **Multi-service compose (option b).** A selection with more than one service
  cannot be represented by a single image. Emitting one of them would hand the
  executor a **half-built Compose**, so Axiom refuses *before building any
  image* and returns a structured `BUILD_COMPOSE_PENDING` error that carries
  the full per-service build plan (service → image). The multi-image handoff is
  owned by #100/#83.

Structured build codes: `BUILD_COMPOSE_FILE_MISSING`,
`BUILD_COMPOSE_INVALID` (parse or policy violation, with rule codes),
`BUILD_COMPOSE_NO_BUILD`, `BUILD_COMPOSE_PENDING`.

## 6. Planner contract

`planner.RuntimePlan` carries `Services` and `DependencyOrder`;
`planner.NetworkPlan` carries `PublicService`. `validateComposeSelection`
(planner package) rejects, for compose plans: an empty selection, duplicate or
empty service names, a public service outside the selection, and a dependency
order that is not exactly the selected services. Acyclicity itself is
guaranteed by the compose preset, which owns the dependency graph.

## 7. Integration notes (#100 / #83)

To complete multi-service Compose deployment:

1. Extend the build artifact/result to carry **multiple** images (one per
   buildable service) plus the rewritten Compose document produced by
   `compose.Rewrite`.
2. Extend the agent runtime contract to accept the Compose document, the
   service selection, and the dependency order — bringing services up in
   `DependencyOrder` and routing only to `PublicService`.
3. Label every created container/network with the canonical ownership labels
   (`services/agent/internal/security/ownership`) so cleanup only touches
   Axiom-owned resources.

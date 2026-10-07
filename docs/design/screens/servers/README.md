# Server Selection, Server Overview & Server Details Screens v1.0

> Screen contracts — issue #138.
>
> Depends on: Infrastructure model (`docs/design/screens/infrastructure/README.md`), Design DNA (#131), Progressive Disclosure (#132), Shell & Navigation (#133).
> Contract sources: `docs/architecture/api-contract.md` §9; #63, #79, #85.

---

## 1. Server Selection

### 1.1 Purpose & action

Choose where the application will run, understanding why each server is or isn't eligible.

Primary action: **Select server** → Deployment Configuration.

### 1.2 Shell

Variant **S**, setup step 3 "Server"; route `/apps/:applicationId/setup/server`. Also reached from Configure → Change (`?from=`), returning with form state preserved.

### 1.3 Layout

```
Stepper: ✓ Analyze ✓ Profile ● Server ○ Configure ○ Plan
Title:   Choose a server                                     [Select server ▸]
Meta:    acme-web needs: container runtime · routing · HTTPS · ~1 GB memory · linux/amd64

Eligible (2)
 (•) srv-eu-1   ● Ready     4 vCPU · 12% │ 8 GB · 5.1 GB free │ 80 GB · 52 GB free   agent v0.1.3   ✓ runtime ✓ routing ✓ HTTPS   seen 5s ago
 ( ) srv-eu-2   ▲ Degraded  2 vCPU · 81% │ 4 GB · 1.2 GB free │ …                    agent v0.1.3   ✓ ✓ ✓                          seen 12s ago
     └ Running with issues: CPU usage high. Deployments may be slow.  [ ] I understand
Not eligible (1)
 ( ) srv-us-1   ⬣ Offline   last known: 2 vCPU · 4 GB                                                                                seen 3h ago
     └ Offline since 11:02 — the agent isn't reachable.
[+ Register a server]
```

### 1.4 Rules

- The **requirements line** (from profile/plan) is shown first so eligibility is explainable.
- Groups: Eligible (incl. with warnings) first, Not eligible collapsed to reasons but visible.
- Each row: state (text + icon), CPU/memory/disk availability (units explicit), agent version/health, required capabilities as ✓/✕ with role labels (hover shows component + version), last seen.
- Not eligible rows are not selectable (`aria-disabled`) and list every failing requirement (infrastructure §4).
- DEGRADED selection requires the acknowledgement checkbox.
- Preselect: last used server for this application, else the only eligible server; never preselect a degraded or ineligible server.
- No servers registered: empty state "Register a server to deploy acme-web" + Register server (opens registration dialog, infrastructure §7, returning here when READY).
- Sort: eligible by available memory desc; stable.

### 1.5 Disclosure

L1: state + eligibility + reasons. L2: resources, capability ✓/✕, agent version. L3: component versions, architecture, exact free values (mono).

---

## 2. Server Overview

### 2.1 Purpose & action

Inventory of servers and their operational health.

Primary action: **Register server**.

### 2.2 Shell

Variant **I**, sidebar "Servers", route `/servers`, query `status`.

### 2.3 Table (priority from #138)

| Column | Content |
|---|---|
| Server | name + architecture (mono, secondary) |
| State | READY / DEGRADED / OFFLINE / PENDING (text + icon) + reason line for non-READY |
| Resources | CPU % · memory used/total · disk free — compact bars with numeric values |
| Agent | version + Connected/Late/Disconnected |
| Container runtime | Available / Unhealthy (+ version on hover) |
| Routing | Available / Unhealthy |
| Applications | count of Axiom-managed applications → filtered list |
| Last seen | relative (mono absolute on hover) |

- Summary strip above table: "3 servers · 2 ready · 1 offline" (text, each count is a filter).
- Non-READY rows sorted first by default ("Needs attention").
- Recent events panel (right or below): heartbeat transitions, capability changes, registration — each with server, time, link.
- Row → Server Details.

### 2.4 States

| State | Behavior |
|---|---|
| Empty | "No servers yet. Axiom deploys to servers you register." + Register server + short explanation of the agent (no command shown until dialog) |
| Loading | skeleton rows |
| All offline | page banner WARNING "No server is reachable — deployments can't start" |

Responsive: < `--bp-md` cards: name + state + reason / resources line / apps + last seen.

---

## 3. Server Details

### 3.1 Purpose

Operate one server in the context of the deployments it runs. Host-level detail is secondary and deliberately disclosed.

State-dominant (no primary button). Return affordance when reached from a deployment (`← Back to Deployment #42 · Production`, navigation §7).

### 3.2 Shell

Variant **I**, sidebar "Servers", route `/servers/:serverId`. Sections as in-page tabs with query `section`: `overview` (default), `applications`, `deployments`, `capabilities`, `network`, `diagnostics`.

### 3.3 Header

```
Title:  srv-eu-1  ● Ready                                         [⋯]
Meta:   linux/amd64 · agent v0.1.3 · seen 5s ago · registered 12 Sep 2026
```

Overflow: Rename, Rotate agent credential, Remove server (infrastructure §6). No shell, exec, reboot or host command actions.

### 3.4 Sections & disclosure (#138 progressive order)

| Section | Content | Level |
|---|---|---|
| Overview | state + reason, resource summary (CPU/memory/disk with 1h sparkline), Axiom-managed application count, recent events | L1/L2 |
| Applications (runtime inventory) | **Axiom-managed** runtimes: application · environment chip · deployment # · runtime status · port · CPU/mem — each links to the application/deployment. Then collapsed "Other workloads on this server — not managed by Axiom" aggregate (count, resource share) | L2 / L3 |
| Deployments | deployment history targeting this server (same table as Application Deployments with Application column) | L2 |
| Capabilities | capability table (infrastructure §3) + agent heartbeat: interval, last heartbeats timeline, credential age | L2/L3 |
| Network | routing state, domains served by this server (→ Domains), public address (mono) | L3 |
| Diagnostics | resource telemetry charts (metrics chart rules), agent events log (log viewer), host facts (kernel, OS, versions); container IDs and internal routing identifiers in L4 expanders with context path | L3/L4 |

Application ↔ server relationships are navigable both ways: application Runtime card → server; server Applications section → application/deployment.

### 3.5 States

| State | Behavior |
|---|---|
| OFFLINE | banner "Not reachable since {time}" + troubleshooting hints (agent process, network) as text; all data marked stale with timestamp |
| DEGRADED | banner with reasons, each linking to the relevant section |
| PENDING | registration waiting view (infrastructure §7) |
| Remove blocked | dialog lists Axiom-managed runtimes to move first |

---

## 4. Accessibility

- Selection list is a radio group; ineligible options `aria-disabled` with reasons in `aria-describedby`.
- Resource bars always have numeric text.
- State changes (server goes offline while viewing) announced politely.

## 5. Acceptance Checklist

- [ ] Eligibility explained per server with all failing requirements and fixes.
- [ ] READY / DEGRADED / OFFLINE always text + icon with reason and last seen.
- [ ] No raw privileged host operation exposed as an action.
- [ ] Application ↔ server navigable in both directions.
- [ ] Axiom-managed resources separated from unmanaged workloads.
- [ ] Diagnostics and identifiers behind L3/L4 disclosure.
- [ ] No Kubernetes/pod/mesh concepts.

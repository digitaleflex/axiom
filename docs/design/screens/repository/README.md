# Repository List & Repository Detail Screens v1.0

> Screen contracts — issue #134.
>
> Depends on: Design DNA (#131), Progressive Disclosure (#132), Shell & Navigation (#133).
> Contract sources: `docs/architecture/api-contract.md` §5–§7, #92 (repository & ref discovery).

---

## 1. Repository List

### 1.1 Purpose & action

Find the repository to deploy among those Axiom can access. Primary action per row: **Select** → Repository Detail.

### 1.2 Shell

- Variant **W**, sidebar "Repositories", route `/repositories`.
- Query state: `q` (search), `connection` (GitHub account/org), `page` (navigation §5.4).

### 1.3 Layout

```
Breadcrumbs: Workspace › Repositories
Title:       Repositories                                  [Manage GitHub access ↗]
Meta:        142 repositories from 2 GitHub accounts
[🔍 Search repositories]  [Account: all ▾]  [Visibility: all ▾]

Repository          Visibility  Language    Default branch  Updated   Axiom
acme/web            Private     TypeScript  main            2h ago    ■ Production ● LIVE
acme/api            Private     Go          main            1d ago    Not deployed
jane/blog           Public      JavaScript  master          3w ago    Not deployed
```

### 1.4 Columns

| Column | Content | Rule |
|---|---|---|
| Repository | `owner/name` mono; owner avatar | whole row is the link to Detail |
| Visibility | Private / Public text + lock icon | never color-only |
| Language | GitHub primary language | label as "GitHub language" in header tooltip — not Axiom's detection |
| Default branch | mono | |
| Updated | relative time; absolute on hover (mono) | |
| Axiom | existing applications: environment chip (abbreviated allowed) + status pill of most relevant deployment; else "Not deployed" in `--text-tertiary` | max one chip; "+1" when several |

Language from GitHub is explicitly not presented as Axiom's analysis (no silent assumption).

### 1.5 States

| State | Behavior |
|---|---|
| Loading | 8 skeleton rows; filters usable |
| Empty — no connection | redirect target: empty state "Connect GitHub to see your repositories" + **Connect GitHub** |
| Empty — no accessible repositories | "Axiom can't see any repositories" + why (GitHub App installed on selected repositories only) + **Manage GitHub access ↗** |
| Empty — no search match | "No repositories match `q`" + Clear search |
| Connection degraded | shell banner "Reconnect GitHub" (shell §9); list shows last known data marked stale |
| Error | inline error panel with Retry; filters preserved |

### 1.6 Responsive

Below `--bp-md`: rows become two-line cards (repository + Axiom status / language + updated); Visibility and Default branch move to L2 details.

---

## 2. Repository Detail

### 2.1 Purpose & action

Choose the ref to deploy and start analysis. Primary action: **Analyze**.

### 2.2 Shell

- Variant **W**, sidebar "Repositories", route `/repositories/:repositoryId`, query `ref` (selected branch/tag).

### 2.3 Layout

```
Breadcrumbs: Workspace › Repositories › acme/web
Title:       acme/web  Private                                   [Analyze ▸]
Meta:        github.com/acme/web ↗ · default branch main

┌ Ref ──────────────────────────────────────────────┐
│ [main ▾]   3f9c2a1  "fix: header overflow"  2h ago │
└────────────────────────────────────────────────────┘
┌ Application name ─────────┐ ┌ How analysis works ────────────────────┐
│ [acme-web            ]    │ │ Axiom reads repository files to detect │
│ Used in URLs and the      │ │ runtime, framework and commands.       │
│ console. Editable later.  │ │ No code is executed.                   │
└───────────────────────────┘ └────────────────────────────────────────┘
┌ Axiom applications from this repository ──────────────────────────┐
│ acme-web   ■ Production ● LIVE   ◧ Staging ● LIVE   [Open]         │
└────────────────────────────────────────────────────────────────────┘
```

### 2.4 Ref selector

- Combobox with search; groups: Branches (default branch first, marked "default"), Tags.
- Each option: ref name (mono), short SHA (mono), last commit relative time.
- Selected ref resolves to a commit SHA displayed in the meta; the analysis runs against that exact commit.

### 2.5 Analyze behavior

1. If no application exists for this repository: `POST /applications` (repositoryId, name), then `POST /applications/{id}/analysis` (ref).
2. If one exists: user chooses in a small dialog — "Analyze for acme-web" (default) or "Create new application".
3. Navigate to `/apps/:applicationId/setup/analysis`.
4. Button shows loading and is disabled during the request; double submission must not create two applications (client guard; server idempotency per #116).

Application name: prefilled from repository name (slugified), validated inline (allowed characters, uniqueness error from API).

### 2.6 Disclosure

| Level | Content |
|---|---|
| L1 | repository, ref, Analyze |
| L2 | commit, application name, existing applications, analysis explanation |
| L3 | full SHA (copy), repository ID |

### 2.7 States

| State | Behavior |
|---|---|
| Loading | header skeleton; ref selector disabled |
| Ref not found (`?ref=` invalid) | inline error in ref card; falls back to showing default branch as suggestion, not auto-selected |
| Empty repository | "This repository has no commits" ; Analyze disabled with reason |
| Access lost | "Axiom no longer has access to this repository" + Manage GitHub access |
| Create/analyze error | inline error near the action; input preserved |

---

## 3. Contract Mapping & Gaps

| UI need | Contract | Status |
|---|---|---|
| List repositories (search, pagination) | `GET /github/connections/{id}/repositories` | available per connection; **gap**: cross-connection listing with search → client merges or #92 adds aggregate endpoint |
| Repository detail | `GET /repositories/{id}` | available |
| Branches/tags with head commit | `GET /repositories/{id}/refs` | available; commit message/time pending #92 |
| Applications for a repository | `GET /applications` | **gap**: needs `repositoryId` filter |
| Latest deployment status per application/environment | — | depends on environment field (#71 / #117, see navigation §10) |

## 4. Acceptance Checklist

- [ ] GitHub-reported language visibly distinct from Axiom detection.
- [ ] Three distinct empty states (no connection / no access / no match).
- [ ] Ref resolves to and displays an exact commit before analysis.
- [ ] Analyze cannot create duplicate applications.
- [ ] "No code is executed" stated before analysis starts.

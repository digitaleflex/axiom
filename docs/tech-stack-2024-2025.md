# 📚 Axiom — Stack Technologique & Feuille de Route 2024/2025

> **Référence rapide** : tous les outils modernes, validés, battle-tested, pour éviter de réinventer la roue.
> 
> **Date** : 2026-10-10 · **Version** : 1.0

---

## 🎯 **Principe Directeur**

> **"Si vous écrivez du boilerplate, quelqu'un l'a déjà fait mieux en open source. Importez-le."**

Chaque fois que vous commencez à écrire un composant métier, vérifiez d'abord si un outil open source le fait déjà.

| Vous voulez écrire... | Utilisez plutôt |
|---|---|
| Un middleware auth | **Better Auth** / **Casbin** |
| Une migration SQL | **Goose** / **Atlas** |
| Un type Go depuis SQL | **SQLC** |
| Un composant UI | **shadcn/ui** / **Radix** |
| Un formulaire | **React Hook Form** + **Zod** + Server Action |
| Un test d'intégration | **Testcontainers** |
| Un mock | **Mockery** |
| Une config | **Koanf** |
| Un log structuré | **Zerolog** |
| Une métrique | **OpenTelemetry** |
| Un secret en prod | **External Secrets** / **1Password** |
| Un déploiement | **Flux CD** / **ArgoCD** |

---

## 1️⃣ **Backend (Go) — Engine + Agent**

| Couche | Outil Moderne | Pourquoi |
|--------|---------------|----------|
| **API Framework** | `go-chi/chi/v5` | Léger, stdlib-compatible, middlewares composables |
| **OpenAPI/Swagger** | `oapi-codegen` | Code-first → spec → clients générés |
| **Validation** | `go-playground/validator/v10` | Struct tags + compile-time |
| **Database** | **SQLC** (SQL → Go types) | Type-safe, zero runtime overhead, pas d'ORM magic |
| **Migrations** | **Goose** / **Atlas** / **golang-migrate** | Versioned, reversible, CI |
| **Config** | **Koanf** / **Viper** / **Cleanenv** | Structuré, validé, hot-reload |
| **Logging** | **Zerolog** | Structured, leveled, sampling, JSON prod |
| **Metrics** | **Prometheus Go client** | Auto-instrumentation, exemplars |
| **Tracing** | **OpenTelemetry Go SDK** | Traces + metrics + logs unifiés |
| **Auth/JWT** | `golang-jwt/jwt/v5` + **Casbin** (RBAC) | Battle-tested, policy engine |
| **Secrets** | **Vault** / **Sealed Secrets** / **1Password SDK** | Chiffré, rotation auto |
| **Queue/Jobs** | **Asynq** (Redis) / **Temporal** (workflows) | Durable, retry, scheduling |
| **Testing** | **Testify** + **Mockery** + **Testcontainers-Go** | Mocks auto, intégration réelle |
| **Dependency Injection** | **Wire** (compile-time) | Compile-time, zéro runtime |
| **Validation** | `validator/v10` | Struct tags +-rules |
| **Serialization** | `encoding/json` + `oapi-codegen` | Types générés depuis spec |

### 📦 **go.mod推荐 (Engine)**

```go
module github.com/digitaleflex/axiom/services/engine

go 1.23

require (
	// API
	github.com/go-chi/chi/v5 v5.2.0
	github.com/oapi-codegen/runtime v1.0.0

	// DB
	github.com/jackc/pgx/v5 v5.6.0
	github.com/sqlc-dev/sqlc v1.27.0

	// Config
	github.com/knadh/koanf/v2 v2.1.0

	// Observability
	github.com/rs/zerolog v1.32.0
	go.opentelemetry.io/otel v1.25.0

	// Auth
	github.com/golang-jwt/jwt/v5 v5.2.1
	github.com/casbin/casbin/v2 v2.9.0

	// Jobs
	github.com/hibiken/asynq v1.8.0

	// Testing
	github.com/stretchr/testify v1.9.0
	github.com/testcontainers/testcontainers-go v0.27.0
)
```

---

## 2️⃣ **Frontend (Next.js 16 + React 19) — Client Dashboard**

| Couche | Outil Moderne | Pourquoi |
|--------|---------------|----------|
| **Framework** | **Next.js 16** (App Router, RSC, Server Actions) | Standard, RSC, streaming, edge |
| **Language** | **TypeScript 5.6 strict** | Types, sécurité, DX |
| **Build** | **Turbopack** (stable Next 16) | 10x plus vite que Webpack |
| **Styling** | **Tailwind CSS v4** + CSS Variables | Utility-first, rapide, customizable |
| **UI Components** | **shadcn/ui** (Radix + Tailwind) | Copy-paste, accessible, no vendor lock |
| **State (Server)** | **TanStack Query v5** | Cache, dedup, retry, infinite scroll |
| **State (Client)** | **Zustand** / **Jotai** | Léger, performant, pas de boilerplate |
| **Routing** | **TanStack Router** | Type-safe routes, search params validation |
| **Forms** | **React Hook Form v7** + **Zod** | Validation partagée client/serveur |
| **Auth** | **Better Auth v1** + plugins | Org, admin, passkey, 2FA, magic link |
| **Charts** | **Recharts** / **Tremor** | React natifs, responsive, accessible |
| **Tables/Data Grid** | **TanStack Table v8** (headless) | Sorting, filtering, pagination, virtual |
| **Date/Time** | **date-fns v4** | Tree-shakeable, moderne |
| **i18n** | **next-intl** / **Lingui** | Type-safe i18n |
| **Testing** | **Vitest** + **Playwright** + RTL | Rapide unitaire, E2E realistic |
| **Storybook** | **Storybook 8** (zero-config) | Documentation, design system |
| **Lint/Format** | **Biome** (Rust) | 10x ESLint+Prettier, tout-en-un |
| **Package Manager** | **pnpm 9** | Fast, disk-efficient, strict |
| **State (Server)** | **Server Actions** (React 19) | Zéro API route boilerplate |

### 📦 **package.json Client Dashboard**

```json
{
  "name": "@axiom/client-dashboard",
  "version": "0.1.0",
  "private": true,
  "scripts": {
    "dev": "next dev --turbopack",
    "build": "next build",
    "start": "next start",
    "lint": "biome check .",
    "format": "biome format --write .",
    "typecheck": "tsc --noEmit",
    "test": "vitest run",
    "test:ui": "vitest --ui",
    "e2e": "playwright test",
    "storybook": "storybook dev -p 6006",
    "build-storybook": "storybook build"
  },
  "dependencies": {
    "next": "16.4.0",
    "react": "19.0.0",
    "react-dom": "19.0.0",
    "@tanstack/react-query": "^5.51.0",
    "@tanstack/react-router": "^1.50.0",
    "@tanstack/react-table": "^8.19.0",
    "better-auth": "^1.0.0",
    "@better-auth/plugin-organization": "^1.0.0",
    "@better-auth/plugin-admin": "^1.0.0",
    "@better-auth/plugin-two-factor": "^1.0.0",
    "@better-auth/plugin-passkey": "^1.0.0",
    "@better-auth/plugin-magic-link": "^1.0.0",
    "react-hook-form": "^7.52.0",
    "@hookform/resolvers": "^3.9.0",
    "zod": "^3.23.0",
    "lucide-react": "^0.428.0",
    "recharts": "^2.12.0",
    "date-fns": "^4.1.0",
    "clsx": "^2.1.1",
    "tailwind-merge": "^2.5.0",
    "class-variance-authority": "^0.7.0",
    "@radix-ui/react-slot": "^1.1.0",
    "@radix-ui/react-dialog": "^1.1.1",
    "@radix-ui/react-dropdown-menu": "^2.1.1",
    "@radix-ui/react-toast": "^1.2.1",
    "@radix-ui/react-tabs": "^1.1.0",
    "@radix-ui/react-tooltip": "^1.1.2",
    "@radix-ui/react-select": "^2.1.1",
    "@radix-ui/react-avatar": "^1.1.0",
    "@radix-ui/react-scroll-area": "^1.1.0",
    "@radix-ui/react-separator": "^1.1.0",
    "@radix-ui/react-switch": "^1.1.0",
    "@radix-ui/react-checkbox": "^1.1.1",
    "@radix-ui/react-label": "^2.1.0",
    "@radix-ui/react-popover": "^1.1.1",
    "@radix-ui/react-alert-dialog": "^1.1.1",
    "posthog-js": "^1.150.0"
  },
  "devDependencies": {
    "typescript": "^5.6.0",
    "@types/react": "^18.3.0",
    "@types/node": "^22.0.0",
    "@biomejs/biome": "1.8.0",
    "tailwindcss": "^4.0.0",
    "@tailwindcss/postcss": "^4.0.0",
    "postcss": "^8.4.41",
    "tw-animate-css": "^1.2.0",
    "vitest": "^2.0.0",
    "@testing-library/react": "^16.0.0",
    "@playwright/test": "^1.46.0",
    "storybook": "^8.2.0",
    "@storybook/nextjs": "^8.2.0",
    "@storybook/react": "^8.2.0",
    "@storybook/addon-essentials": "^8.2.0",
    "@chromatic-com/storybook": "^1.6.0"
  }
}
```

### 🔌 **Better Auth v1 + React 19**

```typescript
// src/lib/auth.ts
import { betterAuth } from "better-auth";
import { organization, admin, twoFactor, passkey, magicLink } from "better-auth/plugins";

export const auth = betterAuth({
  database: db, // Prisma/Drizzle
  trustedOrigins: [process.env.NEXT_PUBLIC_APP_URL!],
  
  // React 19: Server Actions support
  serverActions: {
    enabled: true,
  },
  
  plugins: [
    organization({
      teams: { enabled: true }, // Pour M12.6
    }),
    admin(),
    twoFactor(),
    passkey(),
    magicLink(),
  ],
  
  hooks: {
    after: [
      {
        matcher: (ctx) => ctx.path.startsWith("/api/auth/"),
        handler: async (ctx) => {
          await db.auditLog.create({ /* ... */ });
        },
      },
    ],
  },
});

export type Session = typeof auth.$Infer.Session;
export type User = typeof auth.$Infer.User;
```

---

## 3️⃣ **Infrastructure & DevOps**

| Besoin | Outil | Pourquoi |
|--------|-------|----------|
| **Container Orchestration** | **Kubernetes (k3s / Talos / EKS/GKE)** | Standard, auto-healing, scaling |
| **Service Mesh** | **Cilium (eBPF)** | mTLS, NetworkPolicy, observabilité L7 |
| **Ingress** | **Traefik v3 / NGINX / Caddy** | ACME auto, middleware |
| **Certificats** | **cert-manager** + LE/ZeroSSL | Automatique, wildcard |
| **Secrets** | **External Secrets Operator** | GitOps-safe, rotation auto |
| **GitOps** | **Flux CD v2** / ArgoCD | Declarative, drift detection |
| **Image Registry** | **GHCR / Harbor / ECR** | cosign (signing), Trivy (scanning) |
| **Image Build** | **BuildKit / Earthly / Dagger** | Cache, multi-arch, reproductible |
| **Runtime Security** | **Falco / Tetragon (eBPF)** | Syscall monitoring, threat detect |
| **Vuln Scanning** | **Trivy / Grype / Syft** | CI/CD, SBOM, licenses |
| **Observabilité** | **Grafana Stack (Loki, Tempo, Mimir, Pyroscope)** | Unified, open source |
| **Logs** | **Loki + Promtail / Vector** | Labels, grep, coût maîtrisé |
| **Metrics** | **Prometheus / Mimir / VictoriaMetrics** | Long-term, HA |
| **Tracing** | **Tempo / Jaeger** + OTel | Distributed tracing |
| **Profiling** | **Pyroscope (continuous)** | CPU, Memory, Goroutines |
| **Alerting** | **Alertmanager + Grafana Alerting** | Routes, silences, inhibitions |
| **Incident Mgmt** | **PagerDuty / Grafana OnCall** | On-call, escalades |
| **Backup/DR** | **Velero / KubeBack** | Cluster backup, restore |
| **Cost** | **Kubecost / OpenCost** | Showback, rightsizing |

---

## 4️⃣ **Developer Experience (DX)**

| Catégorie | Outil | Gain |
|-----------|-------|------|
| **Shell** | **Nushell** + Starship | Structured, completions |
| **Terminal** | **Ghostty / WezTerm / Kitty** | GPU, ligatures, tabs |
| **Editor** | **Neovim (LazyVim)** / VS Code + Extensions | LSP, DAP, Treesitter, AI |
| **Git** | **GitButler / Magit / Lazygit** | Branches virtuelles, stacked PR |
| **CLI HTTP** | **HTTPie / xh / curlie** | Highlight, JSON auto |
| **JSON/Query** | **jq / dasel / gron** | Query, transform |
| **Diff** | **difftastic / delta** | AST-aware |
| **Search** | **ripgrep (rg) / fd / fzf** | Instant, regex, preview |
| **Process** | **btop / glances** | Resources, disks, GPU |
| **DB CLI** | **pgcli / mycli / usql** | Autocompletion |
| **K8s CLI** | **kubectl + kubectx + k9s** | Context switch, TUI |
| **Container Debug** | **crictl / nerdctl / dive** | Layers, inspection |
| **API Testing** | **Bruno / Hoppscotch** | Git-friendly, offline |
| **Documentation** | **Mintlify / Docusaurus / Redocly** | OpenAPI → Docs |
| **Diagrammes** | **Excalidraw / Mermaid / Structurizr** | Architecture as code |
| **Secrets Local** | **1Password CLI / Direnv / SOPS / age** | Jamais .env, chiffré |
| **Deps Updates** | **Renovate + Merge Queue** | Auto-PR, semantic |

---

## 5️⃣ **Checklist Adoption**

### 📅 **Cette Semaine (Quick Wins)**
- [ ] **Biome** au lieu ESLint+Prettier (1 outil, 10x plus vite)
- [ ] **pnpm** au lieu npm (disk, speed, strictness)
- [ ] **SQLC** pour Engine (SQL → types Go, zero overhead)
- [ ] **Goose** pour migrations (versioned, reversible, CI)
- [ ] **Zerolog** pour logs (sampling, pretty dev, JSON prod)
- [ ] **Testcontainers** pour tests intégration (PostgreSQL, Redis)
- [ ] **Mockery** pour mocks auto-générés

### 🗓️ **Ce Mois (Foundation)**
- [ ] **OpenTelemetry** sur Engine + Agent (traces + metrics + logs)
- [ ] **External Secrets Operator** + 1Password/Vault
- [ ] **Flux CD** pour GitOps (déploiement = `git push`)
- [ ] **Trivy** en CI (scan images + config + SBOM)
- [ ] **Renovate** (auto-PR deps, merge queue)
- [ ] **Better Auth** sur Client Dashboard

### 📅 **Ce Trimestre (Scale)**
- [ ] **Temporal** pour workflows longs (multi-étapes, rollback)
- [ ] **Cilium** (eBPF network policy, mTLS, L7 obs)
- [ ] **Pyroscope** (profiling continu en prod)
- [ ] **PostHog** self-hosté (analytics + flags + replay)
- [ ] **Turborepo** / **Nx** si monorepo

---

## 6️⃣ **Verdict Stack Actuelle**

| Couche | État | Action Recommandée |
|--------|------|---------------------|
| **Engine (Go)** | ✅ Bon (stdlib) | + SQLC, Goose, Zerolog, OTel, Asynq, Wire |
| **Agent (Go)** | ✅ Bon (stdlib) | + SQLC, OTel, Asynq, better config |
| **Client Dashboard** | ⏳ À créer | **Next 16 + React 19 + Better Auth + TanStack + shadcn/ui** |
| **Infra** | ⚠️ Docker Compose + Traefik | **K3s + Flux + cert-manager + External Secrets + Loki/Tempo/Mimir** |
| **CI/CD** | ⚠️ GH Actions basique | **+ Renovate, Trivy, Testcontainers, Merge Queue** |
| **Observabilité** | ⚠️ Logs basiques | **Grafana Stack + OTel** |

---

## 7️⃣ **Stack Finale Validée — Résumé**

### **Backend (Go)**
| Couche | Choix |
|--------|--------|
| Framework | `go-chi/chi/v5` |
| DB | PostgreSQL + `pgx/v5` + **SQLC** |
| Migrations | **Goose** |
| Config | **Koanf** |
| Logs | **Zerolog** |
| Observabilité | **OpenTelemetry** |
| Auth | `golang-jwt/jwt/v5` + **Casbin** |
| Jobs | **Asynq** (Redis) / **Temporal** |
| Tests | **Testify** + **Mockery** + **Testcontainers** |
| DI | **Wire** (compile-time) |

### **Frontend (Next.js)**
| Couche | Choix |
|--------|--------|
| Framework | **Next.js 16** (App Router) |
| Runtime | **React 19** |
| Build | **Turbopack** |
| Language | **TypeScript 5.6 strict** |
| Styling | **Tailwind CSS v4** |
| UI | **shadcn/ui** (Radix + Tailwind) |
| State Server | **TanStack Query v5** + Server Actions |
| State Client | **Zustand** / **Jotai** |
| Routing | **TanStack Router** |
| Forms | **RHF v7** + **Zod** |
| Auth | **Better Auth v1** + plugins |
| Charts | **Recharts** / **Tremor** |
| Tables | **TanStack Table v8** |
| Date | **date-fns v4** |
| Testing | **Vitest** + **Playwright** + RTL |
| Storybook | **Storybook 8** |
| Lint/Format | **Biome** |
| Package Manager | **pnpm 9** |

### **Infrastructure**
| Couche | Choix |
|--------|--------|
| Orchestration | **K3s** / **Kubernetes** |
| Ingress | **Traefik v3** |
| Certificats | **cert-manager** |
| Secrets | **External Secrets** + **Vault** / **1Password** |
| GitOps | **Flux CD v2** |
| Registry | **GHCR** / **Harbor** |
| Security | **Trivy** + **Falco** + **cosign** |
| Observabilité | **Grafana Stack** (Loki, Tempo, Mimir, Pyroscope) |
| Alerting | **Alertmanager** + **Grafana OnCall** |
| Backup/DR | **Velero** |
| Cost | **Kubecost** / **OpenCost** |

### **DevOps**
| Catégorie | Choix |
|-----------|--------|
| CI/CD | **GitHub Actions** + **Renovate** + **Merge Queue** |
| Testing | **Testcontainers** + **Playwright** |
| Scan | **Trivy** + **Syft** (SBOM) |
| Docs | **Mintlify** / **Docusaurus** |
| Diagrammes | **Mermaid** / **Structurizr** |

---

## 8️⃣ **🔒 Sécurité — Secrets Management**

### **Local Development**
```bash
# 1Password CLI (équipe)
op inject -i .env.example -o .env.local

# Direnv (auto-load)
echo 'dotenv_if_exists .env.local' > .envrc
direnv allow

# age (SOPS)
sops -e -i .env.encrypted.yaml
```

### **Production (Kubernetes)**
```yaml
# ExternalSecret CRD
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: axiom-secrets
spec:
  refreshInterval: 1h
  secretStoreRef:
    name: vault-backend
    kind: SecretStore
  target:
    name: axiom-runtime-secrets
  data:
    - secretKey: DATABASE_URL
      remoteRef:
        key: production/axiom
        property: database_url
```

### **.gitignore Strict**
```gitignore
# Environment
.env
.env.local
.env.*.local

# Secrets
auth-secret.txt
*.pem
*.key
.github-app-private-key.pem

# Dependencies
node_modules/
.pnp

# Build
.next/
dist/
build/
out/

# IDE
.vscode/
.idea/

# OS
.DS_Store
Thumbs.db

# Logs
*.log
npm-debug.log*

# Testing
coverage/
.nyc_output/

# Turbo
.turbo/

# TypeScript
*.tsbuildinfo
next-env.d.ts

# Certs
certs/
```

### **`.env.example` (Commité)**
```env
# App
NEXT_PUBLIC_APP_URL=http://localhost:3000
NEXT_PUBLIC_API_URL=http://localhost:8080/api/v1

# Better Auth
BETTER_AUTH_SECRET=
BETTER_AUTH_URL=http://localhost:3000

# GitHub OAuth
GITHUB_CLIENT_ID=
GITHUB_CLIENT_SECRET=

# GitHub App (M12.2)
GITHUB_APP_ID=
GITHUB_APP_WEBHOOK_SECRET=

# Database
DATABASE_URL=

# Email
EMAIL_FROM=noreply@axiom.run
RESEND_API_KEY=

# Analytics
NEXT_PUBLIC_POSTHOG_KEY=
NEXT_PUBLIC_POSTHOG_HOST=

# Stripe (M12.4)
STRIPE_SECRET_KEY=
STRIPE_WEBHOOK_SECRET=
NEXT_PUBLIC_STRIPE_PUBLISHABLE_KEY=
```

---

## 9️⃣ **📊 Roadmap d'Adoption**

### **Phase 1 — Cette Semaine (Quick Wins)**
```
Biome + pnpm + SQLC + Goose + Zerolog + Testcontainers + Mockery
```
**Impact** : -50% temps dev, +100% vitesse tests

### **Phase 2 — Ce Mois (Foundation)**
```
OpenTelemetry + External Secrets + Flux CD + Trivy + Renovate + Better Auth
```
**Impact** : Observabilité complète, secrets sécurisés, GitOps

### **Phase 3 — Ce Trimestre (Scale)**
```
Temporal + Cilium + Pyroscope + PostHog + Turborepo
```
**Impact** : Workflows résilients, réseau isolé, analytics

---

## 🔗 **Références Rapides**

| Besoin | Documentation |
|--------|--------------|
| Next.js 16 | https://nextjs.org/docs |
| Better Auth | https://better-auth.com/docs |
| TanStack Query | https://tanstack.com/query/latest |
| shadcn/ui | https://ui.shadcn.com/docs |
| Tailwind v4 | https://tailwindcss.com/docs |
| SQLC | https://sqlc.dev/docs |
| Koanf | https://github.com/knadh/koanf |
| Zerolog | https://zerolog.io |
| OpenTelemetry Go | https://opentelemetry.io/docs/languages/go/ |
| Flux CD | https://fluxcd.io/flux/ |
| External Secrets | https://external-secrets.io/latest/ |
| cert-manager | https://cert-manager.io/docs/ |
| Grafana Stack | https://grafana.com/oss/ |
| Trivy | https://trivy.dev/latest/ |
| Playwright | https://playwright.dev |
| Vitest | https://vitest.dev |
| Biome | https://biomejs.dev |
| pnpm | https://pnpm.io |

---

## ✅ **Résumé Final**

| Couche | Stack | Niveau |
|--------|-------|--------|
| **Backend Go** | Chi + SQLC + Goose + Zerolog + OTel + Asynq + Casbin + Wire | 🟢 Moderne |
| **Frontend Next.js** | Next 16 + React 19 + Turbopack + Tailwind 4 + shadcn/ui + Better Auth + TanStack | 🟢 Cutting-edge |
| **Infrastructure** | K3s + Traefik + Flux + cert-manager + External Secrets + Grafana Stack | 🟢 Production-ready |
| **DevOps** | GitHub Actions + Renovate + Trivy + Testcontainers + Playwright | 🟢 Automatisé |
| **DX** | Biome + pnpm + LazyVim + Ghostty + Bruno + Mintlify | 🟢 Productif |

---

**Dernière mise à jour** : 2026-10-10 · **Maintainer** : Axiom Team

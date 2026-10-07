# Axiom Cloud Console

Vite + React + TypeScript frontend for the Axiom Cloud Console.

This app implements **Wave-1 Lane E — issue #119 (Application Shell & Auth)**:
routing, the authenticated shell, the API client and the system-state
components. Real screens (GitHub → Deploy workflow, application/deployment
areas) are wired in **#120** on top of this scaffold.

Design contracts (read-only, source of truth):

- `docs/design/handoff/README.md` — entry point
- `docs/design/screens/shell/README.md` — shell anatomy, environment chip, states
- `docs/design/ux/navigation/README.md` — routes and context rules
- `docs/design/components/system/README.md` — Skeleton / EmptyState / InlineNotice / ErrorPanel / PageBanner / SystemStatePage
- `docs/design/tokens/axiom.css` — tokens (copied verbatim into `src/styles/tokens.css`)
- `docs/architecture/api-contract.md` — §2 auth, §18 errors, §19 statuses, §21 pagination

## Requirements

- Node 18+ (developed on Node 24) and npm.

## Setup

```bash
cd apps/cloud
npm install
cp .env.example .env      # optional; defaults to http://localhost:8080
npm run dev               # http://localhost:5173
```

### Environment variables

| Variable | Default | Purpose |
|---|---|---|
| `VITE_AXIOM_API_URL` | `http://localhost:8080` | Engine base URL. The client appends `/api/v1` itself. No trailing slash. |

No secret is ever read from the environment by the browser bundle.

## Scripts

| Script | Description |
|---|---|
| `npm run dev` | Vite dev server |
| `npm run build` | `tsc --noEmit` typecheck + `vite build` |
| `npm run typecheck` | TypeScript only |
| `npm run test` | Vitest (route guard, error parsing, SSE resume, route context) |
| `npm run preview` | Serve the production build |

## Interim authentication (IMPORTANT)

There is **no identity provider yet** (#125). The Engine accepts a single bearer
token configured as `AXIOM_API_TOKEN` (api-contract §2). The console therefore:

- shows a token field on `/login` and stores the entered token in
  **`sessionStorage` under the key `axiom.token`** (dies with the tab; never
  `localStorage`, never the URL, never logs);
- sends it as `Authorization: Bearer <token>` on every API request;
- calls `GET /api/v1/auth/me` to validate it when reachable (a network/503
  failure keeps the token optimistically);
- on any `401`, clears the token and redirects to `/login` with a return path;
- `Sign out` clears the token and best-effort `POST /api/v1/auth/logout`.

When #125 lands, replace `src/auth/session.ts` and `src/auth/AuthContext.tsx`;
nothing else reads the storage key directly.

## What is implemented (#119)

### Routing (`src/routes/builders.ts`, `src/app/App.tsx`)

Task-lane routes: `/login`, `/dashboard`, `/github`, `/repositories`,
`/repositories/:id`, `/applications/:id`, `/deployments/:id`, `/servers`,
`/servers/:id`, `/settings` (+ `/settings/:section`), `/domains`, and a
`NotFound` fallback for unknown paths.

Canonical navigation routes are also registered as aliases so #120 can deep-link:
`/` → `/dashboard`, `/apps/:applicationId` → `/applications/:applicationId`,
`/apps/:id/setup/:step`, `/apps/:id/:environment/:section`,
`/apps/:id/:environment/deployments/:depId[/:tab]`.

Path building is centralized in `routes` (`src/routes/builders.ts`); no
component concatenates paths. `parseRoute` (`src/routes/context.ts`) derives the
shell variant, active sidebar item and context from the URL.

### Shell (`src/components/shell/`)

`AppShell` (skip link, focus management, document title, offline banner),
`Sidebar` (Workspace / Application / Infrastructure groups), `ContextBar`
(display-only workspace, application switcher placeholder, environment chip),
`PageHeader` + `Breadcrumbs`, `ActivityDrawer` (placeholder) and `ToastRegion`.

Responsive behavior is CSS-only:

- **≥ 1280px** — sidebar 240px (user can collapse to a 64px rail).
- **1024–1279px** — rail 64px, expands as an overlay on hover/focus.
- **≤ 1023px** — sidebar becomes a modal drawer via the context-bar menu button.
- **< 768px** — content padding and header actions collapse.

The **environment chip** uses text + glyph + border only (`■ Production`,
`◧ Staging`, `□ Preview`) with neutral tokens — never status colors
(screens/shell §4.1).

### API client (`src/api/`)

- `client.ts` — `VITE_AXIOM_API_URL` base URL, `/api/v1` prefix, generated
  `X-Request-ID`, `Authorization: Bearer` from session storage, error-envelope
  parsing into typed `ApiError`, and a global 401 hook.
- `errors.ts` — `ApiError` with `code`/`status`/`requestId`/`details` and typed
  predicates (`isUnauthorized`, `isForbidden`, `isNotFound`, `isUnavailable`, …).
- `pagination.ts` — `pageQuery`, `normalizePage`, `hasNextPage`, `nextPage`.
- `sse.ts` — `SseStream` over `EventSource` with `?lastEventId=` resume fallback,
  exponential reconnect backoff, and ordered delivery via `OrderedDelivery`.
- `resources.ts` — typed fetchers for the endpoints that exist today
  (`/applications`, `/github/connections`, repositories, `/servers`, `/auth/*`).

### System states (`src/components/system/`)

`Skeleton`, `EmptyState`, `InlineNotice`, `ErrorPanel`, `PageBanner`,
`SystemStatePage` (404/403/500/503), `FullPageLoading`, `ToastRegion`.

## Verification

Automated (Vitest, jsdom):

```bash
npm run test
npm run build
```

Covered: route guard redirect + return-path capture, `ApiError` envelope
parsing/classification, SSE `?lastEventId=` resume URL + backoff + ordered
delivery, route-context parsing (including unknown environment → Not Found) and
return-path sanitization.

Manual smoke test (needs a running Engine, or observe the handled 503/network
states):

1. `npm run dev`, open `/dashboard` while signed out → redirected to `/login`.
2. Paste a token → land on `/dashboard`; reload → session persists for the tab.
3. `Dashboard` lists applications (`GET /applications`); empty → first-use state.
4. `GitHub` shows connections; with no GitHub configured, `Connect` surfaces the
   `SERVICE_UNAVAILABLE` (503) notice.
5. Resize to 1100px (rail), 900px (drawer), 600px (mobile) and confirm the
   environment chip stays visible on application/deployment routes.
6. Stop the Engine and reload an application route → `ErrorPanel` with Retry;
   toggle the browser offline → offline `PageBanner`.

## Notes for #120

Ready to consume:

- `routes.*` builders and `parseRoute` context (URL is the single source of truth).
- `apiFetch`/`api` client with request IDs, auth and typed errors.
- `useAsync` (abortable load/reload) and `useOnline` hooks.
- `SseStream` for `GET /deployments/:id/events/stream`.
- System components + `ToastProvider`/`useToast`.
- Shell slots for the application switcher, environment menu, activity feed and
  real page headers.

Not yet wired (intentionally): real screens, application switcher data, environment
menu navigation, workspace event stream, settings sections, domain lists. The
environment field on deployments/applications is a **blocking contract gap**
(#71 / #117) — env-dependent UI is rendered only from the URL until it lands.

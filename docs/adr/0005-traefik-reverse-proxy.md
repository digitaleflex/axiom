# ADR-0005 — Traefik as initial reverse proxy

- **Status:** Accepted
- **Date:** 2026-10-07
- **Issue:** #64, #84

## Context

Deployed applications need hostname routing and automatic TLS on the server.

## Decision

Use Traefik on each server, configured by the Runtime Agent's network adapter, with ACME (Let's Encrypt) for certificates. Traefik and ACME remain implementation details behind the Engine API.

## Consequences

- Dynamic routing per container without restarts.
- Console shows "Routing"/"Certificate" states, not Traefik internals.
- Replacing the proxy later only affects the Agent adapter.

## Alternatives considered

Nginx (static config reloads), Caddy (viable; Traefik chosen for Docker-label/dynamic provider integration).

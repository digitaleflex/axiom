# ADR-0007 — Explicit contracts between experts and the Engine

- **Status:** Accepted
- **Date:** 2026-10-07
- **Issue:** #30, #32

## Context

Bounded experts (analyzer, planner, …) must not grow into an unbounded software factory, and their outputs must be verifiable.

## Decision

Each expert has a machine-readable contract (identity, capabilities, inputs, outputs, tools, forbidden responsibilities, quality gates, escalation). Experts exchange versioned artifacts validated by the Engine; invalid artifacts never satisfy downstream stages. Experts are invoked through an Engine interface independent of any model provider.

## Consequences

- Deterministic code can implement experts in V0.1; model-based implementations remain swappable.
- Contract and artifact schemas live in `schemas/**`.

## Alternatives considered

Implicit function calls without contracts — rejected (untestable authority boundaries).

# AGENTS.md

# Axiom Engineering AI Guide

> This document defines how AI coding assistants should work within the Axiom project.

---

# Project Overview

Axiom is a Platform Engineering Platform.

Its mission is to simplify infrastructure management by providing a unified platform for deployment, automation, monitoring, networking and operations.

The project is composed of four major components:

- Axiom Engine
- Axiom Agent
- Axiom Core
- Axiom Cloud

The Engine is the source of truth.

Every component communicates with the Engine.

---

# Engineering Philosophy

Always prefer:

- Simplicity
- Readability
- Explicit code
- Small reusable packages
- Loose coupling
- High cohesion

Avoid:

- Clever code
- Hidden magic
- Unnecessary abstractions
- Over-engineering
- Premature optimization

---

# Project Principles

API First

Everything should be accessible through the Engine API.

---

Documentation First

Every important architectural decision must have an ADR.

---

Security by Design

Never ignore security.

Authentication, authorization and auditability are mandatory.

---

Self Hosted First

Axiom is designed to run on user-owned infrastructure.

Cloud services must remain optional.

---

Plugin Architecture

Whenever possible, prefer plugins over modifying the Engine.

---

# Technology Stack

Backend

- Go

Frontend

- Next.js
- React
- TypeScript

Database

- PostgreSQL

Cache

- Redis

Messaging

- NATS

Reverse Proxy

- Traefik

Containers

- Docker Compose

Observability

- OpenTelemetry
- Prometheus
- Grafana
- Loki

---

# Repository Structure

apps/
    core/
    cloud/

services/
    engine/

agents/
    agent/

packages/

sdk/

cli/

infrastructure/

docs/

scripts/

---

# Coding Guidelines

Always:

- Write clean code.
- Prefer composition over inheritance.
- Keep functions small.
- Keep files focused.
- Prefer interfaces.
- Return explicit errors.
- Write deterministic code.
- Avoid global state.

---

# Go Guidelines

Prefer:

- context.Context
- structured logging
- dependency injection
- interfaces only when useful

Avoid:

- giant packages
- cyclic dependencies
- reflection unless required
- unnecessary goroutines

---

# API Guidelines

REST is the primary API.

Use:

GET

POST

PUT

PATCH

DELETE

Return proper HTTP status codes.

Always return JSON.

---

# Database

PostgreSQL is the source of truth.

Never bypass the repository layer.

Always use migrations.

Never write raw SQL inside handlers.

---

# Frontend

Core and Cloud share components whenever possible.

Business logic belongs in the Engine.

The frontend should remain thin.

---

# Agent

The Agent is responsible for local server operations.

Responsibilities include:

- Docker
- Docker Compose
- Logs
- Metrics
- Backups
- Health Checks
- Deployments

The Agent must never contain business logic.

---

# Logging

Use structured logs.

Never log:

- passwords
- tokens
- secrets
- private keys

---

# Security

Never disable authentication.

Never expose internal endpoints.

Validate every request.

Follow the principle of least privilege.

---

# Testing

Prefer:

- unit tests
- integration tests

Critical infrastructure code must be tested.

---

# Documentation

Whenever an architectural decision changes:

1. Update the ADR.
2. Update the documentation.
3. Update the implementation.

Never let documentation diverge from code.

---

# AI Assistant Rules

When generating code:

- Respect the existing architecture.
- Never introduce unnecessary dependencies.
- Prefer standard libraries.
- Explain trade-offs when proposing alternatives.
- Keep code production-ready.
- Never rewrite large parts of the project unless explicitly requested.
- Preserve backward compatibility whenever possible.

When uncertain:

Stop and ask for clarification instead of making assumptions.

---

# Definition of Done

A task is complete only if:

- Code compiles.
- Tests pass.
- Documentation is updated.
- Errors are handled.
- Logs are meaningful.
- Security has been considered.
- The implementation follows the project architecture.

---

# Final Rule

Every line of code should make Axiom simpler, more maintainable and easier to evolve.

Long-term maintainability is always more important than short-term convenience.s
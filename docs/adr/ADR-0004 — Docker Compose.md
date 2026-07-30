---
title: ADR-0004 - Use Docker Compose Before Kubernetes
status: Accepted
date: 2026-07-28
deciders:
  - Eurin d'ALMEIDA
technical-story: Foundation
tags:
  - docker
  - compose
  - kubernetes
---

# ADR-0004 — Use Docker Compose Before Kubernetes

## Status

Accepted

---

# Context

Le MVP d'Axiom doit être livré rapidement.

La majorité des utilisateurs ciblés utilisent aujourd'hui Docker Compose.

---

# Decision

Docker Compose constitue la plateforme d'orchestration officielle du MVP.

Le support Kubernetes sera développé ultérieurement sous forme de driver.

---

# Rationale

Docker Compose :

- est simple ;
- est largement adopté ;
- nécessite peu de configuration ;
- permet un développement rapide.

---

# Alternatives Considered

## Kubernetes

Très puissant mais plus complexe.

Décision : reporté après la V1.

---

# Consequences

## Positives

- Développement plus rapide.
- Installation simplifiée.
- Adoption facilitée.

## Negatives

- Fonctionnalités limitées par rapport à Kubernetes.

---

# Future Impact

L'architecture devra permettre l'ajout futur d'un driver Kubernetes sans modifier le cœur d'Axiom.
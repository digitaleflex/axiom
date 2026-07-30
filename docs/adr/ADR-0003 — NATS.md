---
title: ADR-0003 - Use NATS as Event Bus
status: Accepted
date: 2026-07-28
deciders:
  - Eurin d'ALMEIDA
technical-story: Foundation
tags:
  - messaging
  - nats
---

# ADR-0003 — Use NATS as Event Bus

## Status

Accepted

---

# Context

Axiom repose sur une architecture orientée événements.

Les composants devront communiquer de manière asynchrone :

- Engine
- Agent
- Workers
- Scheduler
- Monitoring

---

# Decision

NATS est retenu comme Event Bus principal.

---

# Rationale

NATS est :

- léger ;
- rapide ;
- simple à administrer ;
- adapté aux communications distribuées ;
- largement utilisé dans les plateformes cloud-native.

---

# Alternatives Considered

## RabbitMQ

Très complet mais plus complexe.

Décision : non retenu.

---

## Kafka

Très puissant mais surdimensionné pour les besoins du MVP.

Décision : non retenu.

---

# Consequences

## Positives

- Faible latence.
- Simplicité.
- Excellente montée en charge.

## Negatives

- Écosystème plus réduit que Kafka.

---

# Future Impact

Toutes les communications événementielles utiliseront NATS comme bus principal.
---
title: ADR-0002 - Use PostgreSQL as Primary Database
status: Accepted
date: 2026-07-28
deciders:
  - Eurin d'ALMEIDA
technical-story: Foundation
tags:
  - database
  - postgresql
---

# ADR-0002 — Use PostgreSQL as Primary Database

## Status

Accepted

---

# Context

Axiom gère des utilisateurs, organisations, projets, serveurs, déploiements, secrets, permissions, journaux d'audit et événements.

La base de données doit garantir :

- la cohérence des données ;
- les transactions ;
- la scalabilité ;
- la fiabilité.

---

# Decision

PostgreSQL est retenu comme base de données principale d'Axiom.

---

# Rationale

PostgreSQL offre :

- un excellent support transactionnel (ACID) ;
- des performances éprouvées ;
- JSONB pour les données semi-structurées ;
- des index avancés ;
- un vaste écosystème ;
- une compatibilité avec les principaux outils d'observabilité et de sauvegarde.

---

# Alternatives Considered

## MySQL

Bon support mais moins riche fonctionnellement.

Décision : non retenu.

---

## MongoDB

Très flexible mais moins adapté aux relations complexes et aux transactions métier.

Décision : non retenu.

---

# Consequences

## Positives

- Cohérence des données.
- Excellentes performances.
- Standard de l'industrie.

## Negatives

- Schéma plus strict.
- Migrations nécessaires.

---

# Future Impact

Toutes les données métier d'Axiom seront stockées dans PostgreSQL.

Cette décision constitue la source de vérité (Source of Truth) de la plateforme.
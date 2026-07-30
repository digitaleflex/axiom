---
title: ADR-0001 - Use Go for Backend Services
status: Accepted
date: 2026-07-28
deciders:
  - Eurin d'ALMEIDA
technical-story: Foundation
tags:
  - backend
  - golang
  - architecture
---

# ADR-0001 — Use Go for Backend Services

## Status

Accepted

---

# Context

Axiom est une plateforme de Platform Engineering destinée à administrer des infrastructures modernes.

Le backend constitue le cœur de la plateforme. Il est responsable de :

- l'API centrale ;
- l'orchestration ;
- la communication avec les Agents ;
- la gestion des déploiements ;
- les tâches planifiées ;
- les événements ;
- les permissions.

Ce composant doit être performant, fiable et simple à déployer.

Le choix du langage est donc une décision structurante.

---

# Decision

Tous les services backend d'Axiom seront développés en **Go**.

Cela concerne notamment :

- Axiom Engine
- Axiom Agent
- Axiom CLI
- Les futurs microservices internes

---

# Rationale

Go a été retenu pour plusieurs raisons.

## Simplicité

Le langage possède une syntaxe concise et une bibliothèque standard très complète.

---

## Performance

Go offre d'excellentes performances pour :

- les API HTTP ;
- les traitements concurrents ;
- les communications réseau.

---

## Concurrence native

Les goroutines permettent de gérer efficacement un grand nombre de tâches simultanées.

Cette caractéristique est particulièrement adaptée :

- aux communications avec les Agents ;
- aux opérations de déploiement ;
- aux tâches planifiées.

---

## Déploiement

Go produit un binaire unique.

Cela simplifie :

- les installations ;
- les mises à jour ;
- les déploiements.

Aucune machine virtuelle ni runtime externe n'est nécessaire.

---

## Portabilité

Le même code peut être compilé pour :

- Linux
- Windows
- macOS

Cette caractéristique est indispensable pour Axiom Agent.

---

## Écosystème

Go dispose d'un excellent écosystème pour :

- Docker
- Kubernetes
- HTTP
- gRPC
- PostgreSQL
- Observabilité

De nombreux outils de Platform Engineering sont déjà développés en Go.

---

# Alternatives Considered

## Node.js

### Avantages

- excellente productivité ;
- vaste écosystème.

### Inconvénients

- dépendance au runtime Node.js ;
- consommation mémoire plus importante ;
- moins adapté aux services système.

Décision : non retenu.

---

## Rust

### Avantages

- performances exceptionnelles ;
- sécurité mémoire.

### Inconvénients

- courbe d'apprentissage élevée ;
- vitesse de développement plus faible.

Décision : non retenu.

---

## Java

### Avantages

- très mature ;
- excellent pour les grandes entreprises.

### Inconvénients

- runtime JVM ;
- consommation mémoire importante.

Décision : non retenu.

---

# Consequences

## Positives

- Exécutables autonomes.
- Faible consommation mémoire.
- Déploiements simplifiés.
- Très bonnes performances.
- Excellent support de la concurrence.

## Negatives

- Écosystème frontend limité.
- Temps de compilation supérieur à Node.js.
- Moins de développeurs disponibles que JavaScript.

---

# Future Impact

Ce choix influence directement :

- Axiom Engine
- Axiom Agent
- Axiom CLI
- Le SDK Go

Les interfaces utilisateur continueront d'utiliser TypeScript et React.

---

# Review

Cette décision pourra être réévaluée uniquement si une évolution majeure de l'architecture le justifie.

En l'absence d'un bénéfice significatif, Go reste le langage officiel pour tous les services backend d'Axiom.
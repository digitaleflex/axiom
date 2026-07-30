---
title: RFC-0001 - Vision of Axiom
status: Accepted
version: 1.0
authors:
  - Eurin d'ALMEIDA
created: 2026-07-28
updated: 2026-07-28
---

# RFC-0001 — Vision of Axiom

## Status

Accepted

---

# Purpose

Ce document définit la vision fondatrice d'Axiom.

Toutes les décisions techniques, architecturales et produit doivent rester cohérentes avec cette vision.

---

# Why Axiom Exists

Les infrastructures modernes sont devenues extrêmement puissantes, mais également extrêmement complexes.

Déployer une application implique aujourd'hui de maîtriser plusieurs technologies :

- Git
- Docker
- Docker Compose
- Reverse Proxy
- DNS
- SSL
- CI/CD
- Monitoring
- Logs
- Sauvegardes
- Sécurité

Chaque outil résout une partie du problème.

Aucun ne fournit une plateforme unique permettant d'administrer l'ensemble de l'infrastructure de manière cohérente.

Axiom est né pour résoudre cette complexité.

---

# Vision

> Build the Operating System for Modern Infrastructure.

Notre ambition est de construire une plateforme permettant aux développeurs et aux organisations de gérer leur infrastructure aussi simplement qu'ils gèrent leur code.

---

# Mission

Axiom fournit une plateforme unique permettant de :

- déployer des applications ;
- gérer des serveurs ;
- superviser les infrastructures ;
- automatiser les opérations ;
- sécuriser les déploiements ;
- centraliser l'administration.

---

# Target Users

Axiom s'adresse principalement à :

- Développeurs indépendants
- Startups
- Agences web
- Équipes DevOps
- PME
- Entreprises
- Hébergeurs

---

# Product Vision

Axiom est composé de plusieurs produits complémentaires.

```text
                    AXIOM PLATFORM

                         │

        ┌────────────────┼────────────────┐

        ▼                ▼                ▼

    AXIOM CORE      AXIOM CLOUD     AXIOM AGENT

                         │

                         ▼

                  AXIOM ENGINE
```

## Axiom Engine

Le cœur de la plateforme.

Responsable de :

- l'API
- l'orchestration
- la logique métier
- les événements
- les permissions
- les déploiements

---

## Axiom Agent

Service installé sur chaque serveur.

Responsable de :

- Docker
- Docker Compose
- Logs
- Monitoring
- Backups
- Health Checks
- Traefik
- Exécution des opérations

---

## Axiom Core

Interface d'administration destinée aux administrateurs de la plateforme.

---

## Axiom Cloud

Portail destiné aux utilisateurs et aux clients.

---

# Core Principles

Les principes suivants guideront toutes les décisions du projet.

- API First
- Security by Design
- Automation First
- Documentation First
- Self-Hosted First
- Open Standards
- Extensibility
- Simplicity

---

# What Axiom Is

Axiom est :

- une Platform Engineering Platform ;
- une plateforme Self-Hosted ;
- un orchestrateur d'infrastructure ;
- un moteur d'automatisation ;
- une API unifiée.

---

# What Axiom Is Not

Axiom n'est pas :

- un fournisseur Cloud ;
- un remplacement de Docker ;
- un remplacement de Kubernetes ;
- un remplacement de PostgreSQL ;
- un hébergeur ;
- une distribution Linux.

Axiom s'intègre avec ces technologies au lieu de les remplacer.

---

# MVP Goals

Le MVP doit permettre de :

- connecter un serveur ;
- installer un Agent ;
- déployer une application Docker Compose ;
- consulter les logs ;
- gérer les variables d'environnement ;
- gérer les domaines ;
- gérer les certificats SSL.

Tout le reste est hors périmètre du MVP.

---

# Long-Term Vision

À long terme, Axiom devra devenir une plateforme complète de Platform Engineering intégrant :

- Monitoring
- Backups
- Registry
- Secrets
- CLI
- SDK
- Marketplace
- AI
- Plugins

sans remettre en cause les fondations définies dans ce document.

---

# Success Criteria

Nous considérerons la vision réussie lorsque :

- une infrastructure complète pourra être administrée depuis Axiom ;
- les opérations courantes ne nécessiteront plus de connexion SSH manuelle ;
- les déploiements seront reproductibles ;
- la plateforme restera extensible grâce à son architecture modulaire.

---

# Final Statement

Axiom n'est pas un simple outil de déploiement.

Axiom est une plateforme de Platform Engineering conçue pour devenir le système d'exploitation des infrastructures modernes.
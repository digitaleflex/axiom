---
title: ADR-0005 - Use Traefik as Reverse Proxy
status: Accepted
date: 2026-07-28
deciders:
  - Eurin d'ALMEIDA
technical-story: Foundation
tags:
  - traefik
  - networking
---

# ADR-0005 — Use Traefik as Reverse Proxy

## Status

Accepted

---

# Context

Axiom doit gérer automatiquement :

- les domaines ;
- les certificats SSL ;
- le routage HTTP ;
- les applications Docker.

---

# Decision

Traefik est retenu comme reverse proxy officiel.

---

# Rationale

Traefik offre :

- une excellente intégration avec Docker ;
- la découverte automatique des services ;
- la gestion native de Let's Encrypt ;
- une configuration dynamique ;
- une API moderne.

---

# Alternatives Considered

## Nginx

Très performant mais plus orienté configuration manuelle.

Décision : non retenu.

---

## Caddy

Simple à utiliser mais moins flexible pour les besoins d'Axiom.

Décision : non retenu.

---

# Consequences

## Positives

- Déploiement simplifié.
- SSL automatisé.
- Intégration native avec Docker.

## Negatives

- Dépendance à l'écosystème Traefik.

---

# Future Impact

Toutes les fonctionnalités réseau d'Axiom devront être conçues autour de Traefik tout en conservant la possibilité d'ajouter d'autres reverse proxies à travers un système de drivers.
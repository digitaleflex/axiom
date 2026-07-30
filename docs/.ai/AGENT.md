# Instructions pour Axiom Agent

L'Agent est responsable des opérations locales sur les serveurs et environnements d'exécution.

## Responsabilités
- Gérer Docker et Docker Compose.
- Superviser les logs, métriques et health checks.
- Assurer les déploiements et sauvegardes.

## Règles
- Ne jamais embarquer de logique métier dans l'Agent.
- Limiter l'Agent à l'orchestration locale et à l'opérationnel.
- Protéger les secrets et les accès sensibles.

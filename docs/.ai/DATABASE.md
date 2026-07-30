# PostgreSQL

PostgreSQL est la base de données principale du système.

## Règles
- Considérer PostgreSQL comme la source de vérité persistante.
- Utiliser des migrations pour toute évolution du schéma.
- Éviter les accès SQL directs dans les handlers.
- Préférer la couche repository pour l'accès aux données.

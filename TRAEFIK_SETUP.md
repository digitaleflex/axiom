# Axiom + Traefik + Domain Setup Guide

## 🎯 Objectif
Connecter `axiom.eurinhash.com` (et `api.axiom.eurinhash.com`) au stack Axiom via Traefik avec SSL Let's Encrypt automatique.

---

## 📋 Prérequis

| Élément | Valeur |
|---------|--------|
| **Domaine** | `axiom.eurinhash.com` |
| **DNS A record** | Doit pointer vers **l'IP publique de votre serveur** |
| **Ports ouverts** | 80 (HTTP), 443 (HTTPS), 8080 (Engine API), 8081 (Traefik dashboard) |
| **Email Let's Encrypt** | `admin@eurinhash.com` (à changer dans docker-compose.dev.yml) |

### Vérifier le DNS
```bash
# Doit retourner VOTRE IP serveur (pas Cloudflare)
dig +short axiom.eurinhash.com A
```

---

## 🚀 Démarrage Rapide

### 1. Démarrer le stack
```bash
cd /home/audest/axiom/services/engine

# Construire les images
docker compose -f docker-compose.dev.yml build

# Démarrer
docker compose -f docker-compose.dev.yml up -d

# Voir les logs
docker compose -f docker-compose.dev.yml logs -f
```

### 2. Vérifier les services
| Service | URL | Description |
|---------|-----|-------------|
| **Engine API** | https://api.axiom.eurinhash.com | API REST Axiom |
| **Cloud Console** | https://axiom.eurinhash.com | Interface web (à déployer séparément) |
| **Traefik Dashboard** | http://localhost:8081 | Dashboard Traefik (dev only) |
| **PostgreSQL** | localhost:5432 | DB (user: axiom, pass: axiom) |

### 3. Tester l'API
```bash
# Health check
curl https://api.axiom.eurinhash.com/api/v1/health

# Auth (avec token .env)
curl -H "Authorization: Bearer dev-token-12345" \
  https://api.axiom.eurinhash.com/api/v1/auth/me
```

---

## 🔧 Configuration Traefik

### Fichiers importants
```
services/engine/
├── docker-compose.dev.yml          # Stack complet
├── Dockerfile                      # Engine image
├── traefik/
│   └── dynamic/
│       └── axiom-console.yml       # Config statique (console + API)
└── .env                            # Variables d'environnement
```

### Certificats Let's Encrypt
- Stockés dans volume `traefik-letsencrypt` → `/letsencrypt/acme.json`
- Renouvellement automatique par Traefik
- Challenge HTTP-01 sur port 80

### Config dynamique (gérée par l'Agent)
L'Agent écrit dans `/etc/traefik/dynamic/` (partagé via volume) :
- `axiom-<deploymentID>.yml` par déploiement
- L'agent gère : routage, TLS, redirection HTTP→HTTPS

---

## 🌐 Déployer une Application (GitHub → LIVE)

### Via API
```bash
# 1. Connecter GitHub (si configuré)
curl -X POST -H "Authorization: Bearer dev-token-12345" \
  https://api.axiom.eurinhash.com/api/v1/github/connections \
  -d '{"clientId": "...", "clientSecret": "..."}'

# 2. Lister repos
curl -H "Authorization: Bearer dev-token-12345" \
  https://api.axiom.eurinhash.com/api/v1/github/connections/ghc_xxx/repositories

# 3. Créer application
curl -X POST -H "Authorization: Bearer dev-token-12345" \
  https://api.axiom.eurinhash.com/api/v1/applications \
  -d '{"repositoryId": "repo_xxx", "name": "mon-app"}'

# 4. Analyser
curl -X POST -H "Authorization: Bearer dev-token-12345" \
  https://api.axiom.eurinhash.com/api/v1/applications/app_xxx/analysis \
  -d '{"ref": "main"}'

# 5. Voir profil détecté
curl -H "Authorization: Bearer dev-token-12345" \
  https://api.axiom.eurinhash.com/api/v1/applications/app_xxx/profile

# 6. Créer plan de déploiement
curl -X POST -H "Authorization: Bearer dev-token-12345" \
  https://api.axiom.eurinhash.com/api/v1/applications/app_xxx/deployment-plans \
  -d '{"serverId": "srv_local", "environment": "production", "domain": "mon-app.eurinhash.com"}'

# 7. Déployer
curl -X POST -H "Authorization: Bearer dev-token-12345" \
  https://api.axiom.eurinhash.com/api/v1/applications/app_xxx/deployments \
  -d '{"planId": "plan_xxx"}'

# 8. Suivre progression (SSE)
curl -H "Authorization: Bearer dev-token-12345" \
  https://api.axiom.eurinhash.com/api/v1/deployments/dep_xxx/events/stream
```

### Résultat
- L'application sera accessible à `https://mon-app.eurinhash.com`
- Traefik gère automatiquement : SSL, routage, health checks
- L'agent gère : Docker, logs, recovery

---

## 🛠️ Commandes Utiles

### Logs
```bash
# Tous les services
docker compose -f docker-compose.dev.yml logs -f

# Service spécifique
docker compose -f docker-compose.dev.yml logs -f engine
docker compose -f docker-compose.dev.yml logs -f agent
docker compose -f docker-compose.dev.yml logs -f traefik
```

### Redémarrer
```bash
docker compose -f docker-compose.dev.yml restart engine
docker compose -f docker-compose.dev.yml restart agent
```

### Shell dans conteneur
```bash
docker exec -it axiom-engine sh
docker exec -it axiom-agent sh
docker exec -it axiom-traefik sh
```

### Certificats Let's Encrypt
```bash
# Voir les certificats
docker exec axiom-traefik cat /letsencrypt/acme.json | jq '.le.Certificates[] | {domain: .domain.main, expiry: .certificate.expires}'

# Forcer renouvellement (supprimer acme.json et restart traefik)
docker compose -f docker-compose.dev.yml stop traefik
docker volume rm axiom-engine_traefik-letsencrypt
docker compose -f docker-compose.dev.yml up -d traefik
```

---

## ⚠️ Points d'Attention

| Sujet | Note |
|-------|------|
| **DNS** | Doit pointer vers l'IP du serveur (pas Cloudflare proxy orange) pour Let's Encrypt HTTP-01 |
| **Email Let's Encrypt** | Changez `admin@eurinhash.com` dans docker-compose.dev.yml |
| **Traefik Dashboard** | Accessible sur port 8081 (localhost seulement), `--api.insecure=true` = dev only |
| **Agent Data** | Persisté dans volume `agent-data` → `/data` |
| **Dynamic Config** | Partagé via `./traefik/dynamic:/etc/traefik/dynamic` (Engine host ↔ Traefik ↔ Agent) |
| **Docker Socket** | Monté pour builds (Engine) et runtime (Agent) - **risque sécurité en prod** |

---

## 🔐 Sécurité Production (TODO)

- [ ] Désactiver `--api.insecure=true`
- [ ] Protéger dashboard Traefik par auth (basic auth ou OAuth)
- [ ] Utiliser Docker socket proxy (Technology: `tecnativa/docker-socket-proxy`)
- [ ] Séparer réseaux : `traefik-public`, `axiom-internal`
- [ ] Configurer rate limiting, WAF basique
- [ ] Monitoring : Prometheus + Grafana (Traefik expose `/metrics`)

---

## 📁 Structure des Volumes

```
axiom-postgres-data     → /var/lib/postgresql/data  (PostgreSQL)
traefik-letsencrypt     → /letsencrypt              (ACME certificates)
agent-data              → /data                     (Agent identity, credentials, state)
./traefik/dynamic       → /etc/traefik/dynamic      (Dynamic config, shared)
```

---

## 🎯 Prochaines Étapes

1. **Configurer DNS** : `axiom.eurinhash.com` A → votre IP serveur
2. **Lancer stack** : `docker compose -f docker-compose.dev.yml up -d`
3. **Attendre certificats** : ~30-60s pour Let's Encrypt
4. **Tester API** : `curl https://api.axiom.eurinhash.com/api/v1/health`
5. **Déployer Cloud Console** : Build React app → servir via Traefik
6. **Premier déploiement** : Suivre guide "Déployer une Application" ci-dessus
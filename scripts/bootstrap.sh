#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"

echo "=== Axiom Bootstrap ==="
echo ""

# --- Directories ---
echo "Creating directory structure..."

mkdir -p "$ROOT_DIR/.github"
mkdir -p "$ROOT_DIR/.vscode"

# Apps
mkdir -p "$ROOT_DIR/apps/core"
mkdir -p "$ROOT_DIR/apps/cloud"

# Services
mkdir -p "$ROOT_DIR/services/engine"

# Agents
mkdir -p "$ROOT_DIR/agents/agent"

# CLI
mkdir -p "$ROOT_DIR/cli"

# SDKs
mkdir -p "$ROOT_DIR/sdk/go"
mkdir -p "$ROOT_DIR/sdk/ts"
mkdir -p "$ROOT_DIR/sdk/python"

# Drivers
mkdir -p "$ROOT_DIR/drivers"

# Packages
mkdir -p "$ROOT_DIR/packages"

# Infrastructure
mkdir -p "$ROOT_DIR/infrastructure/docker"
mkdir -p "$ROOT_DIR/infrastructure/compose"
mkdir -p "$ROOT_DIR/infrastructure/traefik"
mkdir -p "$ROOT_DIR/infrastructure/postgres"

# Others
mkdir -p "$ROOT_DIR/scripts"
mkdir -p "$ROOT_DIR/examples"
mkdir -p "$ROOT_DIR/tests"

echo "  ✓ Directories created"

# --- Root files ---
echo "Creating root configuration files..."

# .gitignore
cat > "$ROOT_DIR/.gitignore" << 'GITIGNORE'
# Dependencies
node_modules/
.pnpm-store/

# Build output
dist/
.next/
out/
build/
*.exe
*.exe~
*.dll
*.so
*.dylib

# Go
vendor/
*.test
*.out
*.test.exe
go.work.sum

# Python
__pycache__/
*.pyc
*.pyo
*.egg-info/
.venv/
venv/

# Environment
.env
.env.local
.env.*.local

# IDE
.idea/
.vscode/*
!.vscode/settings.json
!.vscode/extensions.json
*.swp
*.swo
*~

# OS
.DS_Store
Thumbs.db
desktop.ini

# Logs
*.log
npm-debug.log*
yarn-debug.log*
yarn-error.log*

# Coverage
coverage/
.coverage
*.lcov

# Turbo
.turbo/

# Agent artifacts
.agent/
*.agent
*.skill
*.prompt
prompts/
*.instructions.md
*.agent.md
*.prompt.md

# Docker
.docker/
GITIGNORE

# .env.example
cat > "$ROOT_DIR/.env.example" << 'ENV'
# Axiom — Environment Variables
# Copy this file to .env and adjust values.

# Database
DATABASE_URL=postgres://axiom:axiom@localhost:5432/axiom?sslmode=disable

# Redis
REDIS_URL=redis://localhost:6379/0

# NATS
NATS_URL=nats://localhost:4222

# Engine
ENGINE_PORT=8080
ENGINE_LOG_LEVEL=info

# Agent
AGENT_PORT=8081

# Auth
JWT_SECRET=change-me-in-production
ENV

# .env.local
cp -n "$ROOT_DIR/.env.example" "$ROOT_DIR/.env" 2>/dev/null || true

echo "  ✓ Root files created"

# --- Go workspace ---
if command -v go &>/dev/null; then
  echo "Initializing Go workspace..."
  cd "$ROOT_DIR"

  # Create go.mod files if they don't exist
  for dir in services/engine agents/agent cli sdk/go; do
    if [ ! -f "$ROOT_DIR/$dir/go.mod" ]; then
      mkdir -p "$ROOT_DIR/$dir"
      cd "$ROOT_DIR/$dir"
      go mod init "github.com/axiom/axiom-paas/$dir"
      cd "$ROOT_DIR"
    fi
  done

  # Create go.work
  cat > "$ROOT_DIR/go.work" << 'GOWORK'
go 1.22

use (
  ./services/engine
  ./agents/agent
  ./cli
  ./sdk/go
)
GOWORK

  echo "  ✓ Go workspace initialized"
fi

# --- Node.js / pnpm ---
if command -v node &>/dev/null; then
  echo "Initializing Node.js workspace..."
  cd "$ROOT_DIR"

  cat > "$ROOT_DIR/package.json" << 'JSON'
{
  "name": "axiom-paas",
  "private": true,
  "version": "0.1.0",
  "description": "Axiom — Platform Engineering Platform",
  "scripts": {
    "dev": "turbo dev",
    "build": "turbo build",
    "lint": "turbo lint",
    "test": "turbo test",
    "format": "prettier --write \"**/*.{ts,tsx,js,jsx,json,md}\"",
    "clean": "turbo clean"
  },
  "devDependencies": {
    "prettier": "^3.2.0",
    "turbo": "^2.0.0"
  },
  "engines": {
    "node": ">=20.0.0",
    "pnpm": ">=9.0.0"
  },
  "packageManager": "pnpm@9.0.0"
}
JSON

  cat > "$ROOT_DIR/pnpm-workspace.yaml" << 'YAML'
packages:
  - "apps/*"
  - "packages/*"
  - "sdk/ts"
  - "cli"
YAML

  cat > "$ROOT_DIR/turbo.json" << 'TURBO'
{
  "$schema": "https://turbo.build/schema.json",
  "globalDependencies": ["**/.env.*local"],
  "pipeline": {
    "build": {
      "dependsOn": ["^build"],
      "outputs": [".next/**", "!.next/cache/**", "dist/**"]
    },
    "dev": {
      "cache": false,
      "persistent": true
    },
    "lint": {},
    "test": {},
    "clean": {
      "cache": false
    }
  }
}
TURBO

  if command -v pnpm &>/dev/null; then
    echo "  ✓ pnpm workspace configured"
  else
    echo "  ! pnpm not found. Install it with: npm install -g pnpm"
  fi
fi

# --- VSCode settings ---
cat > "$ROOT_DIR/.vscode/extensions.json" << 'VSCODE_EXT'
{
  "recommendations": [
    "golang.go",
    "dbaeumer.vscode-eslint",
    "esbenp.prettier-vscode",
    "bradlc.vscode-tailwindcss",
    "ms-vscode.makefile-tools",
    "ms-azuretools.vscode-docker"
  ]
}
VSCODE_EXT

cat > "$ROOT_DIR/.vscode/settings.json" << 'VSCODE_SET'
{
  "editor.formatOnSave": true,
  "editor.defaultFormatter": "esbenp.prettier-vscode",
  "editor.codeActionsOnSave": {
    "source.fixAll.eslint": "explicit"
  },
  "files.exclude": {
    "**/node_modules": true,
    "**/.next": true,
    "**/dist": true
  },
  "search.exclude": {
    "**/node_modules": true,
    "**/.next": true,
    "**/dist": true
  }
}
VSCODE_SET

echo ""
echo "=== Bootstrap complete ==="
echo ""
echo "Next steps:"
echo "  make docker-up    # Start PostgreSQL, Redis, NATS, Traefik"
echo "  make dev          # Start development servers"
echo "  make build        # Build all packages"
echo ""

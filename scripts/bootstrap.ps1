#!/usr/bin/env pwsh
param()

$ErrorActionPreference = "Stop"
$ROOT_DIR = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)

Write-Host "=== Axiom Bootstrap ===" -ForegroundColor Cyan
Write-Host ""

# --- Directories ---
Write-Host "Creating directory structure..."

$dirs = @(
    ".github",
    ".vscode",
    "apps/core",
    "apps/cloud",
    "services/engine",
    "agents/agent",
    "cli",
    "sdk/go",
    "sdk/ts",
    "sdk/python",
    "drivers",
    "packages",
    "infrastructure/docker",
    "infrastructure/compose",
    "infrastructure/traefik",
    "infrastructure/postgres",
    "scripts",
    "examples",
    "tests"
)

foreach ($dir in $dirs) {
    $path = Join-Path $ROOT_DIR $dir
    if (-not (Test-Path $path)) {
        New-Item -ItemType Directory -Path $path -Force | Out-Null
    }
}

Write-Host "  `u{2713} Directories created" -ForegroundColor Green

# --- .env ---
$envExample = Join-Path $ROOT_DIR ".env.example"
if (-not (Test-Path $envExample)) {
    @"
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
"@ | Set-Content -Path $envExample -Encoding UTF8

    Write-Host "  `u{2713} .env.example created" -ForegroundColor Green
}

$envLocal = Join-Path $ROOT_DIR ".env"
if (-not (Test-Path $envLocal)) {
    Copy-Item -Path $envExample -Destination $envLocal
    Write-Host "  `u{2713} .env created from .env.example" -ForegroundColor Green
}

# --- Go workspace ---
$goWorkspace = Join-Path $ROOT_DIR "go.work"
if (Get-Command "go" -ErrorAction SilentlyContinue) {
    Write-Host "Initializing Go workspace..."

    $goDirs = @("services/engine", "agents/agent", "cli", "sdk/go")
    foreach ($dir in $goDirs) {
        $goMod = Join-Path $ROOT_DIR "$dir/go.mod"
        if (-not (Test-Path $goMod)) {
            Push-Location (Join-Path $ROOT_DIR $dir)
            go mod init "github.com/axiom/axiom-paas/$dir"
            Pop-Location
        }
    }

    @"
go 1.22

use (
  ./services/engine
  ./agents/agent
  ./cli
  ./sdk/go
)
"@ | Set-Content -Path $goWorkspace -Encoding UTF8

    Write-Host "  `u{2713} Go workspace initialized" -ForegroundColor Green
}

# --- Node.js workspace ---
if (Get-Command "node" -ErrorAction SilentlyContinue) {
    Write-Host "Initializing Node.js workspace..."

    # pnpm workspace
    @"
packages:
  - "apps/*"
  - "packages/*"
  - "sdk/ts"
  - "cli"
"@ | Set-Content -Path (Join-Path $ROOT_DIR "pnpm-workspace.yaml") -Encoding UTF8

    if (Get-Command "pnpm" -ErrorAction SilentlyContinue) {
        Write-Host "  `u{2713} pnpm workspace configured" -ForegroundColor Green
    } else {
        Write-Host "  ! pnpm not found. Install it with: npm install -g pnpm" -ForegroundColor Yellow
    }
}

# --- VSCode ---
$vscodeExt = @"
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
"@
Set-Content -Path (Join-Path $ROOT_DIR ".vscode/extensions.json") -Value $vscodeExt -Encoding UTF8

$vscodeSettings = @"
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
"@
Set-Content -Path (Join-Path $ROOT_DIR ".vscode/settings.json") -Value $vscodeSettings -Encoding UTF8

Write-Host ""
Write-Host "=== Bootstrap complete ===" -ForegroundColor Cyan
Write-Host ""
Write-Host "Next steps:"
Write-Host "  docker compose up -d   # Start PostgreSQL, Redis, NATS, Traefik"
Write-Host "  pnpm install           # Install JS dependencies"
Write-Host "  turbo dev              # Start development servers"
Write-Host ""

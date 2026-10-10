# Axiom Engine - Multi-stage build
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git make

# Copy go mod files first (for cache)
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /axiom-engine ./cmd/engine

# ──────────────────────────────────────────────────────────────
# Runtime stage
FROM alpine:3.20

# Install runtime dependencies
RUN apk add --no-cache \
    ca-certificates \
    docker-cli \
    git \
    tzdata

# Create non-root user
RUN addgroup -g 1000 axiom && \
    adduser -D -u 1000 -G axiom axiom

# Copy binary
COPY --from=builder /axiom-engine /usr/local/bin/axiom-engine

# Docker socket group (for Docker CLI access)
RUN addgroup -g 999 docker && adduser axiom docker

USER axiom

# Default command
ENTRYPOINT ["axiom-engine"]

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD wget -qO- http://localhost:8080/api/v1/health || exit 1
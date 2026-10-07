-- 009 — Agent registration and credential lifecycle (issues #76, #77).
-- Rules: never edit an applied migration; every change is a new numbered file.
-- Identifiers are opaque TEXT (API contract §3). Credentials and bootstrap
-- tokens are stored only as SHA-256 hashes; plaintext is returned exactly once.

-- 1. Agent identities, bound one-to-one to a server ---------------------------
CREATE TABLE IF NOT EXISTS agent_identities (
    agent_id              TEXT PRIMARY KEY,
    server_id             TEXT NOT NULL UNIQUE REFERENCES servers(id) ON DELETE CASCADE,
    agent_version         TEXT NOT NULL DEFAULT '',
    status                TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'revoked')),
    credential_hash       TEXT NOT NULL,
    credential_version    INTEGER NOT NULL DEFAULT 1 CHECK (credential_version > 0),
    credential_expires_at TIMESTAMPTZ NOT NULL,
    prev_credential_hash  TEXT,
    prev_expires_at       TIMESTAMPTZ,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at            TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_agent_identities_server_id ON agent_identities(server_id);
CREATE INDEX IF NOT EXISTS idx_agent_identities_status ON agent_identities(status);

-- 2. Single-use bootstrap tokens bound to a server -----------------------------
CREATE TABLE IF NOT EXISTS agent_bootstrap_tokens (
    token_hash  TEXT PRIMARY KEY,
    server_id   TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    expires_at  TIMESTAMPTZ NOT NULL,
    used_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_agent_bootstrap_tokens_server_id ON agent_bootstrap_tokens(server_id);
CREATE INDEX IF NOT EXISTS idx_agent_bootstrap_tokens_expires_at ON agent_bootstrap_tokens(expires_at);

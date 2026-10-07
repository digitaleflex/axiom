-- 004 — GitHub connection boundary (issue #91) and repository identity (#92).
-- Tokens are stored only as AES-GCM ciphertexts bound to the connection ID.

ALTER TABLE github_connections
    ADD COLUMN IF NOT EXISTS provider_user_id   TEXT,
    ADD COLUMN IF NOT EXISTS scopes             TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS token_ciphertext   BYTEA,
    ADD COLUMN IF NOT EXISTS refresh_ciphertext BYTEA,
    ADD COLUMN IF NOT EXISTS token_expires_at   TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS disconnected_at    TIMESTAMPTZ;

-- One connection row per (Axiom user, GitHub account); reconnecting updates it.
CREATE UNIQUE INDEX IF NOT EXISTS github_connections_user_provider_key
    ON github_connections (user_id, provider_user_id) WHERE provider_user_id IS NOT NULL;

-- A disconnected connection holds no credentials.
ALTER TABLE github_connections
    ADD CONSTRAINT github_connections_disconnected_no_tokens
    CHECK (status <> 'disconnected' OR (token_ciphertext IS NULL AND refresh_ciphertext IS NULL));

-- Single-use, short-lived OAuth states (CSRF protection), bound to the user
-- who started the flow and to the starting browser (cookie hash).
CREATE TABLE IF NOT EXISTS github_oauth_states (
    state_hash       TEXT PRIMARY KEY,
    user_id          TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    browser_hash     TEXT NOT NULL,
    verifier_sealed  BYTEA NOT NULL,
    expires_at       TIMESTAMPTZ NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_github_oauth_states_expires_at ON github_oauth_states(expires_at);

-- Repository metadata refreshed from GitHub.
ALTER TABLE repositories
    ADD COLUMN IF NOT EXISTS html_url   TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS language   TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS pushed_at  TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

-- 011 — User authentication and session security (issue #125).
-- Rules: never edit an applied migration; every change is a new numbered file.
-- Identifiers are opaque TEXT (API contract §3). Session tokens are stored only
-- as SHA-256 hashes; plaintext tokens are returned exactly once and never logged.

-- 1. Password credentials on the existing users table (columns only) -----------
-- The users table and its unique lower(email) index already exist (001/003).
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS password_hash        TEXT,
    ADD COLUMN IF NOT EXISTS password_updated_at  TIMESTAMPTZ;

-- 2. Sessions ------------------------------------------------------------------
-- token_hash is the SHA-256 of the opaque session token; the plaintext token is
-- never persisted. csrf_token backs double-submit CSRF protection for
-- cookie-authenticated browser requests (bearer API clients are exempt).
CREATE TABLE IF NOT EXISTS sessions (
    session_id   TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash   TEXT NOT NULL UNIQUE,
    csrf_token   TEXT NOT NULL,
    user_agent   TEXT NOT NULL DEFAULT '',
    ip           TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at   TIMESTAMPTZ NOT NULL,
    revoked_at   TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);

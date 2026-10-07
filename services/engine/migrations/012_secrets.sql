-- 012 — Secret store at rest (issue #126).
-- Values are stored only as AES-256-GCM ciphertexts (security/secrets.Box)
-- bound to scope + "/" + name as associated data, so a ciphertext cannot be
-- moved to another scope or renamed without detection. Plaintext never
-- touches the database, logs or errors.

CREATE TABLE IF NOT EXISTS secrets (
    secret_id    TEXT PRIMARY KEY,
    scope        TEXT NOT NULL CHECK (scope <> ''), -- e.g. application:<id> | connection:<id>
    name         TEXT NOT NULL CHECK (name <> ''),
    value_sealed BYTEA NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (scope, name)
);

CREATE INDEX IF NOT EXISTS idx_secrets_scope ON secrets (scope);

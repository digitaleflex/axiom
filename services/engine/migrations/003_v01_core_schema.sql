-- 003 — V0.1 core relational model (issue #115).
-- Rules: never edit an applied migration; every change is a new numbered file.
-- Identifiers are opaque TEXT (API contract §3), not UUIDs.

-- 1. Opaque TEXT identifiers -------------------------------------------------
ALTER TABLE github_connections DROP CONSTRAINT IF EXISTS github_connections_user_id_fkey;
ALTER TABLE repositories      DROP CONSTRAINT IF EXISTS repositories_connection_id_fkey;
ALTER TABLE applications      DROP CONSTRAINT IF EXISTS applications_repository_id_fkey;
ALTER TABLE deployments       DROP CONSTRAINT IF EXISTS deployments_application_id_fkey;
ALTER TABLE deployments       DROP CONSTRAINT IF EXISTS deployments_server_id_fkey;

ALTER TABLE users              ALTER COLUMN id TYPE TEXT USING id::text;
ALTER TABLE github_connections ALTER COLUMN id TYPE TEXT USING id::text,
                               ALTER COLUMN user_id TYPE TEXT USING user_id::text;
ALTER TABLE repositories       ALTER COLUMN id TYPE TEXT USING id::text,
                               ALTER COLUMN connection_id TYPE TEXT USING connection_id::text;
ALTER TABLE applications       ALTER COLUMN id TYPE TEXT USING id::text,
                               ALTER COLUMN repository_id TYPE TEXT USING repository_id::text;
ALTER TABLE servers            ALTER COLUMN id TYPE TEXT USING id::text;
ALTER TABLE deployments        ALTER COLUMN id TYPE TEXT USING id::text,
                               ALTER COLUMN application_id TYPE TEXT USING application_id::text,
                               ALTER COLUMN server_id TYPE TEXT USING server_id::text;

ALTER TABLE github_connections ADD CONSTRAINT github_connections_user_id_fkey
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;
ALTER TABLE repositories ADD CONSTRAINT repositories_connection_id_fkey
    FOREIGN KEY (connection_id) REFERENCES github_connections(id) ON DELETE CASCADE;
ALTER TABLE applications ADD CONSTRAINT applications_repository_id_fkey
    FOREIGN KEY (repository_id) REFERENCES repositories(id) ON DELETE CASCADE;
ALTER TABLE deployments ADD CONSTRAINT deployments_application_id_fkey
    FOREIGN KEY (application_id) REFERENCES applications(id) ON DELETE CASCADE;
ALTER TABLE deployments ADD CONSTRAINT deployments_server_id_fkey
    FOREIGN KEY (server_id) REFERENCES servers(id) ON DELETE RESTRICT;

-- 2. Users & GitHub connections ---------------------------------------------
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS email TEXT,
    ADD COLUMN IF NOT EXISTS display_name TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX IF NOT EXISTS users_email_key ON users (lower(email)) WHERE email IS NOT NULL;

ALTER TABLE github_connections
    ADD COLUMN IF NOT EXISTS account_login TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS account_type TEXT NOT NULL DEFAULT 'User',
    ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'active',
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE github_connections
    ADD CONSTRAINT github_connections_account_type_check CHECK (account_type IN ('User', 'Organization')),
    ADD CONSTRAINT github_connections_status_check CHECK (status IN ('active', 'needs_attention', 'disconnected'));

-- 3. Repositories & applications ---------------------------------------------
ALTER TABLE repositories
    ADD COLUMN IF NOT EXISTS default_branch TEXT NOT NULL DEFAULT 'main',
    ADD COLUMN IF NOT EXISTS private BOOLEAN NOT NULL DEFAULT true,
    ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT now();

-- Several applications may be created from one repository (design #134).
ALTER TABLE applications DROP CONSTRAINT IF EXISTS applications_repository_id_key;
ALTER TABLE applications
    ADD COLUMN IF NOT EXISTS owner_id TEXT REFERENCES users(id) ON DELETE CASCADE,
    ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE applications
    ADD CONSTRAINT applications_name_check CHECK (name ~ '^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$') NOT VALID;
CREATE INDEX IF NOT EXISTS idx_applications_repository_id ON applications(repository_id);
CREATE INDEX IF NOT EXISTS idx_applications_owner_id ON applications(owner_id);

-- 4. Servers -------------------------------------------------------------------
ALTER TABLE servers
    ADD COLUMN IF NOT EXISTS owner_id TEXT REFERENCES users(id) ON DELETE CASCADE,
    ADD COLUMN IF NOT EXISTS architecture TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE servers
    ADD CONSTRAINT servers_status_check CHECK (status IN ('unknown', 'pending', 'ready', 'degraded', 'offline', 'revoked'));

-- 5. Deployment plans (immutable, single-use) ---------------------------------
CREATE TABLE IF NOT EXISTS deployment_plans (
    id                          TEXT PRIMARY KEY,
    application_id              TEXT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    server_id                   TEXT NOT NULL REFERENCES servers(id) ON DELETE RESTRICT,
    environment                 TEXT NOT NULL CHECK (environment IN ('production', 'staging', 'preview')),
    ref                         TEXT NOT NULL,
    commit_sha                  TEXT NOT NULL DEFAULT '',
    application_profile_version INTEGER NOT NULL CHECK (application_profile_version > 0),
    status                      TEXT NOT NULL DEFAULT 'READY' CHECK (status IN ('READY', 'INVALID', 'STALE')),
    fingerprint                 TEXT NOT NULL,
    body                        JSONB NOT NULL,
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_deployment_plans_application_id ON deployment_plans(application_id);
CREATE INDEX IF NOT EXISTS idx_deployment_plans_fingerprint ON deployment_plans(fingerprint);

-- 6. Deployments -------------------------------------------------------------
UPDATE deployments SET status = upper(status);
UPDATE deployments SET status = 'FAILED'
 WHERE status NOT IN ('PENDING', 'ANALYZING', 'PLANNING', 'BUILDING', 'DEPLOYING', 'VERIFYING', 'LIVE', 'FAILED', 'CANCELLED');
ALTER TABLE deployments ALTER COLUMN status SET DEFAULT 'PENDING';
ALTER TABLE deployments
    ADD COLUMN IF NOT EXISTS plan_id TEXT REFERENCES deployment_plans(id) ON DELETE RESTRICT,
    ADD COLUMN IF NOT EXISTS number INTEGER,
    ADD COLUMN IF NOT EXISTS url TEXT,
    ADD COLUMN IF NOT EXISTS error_code TEXT,
    ADD COLUMN IF NOT EXISTS created_by TEXT REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD COLUMN IF NOT EXISTS started_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS completed_at TIMESTAMPTZ;
ALTER TABLE deployments
    ADD CONSTRAINT deployments_status_check
        CHECK (status IN ('PENDING', 'ANALYZING', 'PLANNING', 'BUILDING', 'DEPLOYING', 'VERIFYING', 'LIVE', 'FAILED', 'CANCELLED')),
    ADD CONSTRAINT deployments_environment_check
        CHECK (environment IN ('production', 'staging', 'preview'));
-- A plan is single-use (artifacts.md R3).
CREATE UNIQUE INDEX IF NOT EXISTS deployments_plan_id_key ON deployments(plan_id) WHERE plan_id IS NOT NULL;
-- Human-readable deployment number per application (#42).
CREATE UNIQUE INDEX IF NOT EXISTS deployments_application_number_key ON deployments(application_id, number) WHERE number IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_deployments_app_env_created ON deployments(application_id, environment, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_deployments_status ON deployments(status);

-- 7. Deployment steps ----------------------------------------------------------
CREATE TABLE IF NOT EXISTS deployment_steps (
    deployment_id TEXT NOT NULL REFERENCES deployments(id) ON DELETE CASCADE,
    name          TEXT NOT NULL CHECK (name IN ('BUILD', 'CREATE_RUNTIME', 'NETWORK', 'START', 'VERIFY')),
    position      SMALLINT NOT NULL CHECK (position BETWEEN 1 AND 5),
    status        TEXT NOT NULL DEFAULT 'QUEUED' CHECK (status IN ('QUEUED', 'RUNNING', 'COMPLETED', 'FAILED', 'SKIPPED', 'CANCELLED')),
    started_at    TIMESTAMPTZ,
    completed_at  TIMESTAMPTZ,
    exit_code     INTEGER,
    error_code    TEXT,
    PRIMARY KEY (deployment_id, name),
    UNIQUE (deployment_id, position)
);

-- 8. Deployment events (ordered per deployment) -------------------------------
CREATE TABLE IF NOT EXISTS deployment_events (
    deployment_id TEXT NOT NULL REFERENCES deployments(id) ON DELETE CASCADE,
    seq           BIGINT NOT NULL CHECK (seq > 0),
    id            TEXT NOT NULL UNIQUE,
    type          TEXT NOT NULL,
    data          JSONB NOT NULL DEFAULT '{}'::jsonb,
    occurred_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (deployment_id, seq)
);

-- 9. Idempotency keys ---------------------------------------------------------
CREATE TABLE IF NOT EXISTS idempotency_keys (
    scope        TEXT NOT NULL,
    key          TEXT NOT NULL CHECK (length(key) BETWEEN 1 AND 255),
    request_hash TEXT NOT NULL,
    resource_id  TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (scope, key)
);
CREATE INDEX IF NOT EXISTS idx_idempotency_keys_created_at ON idempotency_keys(created_at);

-- 10. Domains -----------------------------------------------------------------
CREATE TABLE IF NOT EXISTS domains (
    id             TEXT PRIMARY KEY,
    application_id TEXT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    environment    TEXT NOT NULL CHECK (environment IN ('production', 'staging', 'preview')),
    hostname       TEXT NOT NULL CHECK (hostname = lower(hostname) AND hostname ~ '^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$'),
    is_primary     BOOLEAN NOT NULL DEFAULT false,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS domains_hostname_key ON domains(hostname);
CREATE UNIQUE INDEX IF NOT EXISTS domains_primary_key ON domains(application_id, environment) WHERE is_primary;

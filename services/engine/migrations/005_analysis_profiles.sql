-- 005 — Repository analyses and versioned Application Profiles (#58, #59, #95).

CREATE TABLE IF NOT EXISTS analyses (
    id               TEXT PRIMARY KEY,
    application_id   TEXT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    ref              TEXT NOT NULL,
    commit_sha       TEXT NOT NULL DEFAULT '',
    root             TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL CHECK (status IN ('RUNNING', 'COMPLETED', 'FAILED')),
    analyzer_version TEXT NOT NULL DEFAULT '',
    result           JSONB,
    error_code       TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at     TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_analyses_application_created ON analyses(application_id, created_at DESC);

-- Profiles are immutable revisions; the highest version is current.
CREATE TABLE IF NOT EXISTS application_profiles (
    application_id TEXT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    version        INTEGER NOT NULL CHECK (version > 0),
    analysis_id    TEXT NOT NULL REFERENCES analyses(id) ON DELETE CASCADE,
    status         TEXT NOT NULL CHECK (status IN ('ready', 'needs_review', 'unsupported')),
    body           JSONB NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (application_id, version)
);

-- User overrides (explicit values) applied on top of detection.
CREATE TABLE IF NOT EXISTS application_overrides (
    application_id TEXT PRIMARY KEY REFERENCES applications(id) ON DELETE CASCADE,
    hints          JSONB NOT NULL DEFAULT '{}'::jsonb,
    root           TEXT NOT NULL DEFAULT '',
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

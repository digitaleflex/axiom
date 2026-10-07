-- 008 — Deployment logs (issue #66).
-- Durable, queryable deployment logs with levels, step/source correlation,
-- secret redaction (applied before persistence by the logs package) and a
-- bounded per-deployment retention (enforced by logs.PGStore.Append).

CREATE TABLE IF NOT EXISTS deployment_logs (
    id            TEXT PRIMARY KEY,
    deployment_id TEXT NOT NULL REFERENCES deployments(id) ON DELETE CASCADE,
    occurred_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    level         TEXT NOT NULL CHECK (level IN ('DEBUG', 'INFO', 'WARN', 'ERROR')),
    step          TEXT NOT NULL DEFAULT '' CHECK (step IN ('BUILD', 'CREATE_RUNTIME', 'NETWORK', 'START', 'VERIFY', '')),
    source        TEXT NOT NULL CHECK (source IN ('build', 'deploy', 'runtime')),
    message       TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_deployment_logs_deployment_occurred_at ON deployment_logs(deployment_id, occurred_at);

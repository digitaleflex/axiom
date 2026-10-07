-- 013 — Audit events (issue #128).
-- Security-relevant operations: actor, action, target, result, request and
-- correlation IDs, deployment linkage and an allow-listed detail map.
-- Secrets never reach this table: the audit package redacts every string
-- field and drops disallowed detail keys before persistence.

CREATE TABLE IF NOT EXISTS audit_events (
    id             TEXT PRIMARY KEY,
    actor_id       TEXT NOT NULL,
    actor_name     TEXT NOT NULL DEFAULT '',
    action         TEXT NOT NULL,
    target_type    TEXT NOT NULL,
    target_id      TEXT NOT NULL,
    result         TEXT NOT NULL CHECK (result IN ('ok', 'error')),
    error_code     TEXT NOT NULL DEFAULT '',
    request_id     TEXT NOT NULL DEFAULT '',
    correlation_id TEXT NOT NULL DEFAULT '',
    deployment_id  TEXT NOT NULL DEFAULT '',
    owner_id       TEXT NOT NULL DEFAULT '',
    occurred_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    details        JSONB NOT NULL DEFAULT '{}'::jsonb
);

-- Actor, target and time indexes back the owner-scoped read endpoint and
-- the per-actor / per-target queries.
CREATE INDEX IF NOT EXISTS idx_audit_events_actor ON audit_events(actor_id, occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_events_target ON audit_events(target_type, target_id, occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_events_occurred_at ON audit_events(occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_events_owner ON audit_events(owner_id, occurred_at DESC);

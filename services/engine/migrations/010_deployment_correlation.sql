-- 010 — Correlation ID on deployments (#101 traceability).

ALTER TABLE deployments
    ADD COLUMN IF NOT EXISTS correlation_id TEXT NOT NULL DEFAULT '';

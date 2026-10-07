-- 006 — Domain verification state (issue #64).
-- DNS status is checked by the Engine; TLS/routing states are reported by
-- the Runtime Agent (#84) and default to pending.

ALTER TABLE domains
    ADD COLUMN IF NOT EXISTS dns_status    TEXT NOT NULL DEFAULT 'unknown',
    ADD COLUMN IF NOT EXISTS dns_expected  TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS dns_observed  TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS dns_checked_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS tls_status     TEXT NOT NULL DEFAULT 'pending',
    ADD COLUMN IF NOT EXISTS routing_status TEXT NOT NULL DEFAULT 'pending';

ALTER TABLE domains
    ADD CONSTRAINT domains_dns_status_check
        CHECK (dns_status IN ('unknown', 'pending', 'ok', 'mismatch', 'error')),
    ADD CONSTRAINT domains_tls_status_check
        CHECK (tls_status IN ('pending', 'issuing', 'valid', 'expiring', 'failed')),
    ADD CONSTRAINT domains_routing_status_check
        CHECK (routing_status IN ('pending', 'active', 'failed'));

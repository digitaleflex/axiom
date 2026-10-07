-- 007 — Allow single-label hostnames such as localhost (issue #64).

ALTER TABLE domains DROP CONSTRAINT IF EXISTS domains_hostname_check;
ALTER TABLE domains
    ADD CONSTRAINT domains_hostname_check
    CHECK (hostname = lower(hostname)
        AND hostname ~ '^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)*[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$');

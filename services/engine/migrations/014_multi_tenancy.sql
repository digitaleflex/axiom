-- 014 — Multi-tenancy: organizations, projects, memberships (issue #146, M12.1).
-- Rules: never edit an applied migration; every change is a new numbered file.
-- Identifiers are opaque TEXT (API contract §3).
--
-- V0.1 was single-user: `applications.owner_id` pointed at users. This migration
-- introduces the organization as the ownership boundary and keeps every V0.1
-- row reachable through a single backfilled organization, so existing
-- deployments and applications keep working unchanged.

-- 1. Organizations -------------------------------------------------------------
CREATE TABLE IF NOT EXISTS organizations (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 120),
    slug        TEXT NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$'),
    plan        TEXT NOT NULL DEFAULT 'free' CHECK (plan IN ('free', 'starter', 'pro', 'enterprise')),
    -- Limits are stored as JSON so a plan change does not require a migration.
    limits      JSONB NOT NULL DEFAULT '{}'::jsonb,
    -- Usage counters are denormalized for quota checks on the hot path.
    usage       JSONB NOT NULL DEFAULT '{}'::jsonb,
    billing_customer_id TEXT UNIQUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_organizations_plan ON organizations(plan);

-- 2. Memberships (user ↔ organization, with a role) ---------------------------
CREATE TABLE IF NOT EXISTS organization_members (
    org_id     TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role       TEXT NOT NULL CHECK (role IN ('owner', 'admin', 'developer', 'viewer')),
    invited_by TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_org_members_user_id ON organization_members(user_id);

-- 3. Invitations ---------------------------------------------------------------
-- token is stored as a SHA-256 hash; the plaintext is returned once in the
-- invitation link and never persisted (same rule as sessions, #125).
CREATE TABLE IF NOT EXISTS organization_invitations (
    id          TEXT PRIMARY KEY,
    org_id      TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    email       TEXT NOT NULL CHECK (email = lower(email)),
    role        TEXT NOT NULL CHECK (role IN ('admin', 'developer', 'viewer')),
    token_hash  TEXT NOT NULL UNIQUE,
    invited_by  TEXT REFERENCES users(id) ON DELETE SET NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    accepted_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_org_invitations_org ON organization_invitations(org_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_org_invitations_pending
    ON organization_invitations(org_id, email) WHERE accepted_at IS NULL;

-- 4. Projects -----------------------------------------------------------------
-- A project is the client-facing unit: one repository deployed to one or more
-- environments on the servers it is assigned to.
CREATE TABLE IF NOT EXISTS projects (
    id            TEXT PRIMARY KEY,
    org_id        TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name          TEXT NOT NULL CHECK (name ~ '^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$'),
    slug          TEXT NOT NULL CHECK (slug ~ '^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$'),
    description   TEXT NOT NULL DEFAULT '',
    environment   TEXT NOT NULL DEFAULT 'production'
                  CHECK (environment IN ('production', 'staging', 'preview')),
    primary_domain TEXT,
    created_by    TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, slug)
);
CREATE INDEX IF NOT EXISTS idx_projects_org_id ON projects(org_id);

-- 5. Ownership columns on the existing V0.1 tables -----------------------------
-- org_id is nullable so a deployment created before this migration stays valid;
-- it is backfilled immediately below. project_id is nullable for the same reason:
-- a project groups deployments, it does not replace the application.
ALTER TABLE applications  ADD COLUMN IF NOT EXISTS org_id     TEXT REFERENCES organizations(id) ON DELETE CASCADE;
ALTER TABLE applications  ADD COLUMN IF NOT EXISTS project_id TEXT REFERENCES projects(id)      ON DELETE SET NULL;
ALTER TABLE deployments   ADD COLUMN IF NOT EXISTS org_id     TEXT REFERENCES organizations(id) ON DELETE CASCADE;
ALTER TABLE deployments   ADD COLUMN IF NOT EXISTS project_id TEXT REFERENCES projects(id)      ON DELETE SET NULL;
ALTER TABLE servers       ADD COLUMN IF NOT EXISTS org_id     TEXT REFERENCES organizations(id) ON DELETE CASCADE;

CREATE INDEX IF NOT EXISTS idx_applications_org_id     ON applications(org_id);
CREATE INDEX IF NOT EXISTS idx_applications_project_id ON applications(project_id);
CREATE INDEX IF NOT EXISTS idx_deployments_org_id      ON deployments(org_id);
CREATE INDEX IF NOT EXISTS idx_deployments_project_id  ON deployments(project_id);
CREATE INDEX IF NOT EXISTS idx_servers_org_id          ON servers(org_id);

-- 6. Backfill: one organization per existing user ------------------------------
-- Every V0.1 user owns exactly one organization and is its owner. This keeps
-- applications.owner_id (the V0.1 authorization boundary) and the new
-- organization boundary in agreement, so no row becomes orphaned.
INSERT INTO organizations (id, name, slug, plan)
SELECT 'org_' || substr(md5(u.id), 1, 24),
       COALESCE(NULLIF(u.display_name, ''), u.id),
       substr(md5('org:' || u.id), 1, 24),
       'free'
  FROM users u
 WHERE NOT EXISTS (SELECT 1 FROM organizations o WHERE o.slug = substr(md5('org:' || u.id), 1, 24));

INSERT INTO organization_members (org_id, user_id, role)
SELECT 'org_' || substr(md5(u.id), 1, 24), u.id, 'owner'
  FROM users u
 WHERE NOT EXISTS (
       SELECT 1 FROM organization_members m
        WHERE m.user_id = u.id AND m.role = 'owner'
 );

UPDATE applications SET org_id = 'org_' || substr(md5(owner_id), 1, 24)
 WHERE org_id IS NULL AND owner_id IS NOT NULL;

UPDATE servers SET org_id = 'org_' || substr(md5(owner_id), 1, 24)
 WHERE org_id IS NULL AND owner_id IS NOT NULL;

-- Deployments inherit the organization through their application.
UPDATE deployments d
   SET org_id = a.org_id
  FROM applications a
 WHERE d.application_id = a.id AND d.org_id IS NULL;

-- 7. Secrets follow the organization ------------------------------------------
-- The secrets table stores sealed application configuration and agent keys.
-- organization scoping is what a multi-tenant quota check reads first.
ALTER TABLE secrets ADD COLUMN IF NOT EXISTS org_id     TEXT REFERENCES organizations(id) ON DELETE CASCADE;
ALTER TABLE secrets ADD COLUMN IF NOT EXISTS project_id TEXT REFERENCES projects(id)      ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_secrets_org_id     ON secrets(org_id);
CREATE INDEX IF NOT EXISTS idx_secrets_project_id ON secrets(project_id);

-- 8. Audit trail is organization-scoped ----------------------------------------
ALTER TABLE audit_events ADD COLUMN IF NOT EXISTS org_id TEXT REFERENCES organizations(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_audit_events_org_id ON audit_events(org_id);

-- 9. Plan limits for the free tier --------------------------------------------
-- Seeded on every organization so quota checks never read an empty object.
UPDATE organizations
   SET limits = jsonb_build_object(
           'max_projects', 1,
           'max_memory_mb', 512,
           'max_cpu_percent', 50,
           'max_domains', 1,
           'max_deployments_month', 100,
           'max_team_members', 1,
           'custom_domains', false,
           'ssl_auto', true,
           'audit_logs', false
       )
 WHERE limits = '{}'::jsonb;

-- 10. Ownership derivation at insert time -------------------------------------
-- The backfill in §6 only covers rows that existed when the migration ran. A
-- user created afterwards, or an application inserted after the migration, must
-- still land in an organization. Both triggers derive the organization from the
-- V0.1 owner column, so every row is attributable without the caller having to
-- know about multi-tenancy. An explicit org_id is never overwritten.

CREATE OR REPLACE FUNCTION create_personal_organization() RETURNS trigger AS $$
DECLARE
    new_org_id TEXT;
    new_org_slug TEXT;
BEGIN
    new_org_id := 'org_' || substr(md5(NEW.id), 1, 24);
    new_org_slug := substr(md5('org:' || NEW.id), 1, 24);

    -- Limits are seeded here, not only by the §9 UPDATE: an organization created
    -- after the migration must never carry an empty limits object, or a quota
    -- check would read a missing key as "no limit".
    INSERT INTO organizations (id, name, slug, plan, limits)
    VALUES (new_org_id,
            COALESCE(NULLIF(NEW.display_name, ''), NEW.id),
            new_org_slug,
            'free',
            '{"max_projects":1,"max_memory_mb":512,"max_cpu_percent":50,"max_domains":1,"max_deployments_month":100,"max_team_members":1,"custom_domains":false,"ssl_auto":true,"audit_logs":false}'::jsonb)
    ON CONFLICT (id) DO NOTHING;

    INSERT INTO organization_members (org_id, user_id, role)
    VALUES (new_org_id, NEW.id, 'owner')
    ON CONFLICT (org_id, user_id) DO NOTHING;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS users_create_personal_organization ON users;
CREATE TRIGGER users_create_personal_organization
    AFTER INSERT ON users
    FOR EACH ROW EXECUTE FUNCTION create_personal_organization();

-- Applications inherit their owner's organization when the caller does not set
-- one. This keeps the V0.1 insert path (`repository_id`, `name`, `owner_id`)
-- working unchanged: no application can exist outside an organization.
CREATE OR REPLACE FUNCTION derive_application_org() RETURNS trigger AS $$
BEGIN
    IF NEW.org_id IS NULL AND NEW.owner_id IS NOT NULL THEN
        NEW.org_id := 'org_' || substr(md5(NEW.owner_id), 1, 24);
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS applications_derive_org ON applications;
CREATE TRIGGER applications_derive_org
    BEFORE INSERT ON applications
    FOR EACH ROW EXECUTE FUNCTION derive_application_org();

CREATE OR REPLACE FUNCTION derive_server_org() RETURNS trigger AS $$
BEGIN
    IF NEW.org_id IS NULL AND NEW.owner_id IS NOT NULL THEN
        NEW.org_id := 'org_' || substr(md5(NEW.owner_id), 1, 24);
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS servers_derive_org ON servers;
CREATE TRIGGER servers_derive_org
    BEFORE INSERT ON servers
    FOR EACH ROW EXECUTE FUNCTION derive_server_org();

-- A deployment inherits its organization's application, so a deployment can
-- never disagree with the application it rolls out.
CREATE OR REPLACE FUNCTION derive_deployment_org() RETURNS trigger AS $$
DECLARE
    app_org_id TEXT;
BEGIN
    IF NEW.org_id IS NULL THEN
        SELECT a.org_id INTO app_org_id FROM applications a WHERE a.id = NEW.application_id;
        NEW.org_id := app_org_id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS deployments_derive_org ON deployments;
CREATE TRIGGER deployments_derive_org
    BEFORE INSERT ON deployments
    FOR EACH ROW EXECUTE FUNCTION derive_deployment_org();

-- 11. Triggers: keep updated_at honest ----------------------------------------
CREATE OR REPLACE FUNCTION touch_organization_updated_at() RETURNS trigger AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS organizations_touch_updated_at ON organizations;
CREATE TRIGGER organizations_touch_updated_at
    BEFORE UPDATE ON organizations
    FOR EACH ROW EXECUTE FUNCTION touch_organization_updated_at();

CREATE OR REPLACE FUNCTION touch_project_updated_at() RETURNS trigger AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS projects_touch_updated_at ON projects;
CREATE TRIGGER projects_touch_updated_at
    BEFORE UPDATE ON projects
    FOR EACH ROW EXECUTE FUNCTION touch_project_updated_at();

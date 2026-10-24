-- 015 — Project → application link (issue #146, M12.1).
-- internal/project reads and writes projects.application_id, which 014 did not
-- create. It is optional: a project can exist before a repository is attached.
ALTER TABLE projects
    ADD COLUMN IF NOT EXISTS application_id TEXT REFERENCES applications(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_projects_application_id ON projects(application_id);

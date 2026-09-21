CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS github_connections (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS repositories (
    id UUID PRIMARY KEY,
    connection_id UUID NOT NULL REFERENCES github_connections(id) ON DELETE CASCADE,
    external_id TEXT NOT NULL,
    full_name TEXT NOT NULL,
    clone_url TEXT NOT NULL,
    UNIQUE(connection_id, external_id)
);

CREATE TABLE IF NOT EXISTS applications (
    id UUID PRIMARY KEY,
    repository_id UUID NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    stack TEXT,
    UNIQUE(repository_id)
);

CREATE TABLE IF NOT EXISTS servers (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL,
    address TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'unknown'
);

CREATE TABLE IF NOT EXISTS deployments (
    id UUID PRIMARY KEY,
    application_id UUID NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    server_id UUID NOT NULL REFERENCES servers(id) ON DELETE RESTRICT,
    environment TEXT NOT NULL DEFAULT 'production',
    status TEXT NOT NULL DEFAULT 'pending'
);

CREATE INDEX IF NOT EXISTS idx_github_connections_user_id ON github_connections(user_id);
CREATE INDEX IF NOT EXISTS idx_repositories_connection_id ON repositories(connection_id);
CREATE INDEX IF NOT EXISTS idx_deployments_application_id ON deployments(application_id);
CREATE INDEX IF NOT EXISTS idx_deployments_server_id ON deployments(server_id);

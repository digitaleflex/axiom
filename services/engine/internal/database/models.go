package database

// The V0.1 persistence model intentionally follows the deployment domain:
// User -> GitHubConnection -> Repository -> Application -> Deployment -> Server.

type User struct {
	ID        string
	CreatedAt string
}

type GitHubConnection struct {
	ID        string
	UserID    string
	CreatedAt string
}

type Repository struct {
	ID          string
	ConnectionID string
	ExternalID  string
	FullName    string
	CloneURL    string
}

type Application struct {
	ID           string
	RepositoryID string
	Name         string
	Stack        string
}

type Server struct {
	ID        string
	Name      string
	Address   string
	Status    string
}

type Deployment struct {
	ID            string
	ApplicationID string
	ServerID      string
	Environment   string
	Status        string
}

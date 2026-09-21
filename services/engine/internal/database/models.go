package database

import "time"

// The V0.1 persistence model follows the deployment domain:
// User -> GitHubConnection -> Repository -> Application -> Deployment -> Server.
type User struct {
	ID        string
	CreatedAt time.Time
}

type GitHubConnection struct {
	ID        string
	UserID    string
	CreatedAt time.Time
}

type Repository struct {
	ID           string
	ConnectionID string
	ExternalID   string
	FullName     string
	CloneURL     string
}

type Application struct {
	ID           string
	RepositoryID string
	Name         string
	Stack        string
}

type Server struct {
	ID      string
	Name    string
	Address string
	Status  string
}

type Deployment struct {
	ID            string
	ApplicationID string
	ServerID      string
	Environment   string
	Status        string
}

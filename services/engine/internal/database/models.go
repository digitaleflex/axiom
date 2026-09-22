package database

import "time"

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
	ID           string
	Name         string
	Address      string
	Status       string
	AgentVersion string
	Capabilities []string
	CPUCount     int
	MemoryMB     int
	DiskFreeMB   int
	LastSeenAt   time.Time
}

type Deployment struct {
	ID            string
	ApplicationID string
	ServerID      string
	Environment   string
	Status        string
}

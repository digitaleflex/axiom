package artifacts

import "fmt"

type Artifact struct {
	ID        string
	Type      string
	Version   string
	ProjectID string
	TaskID    string
	Valid     bool
	Supersedes string
}

type Registry struct { items map[string]Artifact }

func NewRegistry() *Registry { return &Registry{items: map[string]Artifact{}} }

func (r *Registry) Publish(a Artifact) error {
	if a.ID == "" || a.Version == "" || a.ProjectID == "" || a.TaskID == "" { return fmt.Errorf("invalid artifact identity") }
	key := a.ID + "@" + a.Version
	if _, exists := r.items[key]; exists { return fmt.Errorf("artifact version already exists: %s", key) }
	r.items[key] = a
	return nil
}

func (r *Registry) Get(id, version string) (Artifact, bool) {
	a, ok := r.items[id+"@"+version]
	return a, ok
}
package context

type Snapshot struct {
	ID           string
	ProjectID    string
	TaskID       string
	ArtifactIDs  []string
	DecisionIDs  []string
	RepositoryRef string
	RuntimeRef   string
	SensitiveRefs []string
}

func NewSnapshot(id, projectID, taskID string, artifacts, decisions []string) Snapshot {
	return Snapshot{ID:id, ProjectID:projectID, TaskID:taskID, ArtifactIDs:artifacts, DecisionIDs:decisions}
}
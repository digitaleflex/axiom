package planner

import "fmt"

// ReadyTasks returns tasks whose dependencies are all completed.
// The input is intentionally generic so the core planner stays independent
// from persistence and provider implementations.
type Task struct {
	ID           string
	Dependencies []string
	Status       string
}

func ReadyTasks(tasks []Task) ([]Task, error) {
	byID := make(map[string]Task, len(tasks))
	for _, task := range tasks {
		if _, exists := byID[task.ID]; exists {
			return nil, fmt.Errorf("duplicate task id: %s", task.ID)
		}
		byID[task.ID] = task
	}

	if err := validateAcyclic(tasks); err != nil {
		return nil, err
	}

	ready := make([]Task, 0)
	for _, task := range tasks {
		if task.Status != "PENDING" && task.Status != "READY" {
			continue
		}
		ok := true
		for _, dependencyID := range task.Dependencies {
			dependency, exists := byID[dependencyID]
			if !exists || dependency.Status != "COMPLETED" {
				ok = false
				break
			}
		}
		if ok {
			ready = append(ready, task)
		}
	}
	return ready, nil
}

func validateAcyclic(tasks []Task) error {
	graph := make(map[string][]string, len(tasks))
	for _, task := range tasks {
		graph[task.ID] = task.Dependencies
	}
	visiting := map[string]bool{}
	visited := map[string]bool{}
	var visit func(string) error
	visit = func(id string) error {
		if visiting[id] {
			return fmt.Errorf("circular dependency detected at task: %s", id)
		}
		if visited[id] {
			return nil
		}
		visiting[id] = true
		for _, dep := range graph[id] {
			if _, exists := graph[dep]; !exists {
				return fmt.Errorf("unknown dependency: %s", dep)
			}
			if err := visit(dep); err != nil {
				return err
			}
		}
		delete(visiting, id)
		visited[id] = true
		return nil
	}
	for _, task := range tasks {
		if err := visit(task.ID); err != nil {
			return err
		}
	}
	return nil
}

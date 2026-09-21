package router

import "fmt"

type Agent struct {
	ID           string
	Role         string
	Capabilities map[string]bool
	Permissions  map[string]bool
}

type Task struct {
	Role         string
	Capabilities []string
	Permissions  []string
}

func Match(task Task, agents []Agent) (Agent, error) {
	for _, agent := range agents {
		if agent.Role != task.Role { continue }
		valid := true
		for _, capability := range task.Capabilities {
			if !agent.Capabilities[capability] { valid = false; break }
		}
		if !valid { continue }
		for _, permission := range task.Permissions {
			if !agent.Permissions[permission] { valid = false; break }
		}
		if valid { return agent, nil }
	}
	return Agent{}, fmt.Errorf("no eligible agent for role %q", task.Role)
}

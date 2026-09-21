package planner

import "testing"

func TestReadyTasks(t *testing.T) {
	tasks := []Task{
		{ID: "architecture", Status: "COMPLETED"},
		{ID: "backend", Dependencies: []string{"architecture"}, Status: "PENDING"},
		{ID: "frontend", Dependencies: []string{"architecture"}, Status: "PENDING"},
		{ID: "qa", Dependencies: []string{"backend", "frontend"}, Status: "PENDING"},
	}
	ready, err := ReadyTasks(tasks)
	if err != nil {
		t.Fatal(err)
	}
	if len(ready) != 2 {
		t.Fatalf("expected 2 ready tasks, got %d", len(ready))
	}
}

func TestRejectsCycle(t *testing.T) {
	tasks := []Task{
		{ID: "a", Dependencies: []string{"b"}, Status: "PENDING"},
		{ID: "b", Dependencies: []string{"a"}, Status: "PENDING"},
	}
	if _, err := ReadyTasks(tasks); err == nil {
		t.Fatal("expected circular dependency to be rejected")
	}
}

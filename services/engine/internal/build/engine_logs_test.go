package build

import (
	"context"
	"strings"
	"testing"

	"github.com/digitaleflex/axiom/services/engine/internal/logs"
)

// fakeLogStore records everything the engine persists (issue #66).
type fakeLogStore struct {
	entries []logs.Entry
}

func (f *fakeLogStore) Append(_ context.Context, entries ...logs.Entry) error {
	f.entries = append(f.entries, entries...)
	return nil
}

func TestEnginePersistsBuildLogs(t *testing.T) {
	eng, logger, _ := testEngine(t, &fakeBuilder{})
	store := &fakeLogStore{}
	eng.LogStore = store

	if _, err := eng.Build(context.Background(), Input{
		DeploymentID: "dep_1", Commit: commit, Plan: nextPlan(),
		Source: fakeSource{data: archive(map[string]string{"package.json": "{}"})},
	}); err != nil {
		t.Fatal(err)
	}

	// source checkout, Dockerfile choice, builder output tail, built message.
	if len(store.entries) < 4 {
		t.Fatalf("persisted %d entries, want at least 4", len(store.entries))
	}
	var sawTail bool
	for _, e := range store.entries {
		if e.DeploymentID != "dep_1" {
			t.Errorf("deployment = %q", e.DeploymentID)
		}
		if e.Step != logs.StepBuild || e.Source != logs.SourceBuild {
			t.Errorf("entry step/source = %q/%q, want BUILD/build", e.Step, e.Source)
		}
		if strings.Contains(e.Message, "build output tail:") {
			sawTail = true
			if e.Level != logs.LevelInfo {
				t.Errorf("success tail level = %q, want INFO", e.Level)
			}
		}
	}
	if !sawTail {
		t.Error("builder output tail not persisted as INFO on success")
	}
	// The in-process Logger stream is unchanged (3 events on success).
	if len(logger.msgs) != 3 {
		t.Fatalf("logger events = %v, want 3", logger.msgs)
	}
}

func TestEnginePersistsBuildFailureAsError(t *testing.T) {
	eng, _, _ := testEngine(t, &fakeBuilder{err: &Error{Code: CodeBuildFailed, Message: "boom", ExitCode: 2, Log: "Step 1/1 : FAIL"}})
	store := &fakeLogStore{}
	eng.LogStore = store

	_, err := eng.Build(context.Background(), Input{
		DeploymentID: "dep_1", Commit: commit, Plan: nextPlan(),
		Source: fakeSource{data: archive(map[string]string{"p": "x"})},
	})
	if err == nil {
		t.Fatal("expected build failure")
	}
	var sawErrorTail bool
	for _, e := range store.entries {
		if e.Level == logs.LevelError && strings.Contains(e.Message, "build output tail:") {
			sawErrorTail = true
		}
	}
	if !sawErrorTail {
		t.Error("builder output tail not persisted as ERROR on failure")
	}
}

func TestEngineWithoutLogStoreKeepsWorking(t *testing.T) {
	eng, logger, _ := testEngine(t, &fakeBuilder{})
	if _, err := eng.Build(context.Background(), Input{
		DeploymentID: "dep_1", Commit: commit, Plan: nextPlan(),
		Source: fakeSource{data: archive(map[string]string{"package.json": "{}"})},
	}); err != nil {
		t.Fatal(err)
	}
	if len(logger.msgs) != 3 {
		t.Fatalf("logger events = %v, want 3", logger.msgs)
	}
}

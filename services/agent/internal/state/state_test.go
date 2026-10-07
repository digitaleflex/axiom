package state_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/digitaleflex/axiom/services/agent/internal/protocol"
	"github.com/digitaleflex/axiom/services/agent/internal/state"
)

const (
	depA = "dep_aaaaaaaaaaaaaaaaaaaaaaaa"
	depB = "dep_bbbbbbbbbbbbbbbbbbbbbbbb"
)

func opID(dep, step string, attempt int) string {
	return fmt.Sprintf("op_%s_%s_%d", dep, step, attempt)
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time {
	c.t = c.t.Add(time.Second)
	return c.t
}

func newClock() *clock { return &clock{t: time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC)} }

func openTemp(t *testing.T) (*state.Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "operations.jsonl")
	s, err := state.Open(path, state.WithClock(newClock().now))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s, path
}

func TestBeginIdempotent(t *testing.T) {
	s, _ := openTemp(t)
	defer s.Close()

	op := state.Operation{OperationID: opID(depA, protocol.OpStartRuntime, 1), DeploymentID: depA, Type: protocol.OpStartRuntime}
	e1, err := s.Begin(op)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	e2, err := s.Begin(op)
	if err != nil {
		t.Fatalf("Begin (repeat): %v", err)
	}
	if !e1.StartedAt.Equal(e2.StartedAt) {
		t.Fatalf("Begin restarted the operation: %v != %v", e1.StartedAt, e2.StartedAt)
	}
	if e1.Phase != state.PhaseRunning || e2.Phase != state.PhaseRunning {
		t.Fatalf("phases = %q/%q, want RUNNING", e1.Phase, e2.Phase)
	}
	if s.Len() != 1 {
		t.Fatalf("Len = %d, want 1", s.Len())
	}
	if e1.Attempt != 1 {
		t.Fatalf("Attempt = %d, want 1 (parsed from ID)", e1.Attempt)
	}
}

func TestTerminalNeverLost(t *testing.T) {
	s, _ := openTemp(t)
	defer s.Close()

	id := opID(depA, protocol.OpCreateRuntime, 1)
	op := state.Operation{OperationID: id, DeploymentID: depA, Type: protocol.OpCreateRuntime}
	if _, err := s.Begin(op); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Complete(id, state.Result{Success: true}); err != nil {
		t.Fatal(err)
	}
	// Fail after Complete must not downgrade the terminal result.
	e, err := s.Fail(id, "RUNTIME_DOCKER_FAILED", "boom")
	if err != nil {
		t.Fatal(err)
	}
	if e.Phase != state.PhaseCompleted || !e.Success || e.ErrorCode != "" {
		t.Fatalf("Fail overwrote terminal COMPLETED: %+v", e)
	}
	// Begin on a terminal ID returns the existing entry without restarting.
	e2, err := s.Begin(op)
	if err != nil {
		t.Fatal(err)
	}
	if e2.Phase != state.PhaseCompleted || !e2.Success {
		t.Fatalf("Begin overwrote terminal entry: %+v", e2)
	}
	// Interrupt on a terminal ID is a no-op.
	e3, err := s.Interrupt(id)
	if err != nil {
		t.Fatal(err)
	}
	if e3.Phase != state.PhaseCompleted {
		t.Fatalf("Interrupt overwrote terminal entry: %+v", e3)
	}
}

func TestFailAndInterrupt(t *testing.T) {
	s, _ := openTemp(t)
	defer s.Close()

	fid := opID(depA, protocol.OpStartRuntime, 1)
	s.Begin(state.Operation{OperationID: fid, DeploymentID: depA, Type: protocol.OpStartRuntime})
	fe, err := s.Fail(fid, "RUNTIME_CONTAINER_NOT_FOUND", "no such container")
	if err != nil {
		t.Fatal(err)
	}
	if fe.Phase != state.PhaseFailed || fe.Success || fe.ErrorCode != "RUNTIME_CONTAINER_NOT_FOUND" {
		t.Fatalf("Fail entry = %+v", fe)
	}

	iid := opID(depA, protocol.OpVerifyHealth, 1)
	s.Begin(state.Operation{OperationID: iid, DeploymentID: depA, Type: protocol.OpVerifyHealth})
	ie, err := s.Interrupt(iid)
	if err != nil {
		t.Fatal(err)
	}
	if ie.Phase != state.PhaseInterrupted || !ie.Interrupted {
		t.Fatalf("Interrupt entry = %+v", ie)
	}
	if !ie.Resumable {
		t.Fatalf("VERIFY interrupt should be resumable: %+v", ie)
	}
	if _, err := s.Complete("op_missing", state.Result{}); !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("Complete unknown error = %v, want ErrNotFound", err)
	}
}

func TestListActiveExcludesTerminal(t *testing.T) {
	s, _ := openTemp(t)
	defer s.Close()

	done := opID(depA, protocol.OpCreateRuntime, 1)
	active := opID(depA, protocol.OpStartRuntime, 1)
	s.Begin(state.Operation{OperationID: done, DeploymentID: depA, Type: protocol.OpCreateRuntime})
	s.Complete(done, state.Result{Success: true})
	s.Begin(state.Operation{OperationID: active, DeploymentID: depA, Type: protocol.OpStartRuntime})

	got := s.ListActive()
	if len(got) != 1 || got[0].OperationID != active {
		t.Fatalf("ListActive = %+v, want just %s", got, active)
	}
}

func TestListByDeployment(t *testing.T) {
	s, _ := openTemp(t)
	defer s.Close()

	a1 := opID(depA, protocol.OpStartRuntime, 1)
	a2 := opID(depA, protocol.OpStartRuntime, 2)
	b1 := opID(depB, protocol.OpStartRuntime, 1)
	s.Begin(state.Operation{OperationID: a1, DeploymentID: depA, Type: protocol.OpStartRuntime})
	s.Begin(state.Operation{OperationID: a2, DeploymentID: depA, Type: protocol.OpStartRuntime})
	s.Begin(state.Operation{OperationID: b1, DeploymentID: depB, Type: protocol.OpStartRuntime})

	got := s.ListByDeployment(depA)
	if len(got) != 2 || got[0].OperationID != a1 || got[1].OperationID != a2 {
		t.Fatalf("ListByDeployment = %+v", got)
	}
	if len(s.ListByDeployment(depB)) != 1 {
		t.Fatalf("ListByDeployment(depB) wrong")
	}
}

func TestPruneBound(t *testing.T) {
	s, _ := openTemp(t)
	defer s.Close()

	for i := 1; i <= 5; i++ {
		id := opID(depA, protocol.OpCreateRuntime, i)
		s.Begin(state.Operation{OperationID: id, DeploymentID: depA, Type: protocol.OpCreateRuntime})
		if _, err := s.Complete(id, state.Result{Success: true}); err != nil {
			t.Fatal(err)
		}
	}
	// One active operation must survive pruning.
	active := opID(depA, protocol.OpStartRuntime, 9)
	s.Begin(state.Operation{OperationID: active, DeploymentID: depA, Type: protocol.OpStartRuntime})

	removed, err := s.Prune(2)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 3 {
		t.Fatalf("Prune removed %d, want 3", removed)
	}
	if s.Len() != 3 {
		t.Fatalf("Len after prune = %d, want 3 (2 terminal + 1 active)", s.Len())
	}
	if len(s.ListActive()) != 1 {
		t.Fatalf("active entry was pruned")
	}
}

func TestRestartMarksRunningInterrupted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operations.jsonl")
	s, err := state.Open(path, state.WithClock(newClock().now))
	if err != nil {
		t.Fatal(err)
	}
	run := opID(depA, protocol.OpCreateRuntime, 1)
	verify := opID(depA, protocol.OpVerifyHealth, 1)
	s.Begin(state.Operation{OperationID: run, DeploymentID: depA, Type: protocol.OpCreateRuntime})
	s.Begin(state.Operation{OperationID: verify, DeploymentID: depA, Type: protocol.OpVerifyHealth})
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s2, err := state.Open(path, state.WithClock(newClock().now))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()

	re, ok := s2.Get(run)
	if !ok || re.Phase != state.PhaseInterrupted || !re.Interrupted {
		t.Fatalf("RUNNING not marked INTERRUPTED on restart: %+v", re)
	}
	if re.Resumable {
		t.Fatalf("CREATE_RUNTIME must not be resumable")
	}
	ve, ok := s2.Get(verify)
	if !ok || ve.Phase != state.PhaseInterrupted || !ve.Resumable {
		t.Fatalf("VERIFY not marked resumable INTERRUPTED: %+v", ve)
	}
	if len(s2.ListInterrupted()) != 2 {
		t.Fatalf("ListInterrupted = %d, want 2", len(s2.ListInterrupted()))
	}
}

func TestPermissions0600(t *testing.T) {
	s, path := openTemp(t)
	if _, err := s.Begin(state.Operation{OperationID: opID(depA, protocol.OpStartRuntime, 1), DeploymentID: depA, Type: protocol.OpStartRuntime}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0o600 {
		t.Fatalf("file mode = %o, want 600", got)
	}
}

// TestAtomicRenameIgnoresStaleTmp proves a leftover .tmp from a crash before
// rename never affects the committed file.
func TestAtomicRenameIgnoresStaleTmp(t *testing.T) {
	s, path := openTemp(t)
	id := opID(depA, protocol.OpStartRuntime, 1)
	s.Begin(state.Operation{OperationID: id, DeploymentID: depA, Type: protocol.OpStartRuntime})
	s.Complete(id, state.Result{Success: true})
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".tmp", []byte(`{"v":1,"seq":99,"crc":"deadbeef","entry":{`), 0o600); err != nil {
		t.Fatal(err)
	}
	s2, err := state.Open(path)
	if err != nil {
		t.Fatalf("Open with stale tmp: %v", err)
	}
	defer s2.Close()
	if e, ok := s2.Get(id); !ok || e.Phase != state.PhaseCompleted {
		t.Fatalf("committed entry lost: %+v ok=%v", e, ok)
	}
}

// TestCorruptTailRecoverableKeepsEntries appends a partial record (a crash
// mid-append) and asserts the store rejects it recoverably while keeping every
// previously flushed entry.
func TestCorruptTailRecoverableKeepsEntries(t *testing.T) {
	s, path := openTemp(t)
	a := opID(depA, protocol.OpCreateRuntime, 1)
	b := opID(depA, protocol.OpStartRuntime, 1)
	s.Begin(state.Operation{OperationID: a, DeploymentID: depA, Type: protocol.OpCreateRuntime})
	s.Complete(a, state.Result{Success: true})
	s.Begin(state.Operation{OperationID: b, DeploymentID: depA, Type: protocol.OpStartRuntime})
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"v":1,"seq":99,"crc":"dead`); err != nil {
		t.Fatal(err)
	}
	f.Close()

	s2, err := state.Open(path)
	if !errors.Is(err, state.ErrCorrupt) {
		t.Fatalf("Open error = %v, want ErrCorrupt", err)
	}
	var ce *state.CorruptionError
	if !errors.As(err, &ce) || !ce.Tail {
		t.Fatalf("CorruptionError = %+v, want Tail", ce)
	}
	defer s2.Close()
	if e, ok := s2.Get(a); !ok || e.Phase != state.PhaseCompleted {
		t.Fatalf("flushed COMPLETED entry lost: %+v ok=%v", e, ok)
	}
	if e, ok := s2.Get(b); !ok || e.Phase != state.PhaseInterrupted {
		t.Fatalf("flushed RUNNING entry lost: %+v ok=%v", e, ok)
	}
}

// TestCorruptMiddleRecoverableKeepsEarlierEntries corrupts a record in the
// middle: earlier entries survive, later ones are not trusted.
func TestCorruptMiddleRecoverableKeepsEarlierEntries(t *testing.T) {
	s, path := openTemp(t)
	a := opID(depA, protocol.OpCreateRuntime, 1)
	b := opID(depA, protocol.OpStartRuntime, 1)
	c := opID(depA, protocol.OpVerifyHealth, 1)
	s.Begin(state.Operation{OperationID: a, DeploymentID: depA, Type: protocol.OpCreateRuntime})
	s.Complete(a, state.Result{Success: true})
	s.Begin(state.Operation{OperationID: b, DeploymentID: depA, Type: protocol.OpStartRuntime})
	s.Begin(state.Operation{OperationID: c, DeploymentID: depA, Type: protocol.OpVerifyHealth})
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimRight(data, "\n"), []byte{'\n'})
	if len(lines) < 4 {
		t.Fatalf("expected >=4 records, got %d", len(lines))
	}
	// Records: 0=Begin A, 1=Complete A, 2=Begin B, 3=Begin C. Corrupt B.
	lines[2] = []byte(`{"v":1,"seq":2,"crc":"00000000","entry":{"operationId":"tampered"}}`)
	if err := os.WriteFile(path, append(bytes.Join(lines, []byte{'\n'}), '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	s2, err := state.Open(path)
	if !errors.Is(err, state.ErrCorrupt) {
		t.Fatalf("Open error = %v, want ErrCorrupt", err)
	}
	var ce *state.CorruptionError
	if !errors.As(err, &ce) || ce.Tail {
		t.Fatalf("CorruptionError = %+v, want mid-file", ce)
	}
	defer s2.Close()
	if e, ok := s2.Get(a); !ok || e.Phase != state.PhaseCompleted {
		t.Fatalf("earlier entry lost: %+v ok=%v", e, ok)
	}
	if _, ok := s2.Get(b); ok {
		t.Fatalf("corrupt entry must not be applied")
	}
	if _, ok := s2.Get(c); ok {
		t.Fatalf("entry after corruption must not be trusted")
	}
}

func TestResumableOperation(t *testing.T) {
	if !state.ResumableOperation(protocol.OpVerifyHealth) {
		t.Fatal("VERIFY must be resumable")
	}
	for _, typ := range []string{
		protocol.OpCreateRuntime, protocol.OpConfigureNetwork, protocol.OpStartRuntime,
		protocol.OpStopRuntime, protocol.OpRemoveRuntime,
	} {
		if state.ResumableOperation(typ) {
			t.Fatalf("%s must not be resumable", typ)
		}
	}
}

func TestParseOperationID(t *testing.T) {
	dep, step, attempt, ok := state.ParseOperationID(opID(depA, protocol.OpCreateRuntime, 3))
	if !ok || dep != depA || step != protocol.OpCreateRuntime || attempt != 3 {
		t.Fatalf("ParseOperationID = %q %q %d %v", dep, step, attempt, ok)
	}
	if _, _, _, ok := state.ParseOperationID("garbage"); ok {
		t.Fatal("garbage parsed as valid")
	}
}

func TestMemoryStoreNoFile(t *testing.T) {
	s, err := state.Open("")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	id := opID(depA, protocol.OpStartRuntime, 1)
	if _, err := s.Begin(state.Operation{OperationID: id, DeploymentID: depA, Type: protocol.OpStartRuntime}); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get(id); !ok {
		t.Fatal("memory store lost entry")
	}
}

func TestInvalidBegin(t *testing.T) {
	s, _ := openTemp(t)
	defer s.Close()
	if _, err := s.Begin(state.Operation{DeploymentID: depA}); err == nil {
		t.Fatal("Begin with empty ID should fail")
	}
	if _, err := s.Begin(state.Operation{OperationID: opID(depA, protocol.OpStartRuntime, 1), DeploymentID: depA, Type: protocol.OpStartRuntime, Phase: state.PhaseCompleted}); err == nil {
		t.Fatal("Begin in terminal phase should fail")
	}
	if strings.TrimSpace(string(state.PhaseRunning)) != "RUNNING" {
		t.Fatal("unexpected phase constant")
	}
}

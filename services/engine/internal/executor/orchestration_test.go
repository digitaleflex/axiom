package executor

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/digitaleflex/axiom/services/engine/internal/build"
	"github.com/digitaleflex/axiom/services/engine/internal/deployment"
)

// flakyAgent fails the first fails[step] calls of a step, then succeeds.
type flakyAgent struct {
	fakeAgent
	mu     sync.Mutex
	fails  map[string]int
	calls  map[string]int
	lastOp map[string]Operation
}

func (a *flakyAgent) CreateRuntime(ctx context.Context, req CreateRuntimeRequest) error {
	a.mu.Lock()
	a.lastOp["CREATE_RUNTIME"] = req.Operation
	a.mu.Unlock()
	a.mu.Lock()
	a.calls["CREATE_RUNTIME"]++
	n := a.calls["CREATE_RUNTIME"]
	a.mu.Unlock()
	if n <= a.fails["CREATE_RUNTIME"] {
		return errors.New("agent: connection reset")
	}
	return a.fakeAgent.CreateRuntime(ctx, req)
}

func opOf(t *testing.T, a *flakyAgent, step string) Operation {
	t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	op, ok := a.lastOp[step]
	if !ok {
		t.Fatalf("no operation recorded for %s", step)
	}
	return op
}

func TestTransientFailureRetriesThenSucceeds(t *testing.T) {
	svc, rec, plan := setup(t)
	agent := &flakyAgent{fails: map[string]int{"CREATE_RUNTIME": 1}, calls: map[string]int{}, lastOp: map[string]Operation{}}
	ex := New(svc, fakeBuilder{}, agent)
	ex.Backoff = nil
	res, err := ex.Execute(context.Background(), Request{
		DeploymentID: rec.ID, CorrelationID: "req_test-1", Container: "c", Source: fakeSource{}, Plan: plan,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Deployment.Status != deployment.StateLive {
		t.Fatalf("status = %s", res.Deployment.Status)
	}
	op := opOf(t, agent, "CREATE_RUNTIME")
	if op.CorrelationID != "req_test-1" || op.OperationID != "op_"+rec.ID+"_CREATE_RUNTIME_2" {
		t.Fatalf("operation envelope = %+v", op)
	}
	if op.DeploymentID != rec.ID || op.ServerID != "srv_1" {
		t.Fatalf("operation binding = %+v", op)
	}
	steps, _ := svc.Store().Steps(context.Background(), rec.ID)
	if steps[1].Status != deployment.StepCompleted {
		t.Fatalf("CREATE_RUNTIME step = %s", steps[1].Status)
	}
}

func TestBuildExitFailureIsNotRetried(t *testing.T) {
	svc, rec, plan := setup(t)
	calls := 0
	builder := buildRunnerFunc(func(context.Context, build.Input) (build.Result, error) {
		calls++
		return build.Result{}, &build.Error{Code: build.CodeBuildFailed, Message: "exit 1", ExitCode: 1}
	})
	ex := New(svc, builder, &fakeAgent{})
	ex.Backoff = nil
	if _, err := ex.Execute(context.Background(), Request{DeploymentID: rec.ID, Container: "c", Source: fakeSource{}, Plan: plan}); err == nil {
		t.Fatal("expected failure")
	}
	if calls != 1 {
		t.Fatalf("deterministic build failure must not be retried, got %d calls", calls)
	}
	steps, _ := svc.Store().Steps(context.Background(), rec.ID)
	if steps[0].Status != deployment.StepFailed || steps[0].ErrorCode != ErrorBuildFailed ||
		steps[0].ExitCode == nil || *steps[0].ExitCode != 1 {
		t.Fatalf("BUILD step = %+v", steps[0])
	}
}

// buildRunnerFunc adapts a function to BuildRunner.
type buildRunnerFunc func(context.Context, build.Input) (build.Result, error)

func (f buildRunnerFunc) Build(ctx context.Context, in build.Input) (build.Result, error) {
	return f(ctx, in)
}

type slowAgent struct {
	fakeAgent
	block chan struct{}
}

func (a *slowAgent) CreateRuntime(ctx context.Context, _ CreateRuntimeRequest) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-a.block:
		return nil
	}
}

func TestStepTimeoutFailsTheStep(t *testing.T) {
	svc, rec, plan := setup(t)
	agent := &slowAgent{block: make(chan struct{})}
	ex := New(svc, fakeBuilder{}, agent)
	ex.Backoff = nil
	ex.Timeouts = map[string]time.Duration{"CREATE_RUNTIME": 50 * time.Millisecond}
	done := make(chan error, 1)
	go func() {
		_, err := ex.Execute(context.Background(), Request{DeploymentID: rec.ID, Container: "c", Source: fakeSource{}, Plan: plan})
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected timeout failure")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("execution did not finish after step timeout")
	}
	got, _ := svc.Get(context.Background(), rec.ID)
	if got.Status != deployment.StateFailed || got.ErrorCode != ErrorRuntimeFailed {
		t.Fatalf("status=%s code=%s", got.Status, got.ErrorCode)
	}
	steps, _ := svc.Store().Steps(context.Background(), rec.ID)
	if steps[1].Status != deployment.StepFailed || steps[1].ErrorCode != ErrorRuntimeFailed {
		t.Fatalf("CREATE_RUNTIME step = %+v", steps[1])
	}
}

func TestRunnerLifecycle(t *testing.T) {
	svc, rec, plan := setup(t)
	agent := &slowAgent{block: make(chan struct{})}
	ex := New(svc, fakeBuilder{}, agent)
	ex.Backoff = nil
	ex.Timeouts = map[string]time.Duration{"CREATE_RUNTIME": 100 * time.Millisecond}
	r := NewRunner(ex)

	req := Request{DeploymentID: rec.ID, Container: "c", Source: fakeSource{}, Plan: plan}
	if err := r.Start(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if err := r.Start(context.Background(), req); err != ErrAlreadyRunning {
		t.Fatalf("duplicate start: %v", err)
	}
	if !r.Running(rec.ID) {
		t.Fatal("must be running")
	}
	// Let the execution time out and finish, then the slot frees.
	deadline := time.Now().Add(10 * time.Second)
	for r.Running(rec.ID) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if r.Running(rec.ID) {
		t.Fatal("execution must finish after step timeout")
	}
	got, _ := svc.Get(context.Background(), rec.ID)
	if got.Status != deployment.StateFailed {
		t.Fatalf("status = %s", got.Status)
	}
}

func TestRunnerCancelMarksCancelled(t *testing.T) {
	svc, rec, plan := setup(t)
	release := make(chan struct{})
	agent := &slowAgent{block: release}
	ex := New(svc, fakeBuilder{}, agent)
	ex.Backoff = nil
	ex.Timeouts = map[string]time.Duration{"CREATE_RUNTIME": time.Minute}
	r := NewRunner(ex)

	req := Request{DeploymentID: rec.ID, Container: "c", Source: fakeSource{}, Plan: plan}
	if err := r.Start(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	// Wait until CREATE_RUNTIME is running.
	deadline := time.Now().Add(5 * time.Second)
	for {
		steps, _ := svc.Store().Steps(context.Background(), rec.ID)
		running := false
		for _, s := range steps {
			running = running || (s.Name == "CREATE_RUNTIME" && s.Status == deployment.StepRunning)
		}
		if running || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := r.Cancel(context.Background(), rec.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	close(release)
	deadline = time.Now().Add(10 * time.Second)
	for r.Running(rec.ID) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	got, _ := svc.Get(context.Background(), rec.ID)
	if got.Status != deployment.StateCancelled {
		t.Fatalf("status = %s, want CANCELLED", got.Status)
	}
	if err := r.Cancel(context.Background(), rec.ID); err != ErrNotRunning {
		t.Fatalf("cancel after finish: %v", err)
	}
}

func TestRunnerShutdown(t *testing.T) {
	svc, rec, plan := setup(t)
	agent := &slowAgent{block: make(chan struct{})}
	ex := New(svc, fakeBuilder{}, agent)
	ex.Backoff = nil
	ex.Timeouts = map[string]time.Duration{"CREATE_RUNTIME": time.Minute}
	r := NewRunner(ex)
	if err := r.Start(context.Background(), Request{DeploymentID: rec.ID, Container: "c", Source: fakeSource{}, Plan: plan}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := r.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if r.Running(rec.ID) {
		t.Fatal("nothing must run after shutdown")
	}
	if err := r.Start(context.Background(), Request{DeploymentID: rec.ID}); err == nil {
		t.Fatal("start after shutdown must be rejected")
	}
}

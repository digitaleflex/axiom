// Package executor — Runner runs deployments asynchronously (issue #100).
package executor

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrAlreadyRunning is returned when an execution is started twice.
var ErrAlreadyRunning = errors.New("deployment is already executing")

// ErrNotRunning is returned when cancelling an unknown execution.
var ErrNotRunning = errors.New("deployment is not executing")

// Runner executes deployments in the background so the API can return 202
// immediately and stream progress. At most one execution runs per deployment.
type Runner struct {
	executor *PlanExecutor
	// TotalTimeout bounds a whole execution; 0 disables it.
	TotalTimeout time.Duration

	mu      sync.Mutex
	running map[string]context.CancelFunc
	pending int
	wg      sync.WaitGroup
	closed  bool
}

// NewRunner returns a Runner for executor.
func NewRunner(executor *PlanExecutor) *Runner {
	return &Runner{executor: executor, running: map[string]context.CancelFunc{}, TotalTimeout: time.Hour}
}

// Start launches the execution in the background with a context detached from
// the request (cancellation of the HTTP request must not stop the deployment).
func (r *Runner) Start(parent context.Context, req Request) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errors.New("executor runner is shut down")
	}
	if _, ok := r.running[req.DeploymentID]; ok {
		return ErrAlreadyRunning
	}
	ctx := context.WithoutCancel(parent)
	if r.TotalTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.TotalTimeout)
		_ = cancel // released when the execution finishes
	}
	ctx, cancel := context.WithCancel(ctx)
	r.running[req.DeploymentID] = cancel
	r.pending++
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer cancel()
		defer func() {
			r.mu.Lock()
			delete(r.running, req.DeploymentID)
			r.mu.Unlock()
		}()
		if _, err := r.executor.Execute(ctx, req); err != nil {
			r.executor.log().Info("execution finished with error", "deploymentId", req.DeploymentID, "error", err.Error())
		}
	}()
	return nil
}

// Running reports whether the deployment is executing.
func (r *Runner) Running(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.running[id]
	return ok
}

// Cancel stops a running execution and marks the deployment CANCELLED when
// the state machine permits it (not during VERIFYING — the outcome belongs
// to health verification). Unknown executions return ErrNotRunning.
func (r *Runner) Cancel(ctx context.Context, id string) error {
	r.mu.Lock()
	cancel, ok := r.running[id]
	r.mu.Unlock()
	if !ok {
		return ErrNotRunning
	}
	rec, err := r.executor.deployments.Get(ctx, id)
	if err != nil {
		return err
	}
	if !rec.Status.Cancellable() {
		return errors.New("deployment in " + string(rec.Status) + " cannot be cancelled")
	}
	cancel()
	if _, err := r.executor.deployments.Cancel(ctx, id); err != nil {
		r.executor.log().Warn("marking cancelled deployment failed", "deploymentId", id, "error", err.Error())
	}
	return nil
}

// Shutdown stops accepting executions, cancels running ones and waits for
// them to finish (bounded by ctx).
func (r *Runner) Shutdown(ctx context.Context) error {
	r.mu.Lock()
	r.closed = true
	for _, cancel := range r.running {
		cancel()
	}
	r.mu.Unlock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.wg.Wait()
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	}
}
